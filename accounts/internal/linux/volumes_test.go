package linux_test

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const rootProbe = "lsblk --pairs --noheadings --output NAME,PKNAME,FSTYPE,MOUNTPOINT"
const dataProbe = "lsblk --nodeps --pairs --noheadings --output NAME,SERIAL"
const volumeUUID = "b11361ec-f088-4db6-9102-f823cd3fbc27"

const sshUnitPath = "/etc/systemd/system/sshd.service.d/ikigenba.conf"
const sshDropInPath = "/etc/ssh/sshd_config.d/10-ikigenba.conf"
const dataPrivateKey = "/data/ssh/ssh_host_ed25519_key"
const dataPublicKey = "/data/ssh/ssh_host_ed25519_key.pub"
const instancePrivateKey = "/etc/ssh/ssh_host_ed25519_key"
const instancePublicKey = "/etc/ssh/ssh_host_ed25519_key.pub"

const wantSSHDUnit = "[Unit]\nRequiresMountsFor=/data/ssh\nRequires=ikigenba-imds.service\nAfter=ikigenba-imds.service\n"
const wantSSHDDropIn = `PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
GSSAPIAuthentication no
GSSAPIKeyExchange no
PermitRootLogin no
LoginGraceTime 30
MaxAuthTries 3
ClientAliveInterval 300
ClientAliveCountMax 2
AllowGroups shell
UsePAM yes
AllowTcpForwarding no
AllowStreamLocalForwarding no
AllowAgentForwarding no
X11Forwarding no
PermitTunnel no
PerSourcePenalties yes
MaxStartups 30:30:100
PerSourceMaxStartups 20
PubkeyAcceptedAlgorithms ssh-ed25519
HostKey /data/ssh/ssh_host_ed25519_key
HostKeyAlgorithms ssh-ed25519
KexAlgorithms mlkem768x25519-sha256,sntrup761x25519-sha512,sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org
Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com
MACs hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com

Match Group shell
  AuthorizedKeysFile /etc/ssh/authorized_keys/%u
`

func (f *hardeningFixture) checkDataVolumeCommands(device string) (probe, format string) {
	probeArgs := []string{"blkid", "-p", "--output", "export", device}
	formatArgs := []string{"mkfs.xfs", "-L", "ikigenbadata", device}
	f.observe = func(c linux.Command) {
		name, _, _ := strings.Cut(c.Name, " ")
		var want []string
		switch name {
		case "blkid":
			want = probeArgs
		case "mkfs.xfs":
			want = formatArgs
		default:
			return
		}
		if c.Name != want[0] || !slices.Equal(c.Args, want[1:]) {
			f.t.Fatalf("command = %q with arguments %q, want %q with arguments %q", c.Name, c.Args, want[0], want[1:])
		}
	}
	return strings.Join(probeArgs, " "), strings.Join(formatArgs, " ")
}

