package linux_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

type hardeningFixture struct {
	t        *testing.T
	root     string
	host     *linux.Host
	env      linux.Env
	commands []string
	answer   func(string) (linux.Result, bool)
	observe  func(linux.Command)
}

func newHardeningFixture(t *testing.T) *hardeningFixture {
	t.Helper()
	f := &hardeningFixture{t: t, root: t.TempDir()}
	for path, contents := range map[string]string{
		"/etc/group":                           "shell:x:990:\n",
		"/etc/passwd":                          "",
		"/etc/shadow":                          "",
		"/etc/pam.d/sshd":                      "session optional pam_systemd.so\n",
		"/etc/ssh/sshd_config":                 "Include /etc/ssh/sshd_config.d/*.conf\n",
		"/etc/ssh/ssh_host_ed25519_key":        "instance private\n",
		"/etc/ssh/ssh_host_ed25519_key.pub":    "instance public\n",
		"/usr/local/bin/accountsd":             "daemon\n",
		"/sys/class/block/nvme0n1p1/partition": "1\n",
	} {
		f.put(path, contents, 0o644)
	}
	f.env = linux.Env{
		Root: f.root,
		Now:  func() time.Time { return time.Unix(1_000, 0) },
		Execute: func(_ context.Context, c linux.Command) (linux.Result, error) {
			call := strings.Join(append([]string{c.Name}, c.Args...), " ")
			f.commands = append(f.commands, call)
			if f.observe != nil {
				f.observe(c)
			}
			if f.answer != nil {
				if result, ok := f.answer(call); ok {
					return result, nil
				}
			}
			if call == "rpm -q cronie" || call == "rpm -q at" {
				return linux.Result{ExitCode: 1}, nil
			}
			switch c.Name {
			case "lsblk":
				if c.Args[0] == "--nodeps" {
					return hostAnswer("NAME=\"nvme0n1\" SERIAL=\"vol00000000\"\n" +
						"SERIAL=\"vol12345678\" NAME=\"nvme1n1\"\n"), nil
				}
				return hostAnswer("NAME=\"nvme0n1p128\" PKNAME=\"nvme0n1\" FSTYPE=\"vfat\" MOUNTPOINT=\"/boot/efi\"\n" +
					"NAME=\"nvme0n1p1\" PKNAME=\"nvme0n1\" FSTYPE=\"xfs\" MOUNTPOINT=\"/\"\n"), nil
			case "blockdev":
				return hostAnswer("10737418240\n"), nil
			case "blkid":
				return hostAnswer("UUID=636e7f23-3f2d-4821-8b86-0b703e14be7b\nTYPE=xfs\n"), nil
			}
			return linux.Result{}, nil
		},
	}
	f.host = linux.New(f.env)
	return f
}

func hostAnswer(output string) linux.Result { return linux.Result{Stdout: []byte(output)} }

func (f *hardeningFixture) path(path string) string { return filepath.Join(f.root, path) }

func (f *hardeningFixture) put(path, contents string, mode fs.FileMode) {
	f.t.Helper()
	full := f.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), mode); err != nil {
		f.t.Fatal(err)
	}
	f.setMode(path, mode)
}