func TestApplyVolumeAndSSHSetup(t *testing.T) {
	f := newHardeningFixture(t)
	dataDevice := f.path("/dev/nvme1n1")
	rootDisk := f.path("/dev/nvme0n1")
	probe, format := f.checkDataVolumeCommands(dataDevice)
	rootSize, dataSize := "1048576\n", "2097152\n"
	formatted, mounted := false, false
	mounts := map[string]bool{}
	f.answer = func(call string) (linux.Result, bool) {
		switch call {
		case "blockdev --getsize64 " + rootDisk:
			return hostAnswer(rootSize), true
		case "blockdev --getsize64 " + dataDevice:
			return hostAnswer(dataSize), true
		case "growpart " + rootDisk + " 1":
			return linux.Result{ExitCode: 1}, true
		case probe:
			if !formatted {
				return linux.Result{ExitCode: 2}, true
			}
			return hostAnswer("LABEL=ikigenbadata\nUUID=" + volumeUUID + "\nTYPE=xfs\n"), true
		case format:
			formatted = true
		case "systemctl is-enabled home.mount":
			if !mounted {
				t.Fatal("home.mount was queried before data.mount was active")
			}
			f.expectMode("/data/home", 0o755)
			f.expectMode("/data/srv", 0o755)
			f.expectMode("/data/ssh", 0o700)
			if !mounts["home.mount"] {
				return linux.Result{ExitCode: 1}, true
			}
		case "systemctl enable --now data.mount":
			mounted = true
		case "systemctl is-active data.mount":
			if !mounted {
				f.expectAbsent("/data")
				return linux.Result{ExitCode: 3}, true
			}
		case "systemctl is-active srv.mount":
			if !mounts["srv.mount"] {
				return linux.Result{ExitCode: 3}, true
			}
		case "systemctl enable --now home.mount":
			mounts["home.mount"] = true
		case "systemctl enable --now srv.mount":
			mounts["srv.mount"] = true
		case "sshd -t -f " + f.path("/etc/ssh/sshd_config"):
			f.expectFile(sshUnitPath, wantSSHDUnit, 0o644)
			f.expectFile(sshDropInPath, wantSSHDDropIn, 0o600)
			f.expectFile(dataPrivateKey, "instance private\n", 0o600)
			f.expectFile(dataPublicKey, "instance public\n", 0o644)
		}
		return linux.Result{}, false
	}
	state := protocol.DesiredState{DataVolume: "vol-12345678"}
	// R-C7F9-SJWL R-C8N6-6BNA R-0RPX-5CWH R-CB2Y-XV4O R-CCAV-BMVD R-CDIR-PEM2 R-CEQO-36CR R-7VK6-UCMU R-CH6G-UPU5 R-CIED-8HKU
	f.apply(state)
	want := []string{
		rootProbe, "blockdev --getsize64 " + rootDisk, "growpart " + rootDisk + " 1", "xfs_growfs -d " + f.root,
		dataProbe, probe, format, probe, "systemctl daemon-reload",
		"systemctl is-enabled data.mount", "systemctl is-active data.mount", "systemctl enable --now data.mount",
		"systemctl is-enabled home.mount", "systemctl is-active home.mount", "systemctl enable --now home.mount",
		"systemctl is-enabled srv.mount", "systemctl is-active srv.mount", "systemctl enable --now srv.mount",
		"blockdev --getsize64 " + dataDevice, "xfs_growfs -d " + f.path("/data"), "systemctl daemon-reload",
		"sshd -t -f " + f.path("/etc/ssh/sshd_config"), "systemctl reload sshd.service",
	}
	start := slices.Index(f.commands, rootProbe)
	if start < 0 || len(f.commands) < start+len(want) || !slices.Equal(f.commands[start:start+len(want)], want) {
		t.Fatalf("commands = %v, want sequence %v", f.commands, want)
	}
	f.expectFile("/etc/systemd/system/data.mount", "[Mount]\nWhat=/dev/disk/by-uuid/"+volumeUUID+"\nWhere=/data\nType=xfs\n\n[Install]\nWantedBy=local-fs.target\n", 0o644)
	f.expectFile("/etc/systemd/system/home.mount", "[Unit]\nRequires=data.mount\nAfter=data.mount\n\n[Mount]\nWhat=/data/home\nWhere=/home\nType=none\nOptions=bind\n\n[Install]\nWantedBy=local-fs.target\n", 0o644)
	f.expectFile("/etc/systemd/system/srv.mount", "[Unit]\nRequires=data.mount\nAfter=data.mount\n\n[Mount]\nWhat=/data/srv\nWhere=/srv\nType=none\nOptions=bind\n\n[Install]\nWantedBy=local-fs.target\n", 0o644)
	f.expectMode(linux.HostKeyDir, 0o700)
	f.put(instancePrivateKey, "new instance private\n", 0o600)
	f.put(instancePublicKey, "new instance public\n", 0o644)
	f.setMode(sshDropInPath, 0o644)
	f.setMode(dataPrivateKey, 0o644)
	privateBefore, publicBefore := f.stat(dataPrivateKey), f.stat(dataPublicKey)
	f.commands = nil
	f.apply(state)
	f.forbidCalls("growpart ", "xfs_growfs ", "mkfs", "systemctl enable", "systemctl reload", "systemctl daemon-reload", "sshd ")
	assertUnchanged(t, f.path(dataPrivateKey), privateBefore)
	assertUnchanged(t, f.path(dataPublicKey), publicBefore)
	f.expectFile(dataPrivateKey, "instance private\n", 0o600)
	f.expectFile(dataPublicKey, "instance public\n", 0o644)
	f.expectFile(sshDropInPath, wantSSHDDropIn, 0o600)
	// Root and data disks grow independently.
	rootSize = "2097152\n"
	f.commands = nil
	f.apply(state)
	if !slices.Contains(f.commands, "growpart "+rootDisk+" 1") || !slices.Contains(f.commands, "xfs_growfs -d "+f.root) {
		t.Fatalf("root growth not applied: %v", f.commands)
	}
	f.forbidCalls("xfs_growfs -d " + f.path("/data"))
	dataSize = "4194304\n"
	f.commands = nil
	f.apply(state)
	f.forbidCalls("growpart ")
	if !slices.Contains(f.commands, "xfs_growfs -d "+f.path("/data")) || slices.Contains(f.commands, "xfs_growfs -d "+f.root) {
		t.Fatalf("data growth not isolated to data disk: %v", f.commands)
	}
}