func (f *hardeningFixture) setMode(path string, mode fs.FileMode) {
	f.t.Helper()
	if err := os.Chmod(f.path(path), mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *hardeningFixture) expectFile(path, contents string, mode fs.FileMode) {
	f.t.Helper()
	data, err := os.ReadFile(f.path(path))
	if err != nil || string(data) != contents {
		f.t.Fatalf("%s bytes differ (read: %v)", path, err)
	}
	f.expectMode(path, mode)
}

func (f *hardeningFixture) expectMode(path string, mode fs.FileMode) {
	f.t.Helper()
	info := f.stat(path)
	if info.Mode().Perm() != mode {
		f.t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
	}
}

func (f *hardeningFixture) stat(path string) fs.FileInfo {
	f.t.Helper()
	info, err := os.Stat(f.path(path))
	if err != nil {
		f.t.Fatal(err)
	}
	return info
}

func (f *hardeningFixture) expectAbsent(path string) {
	f.t.Helper()
	if _, err := os.Lstat(f.path(path)); !errors.Is(err, fs.ErrNotExist) {
		f.t.Fatalf("%s: expected absence, got %v", path, err)
	}
}

func (f *hardeningFixture) apply(state protocol.DesiredState) {
	f.t.Helper()
	if err := f.host.Apply(f.t.Context(), state); err != nil {
		f.t.Fatal(err)
	}
}

func (f *hardeningFixture) applyFails(state protocol.DesiredState, lastCall, label string) {
	f.t.Helper()
	err := f.host.Apply(f.t.Context(), state)
	var commandErr *linux.CommandError
	if !errors.As(err, &commandErr) || commandErr.Label != label || commandErr.Result.ExitCode != 7 {
		f.t.Fatalf("apply error = %v, want %s exit 7", err, label)
	}
	if len(f.commands) == 0 || f.commands[len(f.commands)-1] != lastCall {
		f.t.Fatalf("apply ran beyond %q: %v", lastCall, f.commands)
	}
}

func (f *hardeningFixture) expectCalls(calls ...string) {
	f.t.Helper()
	if !slices.Equal(f.commands, calls) {
		f.t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(f.commands, "\n"), strings.Join(calls, "\n"))
	}
}

func (f *hardeningFixture) forbidCalls(prefixes ...string) {
	f.t.Helper()
	for _, call := range f.commands {
		for _, prefix := range prefixes {
			if strings.HasPrefix(call, prefix) {
				f.t.Fatalf("unexpected command %q", call)
			}
		}
	}
}

const hardenRules = "/etc/nftables/ikigenba-imds.nft"
const hardenIMDSUnit = "/etc/systemd/system/ikigenba-imds.service"
const hardenJournal = "/etc/systemd/journald.conf.d/ikigenba.conf"
const hardenUpdates = "/etc/dnf/automatic.conf"
const hardenJail = "/etc/fail2ban/jail.d/ikigenba.local"

const wantIMDSRules = "table inet ikigenba_imds\ndelete table inet ikigenba_imds\ntable inet ikigenba_imds {\n\tchain output {\n\t\ttype filter hook output priority filter; policy accept;\n\t\tmeta skuid != 0 ip daddr 169.254.169.254 drop\n\t\tmeta skuid != 0 ip6 daddr fd00:ec2::254 drop\n\t}\n}\n"
const wantIMDSUnit = "[Unit]\nBefore=sshd.service\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=nft -f /etc/nftables/ikigenba-imds.nft\n\n[Install]\nWantedBy=multi-user.target\n"
const wantJournal = "[Journal]\nStorage=persistent\n"
const wantJail = "[sshd]\nenabled = true\nbackend = systemd\nmode = normal\nbanaction = nftables-multiport\nmaxretry = 5\nfindtime = 10m\nbantime = 10m\nignoreip = 127.0.0.1/8 ::1\n"

func TestApplyPackages(t *testing.T) {
	// R-GZKW-7A5C R-H0SS-L1W1
	tests := []struct {
		name                    string
		missing                 []string
		wantInstall, wantRemove []string
	}{
		{
			name:    "no package changes",
			missing: []string{"cronie", "at"},
		},
		{
			name:        "install and remove all",
			missing:     []string{"nftables", "fail2ban-server", "dnf-automatic", "git"},
			wantInstall: []string{"dnf", "install", "-y", "nftables", "fail2ban-server", "dnf-automatic", "git"},
			wantRemove:  []string{"dnf", "remove", "-y", "cronie", "at"},
		},
		{
			name:        "install git only",
			missing:     []string{"git", "cronie", "at"},
			wantInstall: []string{"dnf", "install", "-y", "git"},
		},
		{
			name:        "install missing packages and remove at",
			missing:     []string{"fail2ban-server", "git", "cronie"},
			wantInstall: []string{"dnf", "install", "-y", "fail2ban-server", "git"},
			wantRemove:  []string{"dnf", "remove", "-y", "at"},
		},
		{
			name:       "remove cronie only",
			missing:    []string{"at"},
			wantRemove: []string{"dnf", "remove", "-y", "cronie"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.answer = func(call string) (linux.Result, bool) {
				name, ok := strings.CutPrefix(call, "rpm -q ")
				if ok && slices.Contains(test.missing, name) {
					return linux.Result{ExitCode: 1}, true
				}
				return linux.Result{}, ok
			}
			var got [][]string
			f.observe = func(c linux.Command) {
				if c.Name == "rpm" || c.Name == "dnf" {
					got = append(got, append([]string{c.Name}, c.Args...))
				}
			}
			f.apply(protocol.DesiredState{})
			want := [][]string{
				{"rpm", "-q", "nftables"},
				{"rpm", "-q", "fail2ban-server"},
				{"rpm", "-q", "dnf-automatic"},
				{"rpm", "-q", "git"},
			}
			if test.wantInstall != nil {
				want = append(want, test.wantInstall)
			}
			want = append(want, []string{"rpm", "-q", "cronie"}, []string{"rpm", "-q", "at"})
			if test.wantRemove != nil {
				want = append(want, test.wantRemove)
			}
			if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
				t.Fatalf("package commands = %q, want %q", got, want)
			}
		})
	}
}