func TestApplyExistingFilesystems(t *testing.T) {
	for _, kind := range []string{"ext4", "btrfs"} {
		t.Run(kind, func(t *testing.T) {
			f := newHardeningFixture(t)
			probe, _ := f.checkDataVolumeCommands(f.path("/dev/nvme1n1"))
			probes := 0
			f.answer = func(call string) (linux.Result, bool) {
				switch call {
				case rootProbe:
					return hostAnswer("NAME=\"nvme0n1\" PKNAME=\"\" FSTYPE=\"" + kind + "\" MOUNTPOINT=\"/\"\n"), true
				case probe:
					probes++
					return hostAnswer("TYPE=" + kind + "\nUUID=" + volumeUUID + "\n"), true
				}
				return linux.Result{}, false
			}
			// R-C7F9-SJWL R-0RPX-5CWH R-CDIR-PEM2
			f.apply(protocol.DesiredState{DataVolume: "vol-12345678"})
			if probes != 1 {
				t.Fatalf("signed device probed %d times, want 1", probes)
			}
			f.forbidCalls("growpart ", "mkfs", "xfs_growfs ")
			if kind == "ext4" {
				for _, name := range []string{"nvme0n1", "nvme1n1"} {
					if !slices.Contains(f.commands, "resize2fs "+f.path("/dev/"+name)) {
						t.Fatalf("did not grow %s: %v", name, f.commands)
					}
				}
			} else {
				f.forbidCalls("resize2fs ")
			}
			f.expectFile("/etc/systemd/system/data.mount", "[Mount]\nWhat=/dev/disk/by-uuid/"+volumeUUID+"\nWhere=/data\nType="+kind+"\n\n[Install]\nWantedBy=local-fs.target\n", 0o644)
		})
	}
}

func TestApplyUsesFilesystemMetadataFromSecondProbe(t *testing.T) {
	f := newHardeningFixture(t)
	probe, _ := f.checkDataVolumeCommands(f.path("/dev/nvme1n1"))
	const uuid = "5d5bd0a3-a2ab-443a-978d-8d45ae0b8fb8"
	// Use a synthetic type to distinguish the probe result from the mkfs program name.
	const kind = "ext4"
	probes := 0
	f.answer = func(call string) (linux.Result, bool) {
		if call != probe {
			return linux.Result{}, false
		}
		probes++
		if probes == 1 {
			return linux.Result{ExitCode: 2}, true
		}
		return hostAnswer("UUID=" + uuid + "\nTYPE=" + kind + "\n"), true
	}
	// R-0RPX-5CWH
	f.apply(protocol.DesiredState{DataVolume: "vol-12345678"})
	if probes != 2 {
		t.Fatalf("blank device probed %d times, want 2", probes)
	}
	f.expectFile("/etc/systemd/system/data.mount", "[Mount]\nWhat=/dev/disk/by-uuid/"+uuid+"\nWhere=/data\nType="+kind+"\n\n[Install]\nWantedBy=local-fs.target\n", 0o644)
}

func TestApplyUnsafeToFormat(t *testing.T) {
	for _, status := range []int{2, 7} {
		t.Run(map[int]string{2: "second probe still has no signature", 7: "probe failed"}[status], func(t *testing.T) {
			f := newHardeningFixture(t)
			probe, format := f.checkDataVolumeCommands(f.path("/dev/nvme1n1"))
			f.answer = func(call string) (linux.Result, bool) {
				return linux.Result{ExitCode: status}, call == probe
			}
			// R-0RPX-5CWH
			err := f.host.Apply(t.Context(), protocol.DesiredState{DataVolume: "vol-12345678"})
			var commandErr *linux.CommandError
			if !errors.As(err, &commandErr) || commandErr.Label != "blkid" || commandErr.Result.ExitCode != status {
				t.Fatalf("error = %v, want blkid status %d", err, status)
			}
			part := slices.Index(f.commands, dataProbe)
			if part < 0 {
				t.Fatalf("data device probe missing: %v", f.commands)
			}
			f.commands = f.commands[part:]
			if status == 2 {
				f.expectCalls(dataProbe, probe, format, probe)
			} else {
				f.expectCalls(dataProbe, probe)
			}
			f.expectAbsent("/etc/systemd/system/data.mount")
		})
	}
}