func TestApplyStopsAtCommandFailure(t *testing.T) {
	// R-S739-S0BW
	launchError := errors.New("executable unavailable")
	for _, tc := range []struct {
		name, command, label string
		err                  error
	}{
		{"launch error", "nft list table inet ikigenba_imds", "nft", launchError},
		{"systemctl status", "systemctl daemon-reload", "systemctl daemon-reload", nil},
		{"dnf status", "dnf install -y git", "dnf install", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			if tc.label == "dnf install" {
				f.answer = func(call string) (linux.Result, bool) {
					return linux.Result{ExitCode: 1}, call == "rpm -q git"
				}
			}
			want := linux.Result{ExitCode: 7, Stdout: []byte("partial stdout"), Stderr: []byte("partial stderr")}
			execute := f.env.Execute
			f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
				res, err := execute(ctx, c)
				if f.commands[len(f.commands)-1] == tc.command {
					return want, tc.err
				}
				return res, err
			}
			f.host = linux.New(f.env)
			err := f.host.Apply(t.Context(), protocol.DesiredState{DataVolume: "vol-12345678"})
			var commandErr *linux.CommandError
			if !errors.As(err, &commandErr) || commandErr.Label != tc.label || !errors.Is(commandErr.Err, tc.err) {
				t.Fatalf("Apply error = %v, want CommandError with label %q and cause %v", err, tc.label, tc.err)
			}
			got := commandErr.Result
			if got.ExitCode != want.ExitCode || !slices.Equal(got.Stdout, want.Stdout) || !slices.Equal(got.Stderr, want.Stderr) {
				t.Fatalf("CommandError.Result = %+v, want %+v", got, want)
			}
			if f.commands[len(f.commands)-1] != tc.command {
				t.Fatalf("commands continued after failure: %v", f.commands)
			}
			f.expectAbsent(hardenJournal)
			f.expectAbsent(claudeSettingsPath)
			if tc.label != "dnf install" {
				f.expectFile(hardenIMDSUnit, wantIMDSUnit, 0o644)
			}
		})
	}
}

func TestApplyStopsAtHostFilesystemFailure(t *testing.T) {
	// R-S739-S0BW
	f := newHardeningFixture(t)
	const path = "/etc/dnf/vars/releasever"
	if err := os.MkdirAll(f.path(path), 0o700); err != nil {
		t.Fatal(err)
	}
	err := f.host.Apply(t.Context(), protocol.DesiredState{DataVolume: "vol-12345678"})
	if !errors.Is(err, fs.ErrInvalid) || !strings.Contains(err.Error(), path) {
		t.Fatalf("Apply error = %v, want invalid path %s", err, path)
	}
	if f.commands[len(f.commands)-1] != "journalctl --flush" {
		t.Fatalf("commands continued after filesystem failure: %v", f.commands)
	}
	f.expectFile(hardenJournal, wantJournal, 0o644)
	f.expectAbsent(hardenUpdates)
	f.expectAbsent(hardenJail)
	f.expectAbsent(claudeSettingsPath)
}