func TestApplyWithoutDataVolume(t *testing.T) {
	for _, volume := range []string{"", "vol-12345679"} {
		for name, state := range map[string]protocol.DesiredState{
			"empty state":  {},
			"with account": {Accounts: []protocol.Account{{Slug: "citron", GID: 1001, State: protocol.AccountActive}}},
			"with user":    {Users: []protocol.User{{Name: "someone", Account: "citron", UID: 1002, State: protocol.UserDeleted}}},
		} {
			t.Run(name+" "+volume, func(t *testing.T) {
				f := newHardeningFixture(t)
				state.DataVolume = volume
				// R-C8N6-6BNA
				err := f.host.Apply(t.Context(), state)
				if name == "empty state" {
					if err != nil {
						t.Fatal(err)
					}
					f.expectFile("/etc/systemd/system/acct.slice", "[Slice]\nCPUWeight=50\n", 0o644)
				} else {
					if !errors.Is(err, machine.ErrNoDataVolume) {
						t.Fatalf("error = %v, want ErrNoDataVolume", err)
					}
					f.expectAbsent("/etc/systemd/system/acct.slice")
				}
				for _, path := range []string{"/home", "/srv", "/data", sshUnitPath, sshDropInPath} {
					f.expectAbsent(path)
				}
				f.forbidCalls("blkid ", "mkfs", "sshd ", "groupadd --system --gid", "useradd ", "chown ")
			})
		}
	}
}

func TestApplyHostKeyWhenPairIncomplete(t *testing.T) {
	for _, path := range []string{dataPrivateKey, dataPublicKey} {
		t.Run(path, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put(path, "stale half\n", 0o600)
			// R-CEQO-36CR
			f.apply(protocol.DesiredState{DataVolume: "vol-12345678"})
			f.expectFile(dataPrivateKey, "instance private\n", 0o600)
			f.expectFile(dataPublicKey, "instance public\n", 0o644)
		})
	}
	for _, path := range []string{instancePrivateKey, instancePublicKey} {
		t.Run("missing "+path, func(t *testing.T) {
			f := newHardeningFixture(t)
			if err := os.Remove(f.path(path)); err != nil {
				t.Fatal(err)
			}
			err := f.host.Apply(t.Context(), protocol.DesiredState{DataVolume: "vol-12345678"})
			if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v, want missing %s", err, path)
			}
			f.expectAbsent(dataPrivateKey)
			f.expectAbsent(dataPublicKey)
			f.forbidCalls("sshd ", "systemctl reload sshd.service")
		})
	}
}

func TestApplySSHDConfigRollback(t *testing.T) {
	for _, before := range []string{"", "X11Forwarding yes\n"} {
		for _, failure := range []string{"check", "reload"} {
			t.Run(failure+" "+strings.TrimSpace(before), func(t *testing.T) {
				f := newHardeningFixture(t)
				if before != "" {
					f.put(sshDropInPath, before, 0o640)
				}
				check := "sshd -t -f " + f.path("/etc/ssh/sshd_config")
				const reload = "systemctl reload sshd.service"
				failingCall, label := check, "sshd"
				if failure == "reload" {
					failingCall, label = reload, "systemctl reload"
				}
				f.answer = func(call string) (linux.Result, bool) {
					if call == check || call == reload {
						f.expectFile(sshDropInPath, wantSSHDDropIn, 0o600)
					}
					return linux.Result{ExitCode: 7}, call == failingCall
				}
				state := protocol.DesiredState{DataVolume: "vol-12345678"}
				// R-CIED-8HKU
				f.applyFails(state, failingCall, label)
				if before == "" {
					f.expectAbsent(sshDropInPath)
				} else {
					f.expectFile(sshDropInPath, before, 0o640)
				}
				if failure == "check" {
					f.forbidCalls(reload)
				}
				f.commands, f.answer = nil, nil
				f.apply(state)
				index := slices.Index(f.commands, check)
				if index < 0 || index+1 >= len(f.commands) || f.commands[index+1] != reload {
					t.Fatalf("retry did not validate then reload sshd: %v", f.commands)
				}
			})
		}
	}
}