func TestApplyHostHardening(t *testing.T) {
	f := newHardeningFixture(t)
	f.put("/etc/group", "root:x:0:\n", 0o644)
	f.put(linux.SkelDir+"/retained", "keep\n", 0o600)
	f.setMode(linux.SkelDir, 0o700)
	f.answer = func(call string) (linux.Result, bool) {
		switch call {
		case "rpm -q nftables", "rpm -q dnf-automatic", "nft list table inet ikigenba_imds":
			return linux.Result{ExitCode: 1}, true
		case "systemctl is-enabled ikigenba-imds.service", "systemctl is-active dnf-automatic.timer", "systemctl is-enabled fail2ban.service":
			return linux.Result{ExitCode: 4}, true
		case "systemctl daemon-reload":
			f.expectFile(hardenIMDSUnit, wantIMDSUnit, 0o644)
		case "groupadd --system shell":
			f.put("/etc/group", "root:x:0:\nshell:x:990:\n", 0o644)
		case "nft -f " + f.path(hardenRules):
			f.expectFile(hardenRules, wantIMDSRules, 0o644)
		}
		return linux.Result{}, false
	}
	f.apply(protocol.DesiredState{})
	// R-BYVZ-45PQ R-C03V-HXGF R-C1BR-VP74 R-C3RK-N8OI R-ZHGD-K9Z9 R-C67D-ES5W
	sequence := []string{
		"rpm -q nftables", "rpm -q fail2ban-server", "rpm -q dnf-automatic", "rpm -q git", "dnf install -y nftables dnf-automatic",
		"rpm -q cronie", "rpm -q at",
		"systemctl daemon-reload", "nft list table inet ikigenba_imds", "nft -f " + f.path(hardenRules), "systemctl is-enabled ikigenba-imds.service", "systemctl enable ikigenba-imds.service",
		"systemd-tmpfiles --create --prefix " + f.path("/var/log/journal"), "systemctl restart systemd-journald.service", "journalctl --flush",
		"systemctl is-enabled dnf-automatic.timer", "systemctl is-active dnf-automatic.timer", "systemctl enable --now dnf-automatic.timer",
		"systemctl is-enabled fail2ban.service", "systemctl is-active fail2ban.service", "systemctl enable --now fail2ban.service", "systemctl reload fail2ban.service",
		"groupadd --system shell",
	}
	if len(f.commands) < len(sequence) || !slices.Equal(f.commands[:len(sequence)], sequence) {
		t.Fatalf("hardening commands = %v, want prefix %v", f.commands, sequence)
	}
	f.expectFile(hardenRules, wantIMDSRules, 0o644)
	f.expectFile(hardenIMDSUnit, wantIMDSUnit, 0o644)
	f.expectFile(hardenJournal, wantJournal, 0o644)
	f.expectMode("/var/log/journal", 0o755)
	f.expectFile("/etc/dnf/vars/releasever", "latest\n", 0o644)
	f.expectFile(hardenUpdates, "[commands]\nupgrade_type = security\napply_updates = yes\n", 0o644)
	f.expectFile(hardenJail, wantJail, 0o644)
	f.expectMode(linux.KeysDir, 0o755)
	f.expectMode(linux.SkelDir, 0o755)
	f.expectFile(linux.SkelDir+"/retained", "keep\n", 0o600)
	entries, err := os.ReadDir(f.path(linux.SkelDir))
	if err != nil || len(entries) != 1 {
		t.Fatalf("skeleton entries = %v, %v", entries, err)
	}
	f.answer, f.commands = nil, nil
	f.apply(protocol.DesiredState{})
	f.forbidCalls("dnf ", "nft -f", "systemctl daemon-reload", "systemd-tmpfiles ", "systemctl restart", "journalctl ", "systemctl enable", "systemctl reload ", "groupadd ")
}

func TestApplyHardeningRetries(t *testing.T) {
	tests := []struct {
		name, path, configured, before, label string
		failedCall                            func(*hardeningFixture) string
	}{
		{"rule rejected", hardenRules, wantIMDSRules, "old rule\n", "nft", func(f *hardeningFixture) string { return "nft -f " + f.path(hardenRules) }},
		{"new rule rejected", hardenRules, wantIMDSRules, "", "nft", func(f *hardeningFixture) string { return "nft -f " + f.path(hardenRules) }},
		{"journal tmpfiles", hardenJournal, wantJournal, "old journal\n", "systemd-tmpfiles", func(f *hardeningFixture) string {
			return "systemd-tmpfiles --create --prefix " + f.path("/var/log/journal")
		}},
		{"journal restart", hardenJournal, wantJournal, "", "systemctl restart", func(_ *hardeningFixture) string { return "systemctl restart systemd-journald.service" }},
		{"journal flush with correct drop-in", hardenJournal, wantJournal, wantJournal, "journalctl", func(_ *hardeningFixture) string { return "journalctl --flush" }},
		{"jail reload", hardenJail, wantJail, "old jail\n", "systemctl reload", func(_ *hardeningFixture) string { return "systemctl reload fail2ban.service" }},
		{"jail enable", hardenJail, wantJail, "", "systemctl enable", func(_ *hardeningFixture) string { return "systemctl enable --now fail2ban.service" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			if test.before != "" {
				f.put(test.path, test.before, 0o640)
			}
			call := test.failedCall(f)
			f.answer = func(invocation string) (linux.Result, bool) {
				if invocation == call {
					f.expectFile(test.path, test.configured, 0o644)
					return linux.Result{ExitCode: 7}, true
				}
				if test.name == "jail enable" && invocation == "systemctl is-active fail2ban.service" {
					return linux.Result{ExitCode: 3}, true
				}
				return linux.Result{}, false
			}
			// R-C03V-HXGF R-C1BR-VP74 R-ZHGD-K9Z9
			f.applyFails(protocol.DesiredState{}, call, test.label)
			if test.before == "" || test.before == test.configured {
				f.expectAbsent(test.path)
			} else {
				f.expectFile(test.path, test.before, 0o640)
			}
			// Run the same Apply again and observe it retry the failed work.
			f.commands = nil
			f.answer = func(invocation string) (linux.Result, bool) {
				if test.name == "jail enable" && invocation == "systemctl is-active fail2ban.service" {
					return linux.Result{ExitCode: 3}, true
				}
				return linux.Result{}, false
			}
			f.apply(protocol.DesiredState{})
			if !slices.Contains(f.commands, call) {
				t.Fatalf("retry omitted %q: %v", call, f.commands)
			}
			if test.name == "jail enable" {
				f.forbidCalls("systemctl reload fail2ban.service")
			}
		})
	}
}

func TestApplyAutomaticRetainsOtherLines(t *testing.T) {
	longComment := "#" + strings.Repeat(" preserved ", linux.MaxCapture) + "\r\n"
	tests := []struct{ name, before, want string }{
		{"append section", "[emitters]\nemit_via=motd", "[emitters]\nemit_via=motd\n[commands]\nupgrade_type = security\napply_updates = yes\n"},
		{"insert both before original lines", "[commands]\n# apply_updates = no\n; upgrade_type = default\n\n", "[commands]\nupgrade_type = security\napply_updates = yes\n# apply_updates = no\n; upgrade_type = default\n\n"},
		{"insert missing apply option", "[commands]\nupgrade_type=default\n[base]\napply_updates=no\n", "[commands]\napply_updates = yes\nupgrade_type = security\n[base]\napply_updates=no\n"},
		{"change repeated directives", "[commands]\r\n" + longComment + " \tupgrade_type  = default\r\napply_updates=no\nupgrade_type = bugfix\n" + strings.Repeat(" ", linux.MaxCapture) + "apply_updates = no\n[base]\nupgrade_type = default", "[commands]\r\n" + longComment + "upgrade_type = security\r\napply_updates = yes\nupgrade_type = security\napply_updates = yes\n[base]\nupgrade_type = default"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put(hardenUpdates, test.before, 0o644)
			// R-C3RK-N8OI
			f.apply(protocol.DesiredState{})
			f.expectFile(hardenUpdates, test.want, 0o644)
			before := f.stat(hardenUpdates)
			f.apply(protocol.DesiredState{})
			assertUnchanged(t, f.path(hardenUpdates), before)
		})
	}
}
