package linux_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestApplyInvalidStateDoesNothing(t *testing.T) {
	// R-ZJW6-BTGN
	acct := protocol.Account{Slug: "citron", GID: 20000, State: protocol.AccountActive}
	user := protocol.User{Name: "alice", Account: "citron", UID: 20001, State: protocol.UserActive}
	cases := map[string]func(*protocol.DesiredState){
		"slug":           func(s *protocol.DesiredState) { s.Accounts[0].Slug = "invalid/name" },
		"name":           func(s *protocol.DesiredState) { s.Users[0].Name = "shell" },
		"account absent": func(s *protocol.DesiredState) { s.Users[0].Account = "absent" },
		"small gid":      func(s *protocol.DesiredState) { s.Accounts[0].GID = 999 },
		"small uid":      func(s *protocol.DesiredState) { s.Users[0].UID = 999 },
		"duplicate id":   func(s *protocol.DesiredState) { s.Users[0].UID = acct.GID },
		"invalid key":    func(s *protocol.DesiredState) { s.Users[0].Keys = []string{"invalid SSH key"} },
		"multiline key": func(s *protocol.DesiredState) {
			s.Users[0].Keys = []string{fixtureKey("first") + "\r\n" + fixtureKey("second")}
		},
		"volume id":          func(s *protocol.DesiredState) { s.DataVolume = "/dev/invalid" },
		"invalid allowlist":  func(s *protocol.DesiredState) { s.Allowlist = []string{"192.0.2.7\n"} },
		"uncanonical prefix": func(s *protocol.DesiredState) { s.Allowlist = []string{"192.0.2.77/24"} },
		"unban prefix":       func(s *protocol.DesiredState) { s.Unban = []string{"192.0.2.0/24"} },
		"uncanonical unban":  func(s *protocol.DesiredState) { s.Unban = []string{"2001:DB8::1"} },
		"too many allowed": func(s *protocol.DesiredState) {
			s.Allowlist = slices.Repeat([]string{"192.0.2.0/24"}, protocol.MaxAllowlist+1)
		},
		"too many unbans": func(s *protocol.DesiredState) {
			s.Unban = slices.Repeat([]string{"192.0.2.77"}, protocol.MaxUnbans+1)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newHardeningFixture(t)
			s := protocol.DesiredState{DataVolume: "vol-12345678", Accounts: []protocol.Account{acct}, Users: []protocol.User{user}}
			change(&s)
			before := snapshotHost(t, f.root)
			if err := f.host.Apply(t.Context(), s); !errors.Is(err, machine.ErrInvalidState) {
				t.Fatalf("invalid Apply = %v", err)
			}
			f.expectCalls()
			if after := snapshotHost(t, f.root); !reflect.DeepEqual(after, before) {
				t.Fatal("invalid Apply changed a host path")
			}
		})
	}
}

func TestApplyValidatesBackupIDs(t *testing.T) {
	// R-YRKQ-RQ9X
	for _, id := range []string{"op_123\n", "OP_123", strings.Repeat("a", 65), "../b"} {
		for _, b := range []protocol.Backup{{Close: id}, {Restore: &protocol.HostRestore{ID: id}}} {
			f := newHardeningFixture(t)
			before := snapshotHost(t, f.root)
			if err := f.host.Apply(t.Context(), protocol.DesiredState{Backup: &b}); !errors.Is(err, machine.ErrInvalidState) {
				t.Fatalf("invalid Apply = %v", err)
			}
			f.expectCalls()
			if after := snapshotHost(t, f.root); !reflect.DeepEqual(after, before) {
				t.Fatal("invalid Apply changed a host path")
			}
		}
	}
}

func TestApplyRecordsCloseAfterLocks(t *testing.T) {
	// R-YWGC-AT8P R-YRKQ-RQ9X
	f := newHardeningFixture(t)
	const stamp = linux.BackupDir + "/status/locked"
	f.put("/etc/group", "shell:x:990:alice\n", 0o644)
	f.put("/etc/passwd", "alice:x:20001:20001::/home/alice:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "alice:!:1::::::\n", 0o600)
	now := time.Date(2026, 11, 19, 6, 0, 0, 123, time.FixedZone("fixture", -6*60*60))
	f.env.Now = func() time.Time { return now }
	f.host = linux.New(f.env)
	const id = "op_0123456789abcdef"
	const lock = "systemctl stop user-20001.slice"
	s := protocol.DesiredState{Backup: &protocol.Backup{Close: id}, Version: 7}
	f.answer = func(call string) (linux.Result, bool) {
		if call == lock {
			f.expectAbsent(stamp)
			return linux.Result{ExitCode: 7}, true
		}
		return linux.Result{}, false
	}
	f.applyFails(s, lock, "systemctl stop")
	f.expectAbsent(stamp)
	f.commands = nil

	const lateFailure = "rpm -q nftables"
	f.answer = func(call string) (linux.Result, bool) {
		return linux.Result{ExitCode: 7}, call == lateFailure
	}
	f.applyFails(s, lateFailure, "rpm")
	f.expectFile(stamp, id+" 7 2026-11-19T12:00:00Z\n", 0o600)
	now = now.Add(time.Minute)
	f.commands = nil
	f.applyFails(s, lateFailure, "rpm")
	f.expectFile(stamp, id+" 7 2026-11-19T12:00:00Z\n", 0o600)
	s.Version++
	f.commands = nil
	f.applyFails(s, lateFailure, "rpm")
	f.expectFile(stamp, id+" 8 2026-11-19T12:01:00Z\n", 0o600)
}

func fixtureKey(comment string) string {
	wire := []byte{0, 0, 0, 11, 's', 's', 'h', '-', 'e', 'd', '2', '5', '5', '1', '9', 0, 0, 0, 32}
	wire = append(wire, make([]byte, 32)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire) + " " + comment
}

func TestApplyLockBeforeAnyHardeningFailure(t *testing.T) {
	// R-KXYD-8O0O R-AXUQ-0YCB
	f := newHardeningFixture(t)
	const locked = "shell:x:990:alice,bob,zoe,wren,sam\n"
	f.put("/etc/group", locked, 0o644)
	f.put("/etc/passwd", "alice:x:20006:20001::/home/alice:/bin/bash\nbob:x:20002:20002::/home/bob:/bin/bash\n"+
		"zoe:x:20003:20003::/home/zoe:/bin/bash\nwren:x:20004:20004::/home/wren:/bin/bash\n"+
		"sam:x:20005:20005::/home/sam:/bin/bash\nforeign:x:20008:20008::/home/foreign:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "alice:!:1::::::\nbob:!:1:::::1:\nzoe:!:1::::::\nwren:!:1:::::1:\nsam:!:1:::::1:\nforeign:!:1:::::1:\n", 0o600)
	const unitDir = "/etc/systemd/system/"
	alice, bob, zoe := linux.PersonSlice("co-op", "alice"), linux.PersonSlice("co-op", "bob"), linux.PersonSlice("co-op", "zoe")
	for _, unit := range []string{alice, bob, zoe} {
		f.put(unitDir+unit, "fixture", 0o644)
	}
	f.put(unitDir+"acct-outer-inner-zoe.slice", "not a person slice", 0o644)
	f.put(linux.KeysDir+"/zoe", "old keys", 0o644)
	f.put("/home/zoe/keep", "old home", 0o640)
	f.answer = func(call string) (linux.Result, bool) {
		switch call {
		case "systemctl is-active " + bob:
			return linux.Result{ExitCode: 3}, true
		case "systemctl is-active user-20004.slice":
			return linux.Result{ExitCode: 11}, true
		case "rpm -q nftables":
			return linux.Result{ExitCode: 7}, true
		}
		return linux.Result{}, false
	}
	f.applyFails(protocol.DesiredState{
		Accounts: []protocol.Account{{Slug: "co-op", GID: 20000, State: protocol.AccountActive}},
		Users: []protocol.User{
			{Name: "alice", Account: "co-op", UID: 20001, State: protocol.UserLocked},
			{Name: "bob", Account: "co-op", UID: 20002, State: protocol.UserDeleted},
			{Name: "sam", Account: "co-op", UID: 20005, State: protocol.UserActive},
			{Name: "tess", Account: "co-op", UID: 20007, State: protocol.UserLocked},
		},
	}, "rpm -q nftables", "rpm")
	f.expectCalls(
		"usermod --expiredate 1 alice", "systemctl is-active "+alice, "systemctl stop "+alice,
		"systemctl is-active user-20006.slice", "systemctl stop user-20006.slice",
		"systemctl is-active "+bob, "systemctl is-active user-20002.slice", "systemctl stop user-20002.slice",
		"usermod --expiredate 1 zoe", "systemctl is-active "+zoe, "systemctl stop "+zoe,
		"systemctl is-active user-20003.slice", "systemctl stop user-20003.slice",
		"systemctl is-active user-20004.slice",
		"rpm -q nftables",
	)
	f.expectFile(linux.KeysDir+"/zoe", "old keys", 0o644)
	f.expectFile("/home/zoe/keep", "old home", 0o640)
}

func TestApplyAbortsWhenStoppingUserSliceFails(t *testing.T) {
	// R-AXUQ-0YCB R-S739-S0BW
	f := newHardeningFixture(t)
	f.put("/etc/group", "shell:x:990:alice,bob\n", 0o644)
	f.put("/etc/passwd", "alice:x:20001:20001::/home/alice:/bin/bash\nbob:x:20002:20002::/home/bob:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "alice:!:1:::::1:\nbob:!:1::::::\n", 0o600)
	const stop = "systemctl stop user-20001.slice"
	f.answer = func(call string) (linux.Result, bool) {
		return linux.Result{ExitCode: 7}, call == stop
	}
	f.applyFails(protocol.DesiredState{}, stop, "systemctl stop")
	f.expectCalls("systemctl is-active user-20001.slice", stop)
}

func TestApplyAccountsAndUsers(t *testing.T) {
	// R-CM22-DSSX R-0FNF-5G1G R-CQXN-WVRP R-KZ69-MFRD R-XXPT-MSVA R-82VL-4Z30 R-CX15-TQH6 R-XYXQ-0KLZ R-SM70-07MK
	f := newHardeningFixture(t)
	const groups = "willow:x:21000:\nperry:x:21001:maia\nmaia:x:21003:\nwheel:x:10:maia\n"
	f.put("/etc/group", groups, 0o644)
	f.put("/etc/passwd", "perry:x:21001:21001::/srv/perry:/usr/sbin/nologin\nwillow:x:21000:21000::/srv/willow:/usr/sbin/nologin\nmaia:x:21003:21003::/home/maia:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "maia:!:1:::::1:\n", 0o600)
	f.put("/srv/willow/retained", "do not remove", 0o600)
	f.put("/srv/citron/.git/config", "existing git configuration", 0o600)
	f.put("/srv/perry/.git/config", "existing git configuration", 0o600)
	f.put("/home/maia/cover", "private", 0o600)
	f.setMode("/home/maia", 0o750)
	f.put(linux.KeysDir+"/deleted", "obsolete", 0o644)
	first, second := fixtureKey("first"), fixtureKey("second")
	state := protocol.DesiredState{DataVolume: "vol-12345678", Accounts: []protocol.Account{
		{Slug: "citron", GID: 21002, State: protocol.AccountActive},
		{Slug: "perry", GID: 21001, State: protocol.AccountActive},
		{Slug: "willow", GID: 21000, State: protocol.AccountRemoved},
	}, Users: []protocol.User{
		{Name: "maia", Account: "perry", UID: 21003, State: protocol.UserActive, Keys: []string{first, second}},
		{Name: "leon", Account: "citron", UID: 21004, State: protocol.UserLocked},
		{Name: "deleted", Account: "willow", UID: 21005, State: protocol.UserDeleted},
	}}
	state.Unban = []string{"198.51.100.7"}
	state.Allowlist = []string{"192.0.2.0/24"}
	gitQuery := "git config --file " + f.path("/etc/gitconfig") + " --get-all safe.directory"
	f.observe = func(c linux.Command) {
		if c.Name == "fail2ban-client" && c.Args[0] == "get" {
			writeBanOutput(t, c, "192.0.2.77\n")
		}
	}
	f.answer = func(call string) (linux.Result, bool) {
		if call == "groupadd --system shell" {
			f.put("/etc/group", "shell:x:990:retained\n"+groups, 0o644)
		}
		if call == gitQuery {
			return hostAnswer("/srv/other\n/srv/perry\n"), true
		}
		if call == "usermod --expiredate  maia" {
			f.expectFile(linux.KeysDir+"/maia", first+"\n"+second+"\n", 0o644)
		}
		return linux.Result{}, false
	}
	f.apply(state)
	f.expectFile(linux.KeysDir+"/maia", first+"\n"+second+"\n", 0o644)
	f.expectFile(linux.KeysDir+"/leon", "", 0o644)
	f.expectAbsent(linux.KeysDir + "/deleted")
	f.expectFile("/srv/willow/retained", "do not remove", 0o600)
	f.expectFile("/srv/perry/.git/config", "existing git configuration", 0o600)
	f.expectMode("/home/maia", 0o700)
	for _, path := range []string{"/srv/citron", "/srv/perry"} {
		info, err := os.Stat(f.path(path))
		if err != nil || info.Mode()&(fs.ModePerm|fs.ModeSetgid) != fs.ModeSetgid|0o770 {
			t.Fatalf("mode %s = %v, %v", path, info, err)
		}
	}
	last := -1
	for _, call := range []string{
		"rpm -q nftables",
		"nft list table inet ikigenba_imds",
		"journalctl --flush",
		"systemctl is-enabled dnf-automatic.timer",
		"systemctl is-enabled fail2ban.service",
		"systemctl reload fail2ban.service",
		unbanPrefix + "198.51.100.7",
		banQuery,
		unbanPrefix + "192.0.2.77",
		"groupadd --system shell",
		"lsblk --pairs --noheadings --output NAME,PKNAME,FSTYPE,MOUNTPOINT",
		"lsblk --nodeps --pairs --noheadings --output NAME,SERIAL",
		"sshd -t -f " + f.path("/etc/ssh/sshd_config"),
		"systemctl reload sshd.service",
		"groupadd --system --gid 21002 citron",
		"useradd --system --uid 21002 --gid citron --home-dir /srv/citron --no-create-home --shell /usr/sbin/nologin citron",
		"chown citron:citron " + f.path("/srv/citron"),
		"setfacl -d -m g::rwx,o::- " + f.path("/srv/citron"),
		gitQuery,
		"git config --file " + f.path("/etc/gitconfig") + " --add safe.directory /srv/citron",
		"chown perry:perry " + f.path("/srv/perry"),
		"usermod --append --groups shell,perry maia",
		"usermod --expiredate  maia",
		"groupadd --gid 21004 leon",
		"useradd --uid 21004 --gid leon --groups shell,citron --create-home --home-dir " + f.path("/home/leon") + " --skel " + f.path(linux.SkelDir) + " --key HOME_MODE=0700 --shell /bin/bash --expiredate 1 leon",
		"userdel willow",
		"groupdel willow",
		"gpasswd --delete maia wheel",
	} {
		i := slices.Index(f.commands, call)
		if i <= last {
			t.Fatalf("out of order or missing %q in %v", call, f.commands)
		}
		last = i
	}
	f.forbidCalls("getent", "id ", "groups ", "userdel --remove willow", "userdel deleted", "groupdel deleted",
		"runuser -u perry", "sudo", "git config --file "+f.path("/etc/gitconfig")+" --add safe.directory /srv/perry")
	queries := 0
	for _, call := range f.commands {
		if call == gitQuery {
			queries++
		}
	}
	if queries != 1 {
		t.Fatalf("safe.directory was queried %d times", queries)
	}
}

func TestApplyRefusesIdentityMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, file, text string
	}{
		{"same group name", "/etc/group", "citron:x:21001:\n"},
		{"other group name", "/etc/group", "other:x:21000:\n"},
		{"same login name", "/etc/passwd", "citron:x:21000:21001::/srv/citron:/usr/sbin/nologin\n"},
		{"other login name", "/etc/passwd", "other:x:21000:21001::/srv/other:/usr/sbin/nologin\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put(tc.file, tc.text, 0o644)
			err := f.host.Apply(t.Context(), protocol.DesiredState{DataVolume: "vol-12345678", Accounts: []protocol.Account{
				{Slug: "citron", GID: 21000, State: protocol.AccountActive},
			}})
			if !errors.Is(err, machine.ErrIdentity) {
				t.Fatalf("identity conflict Apply = %v", err)
			}
			f.forbidCalls("groupadd --system --gid", "useradd", "chown citron:citron ", "git config")
			f.expectAbsent("/srv/citron")
		})
	}
}

func TestApplyCreatesActiveUser(t *testing.T) {
	// R-KZ69-MFRD
	for _, tc := range []struct {
		name         string
		privateGroup bool
	}{
		{name: "without private group"},
		{name: "with private group", privateGroup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			groups := "shell:x:990:\ncitron:x:20000:\n"
			var want []linux.Command
			if tc.privateGroup {
				groups += "alice:x:20001:\n"
			} else {
				want = append(want, linux.Command{Name: "groupadd", Args: []string{"--gid", "20001", "alice"}})
			}
			f.put("/etc/group", groups, 0o644)
			f.put("/etc/passwd", "citron:x:20000:20000::/srv/citron:/usr/sbin/nologin\n", 0o644)
			f.put("/srv/citron/.git/config", "existing", 0o644)
			want = append(want, linux.Command{Name: "useradd", Args: []string{
				"--uid", "20001", "--gid", "alice", "--groups", "shell,citron", "--create-home",
				"--home-dir", f.path("/home/alice"), "--skel", f.path(linux.SkelDir),
				"--key", "HOME_MODE=0700", "--shell", "/bin/bash", "alice",
			}})
			var got []linux.Command
			f.observe = func(c linux.Command) {
				if (c.Name == "groupadd" || c.Name == "useradd") && slices.Contains(c.Args, "alice") {
					got = append(got, c)
				}
			}
			f.apply(protocol.DesiredState{
				DataVolume: "vol-12345678",
				Accounts:   []protocol.Account{{Slug: "citron", GID: 20000, State: protocol.AccountActive}},
				Users:      []protocol.User{{Name: "alice", Account: "citron", UID: 20001, State: protocol.UserActive}},
			})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("active user creation = %+v, want %+v", got, want)
			}
		})
	}
}

func TestApplyRepairsUserGroupMembership(t *testing.T) {
	// R-KZ69-MFRD
	for _, tc := range []struct {
		name         string
		shellMembers string
		acctMembers  string
		wantAppend   bool
	}{
		{name: "missing shell membership", acctMembers: "alice", wantAppend: true},
		{name: "missing account membership", shellMembers: "alice", wantAppend: true},
		{name: "both memberships present", shellMembers: "alice", acctMembers: "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			groups := fmt.Sprintf("shell:x:990:%s\ncitron:x:20000:%s\nalice:x:20001:\n", tc.shellMembers, tc.acctMembers)
			f.put("/etc/group", groups, 0o644)
			f.put("/etc/passwd", "citron:x:20000:20000::/srv/citron:/usr/sbin/nologin\nalice:x:20001:20001::/home/alice:/bin/bash\n", 0o644)
			f.put("/srv/citron/.git/config", "existing", 0o644)
			var got, want []linux.Command
			if tc.wantAppend {
				want = []linux.Command{{Name: "usermod", Args: []string{"--append", "--groups", "shell,citron", "alice"}}}
			}
			f.observe = func(c linux.Command) {
				switch c.Name {
				case "groupadd", "useradd", "usermod":
					if slices.Contains(c.Args, "alice") {
						got = append(got, c)
					}
				}
			}
			f.apply(protocol.DesiredState{
				DataVolume: "vol-12345678",
				Accounts:   []protocol.Account{{Slug: "citron", GID: 20000, State: protocol.AccountActive}},
				Users:      []protocol.User{{Name: "alice", Account: "citron", UID: 20001, State: protocol.UserActive}},
			})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("existing user identity commands = %+v, want %+v", got, want)
			}
		})
	}
}

func TestApplyLocksBeforeRefusingConflictingUserIdentity(t *testing.T) {
	// R-KZ69-MFRD R-KXYD-8O0O
	const groups = "shell:x:990:alice\ncitron:x:20000:alice\n"
	const group = "alice:x:20001:\n"
	const login = "alice:x:20001:20001::/home/alice:/bin/bash\n"
	for _, tc := range []struct{ name, group, passwd string }{
		{"private group id", "alice:x:20002:\n", login},
		{"account uid", group, "alice:x:20002:20001::/home/alice:/bin/bash\n"},
		{"account primary gid", group, "alice:x:20001:20002::/home/alice:/bin/bash\n"},
		{"conflicting group name", "other:x:20001:\n", login},
		{"conflicting account name", group, login + "other:x:20001:20002::/home/other:/bin/bash\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put("/etc/group", groups+tc.group, 0o644)
			f.put("/etc/passwd", tc.passwd, 0o644)
			f.put("/etc/shadow", "alice:!:1::::::\n", 0o600)
			f.put("/srv/citron/.git/config", "existing", 0o644)
			unit := linux.PersonSlice("citron", "alice")
			f.put("/etc/systemd/system/"+unit, "existing", 0o644)
			err := f.host.Apply(t.Context(), protocol.DesiredState{
				DataVolume: "vol-12345678",
				Accounts:   []protocol.Account{{Slug: "citron", GID: 20000, State: protocol.AccountActive}},
				Users:      []protocol.User{{Name: "alice", Account: "citron", UID: 20001, State: protocol.UserLocked}},
			})
			if !errors.Is(err, machine.ErrIdentity) {
				t.Fatalf("conflicting identity Apply = %v, want ErrIdentity", err)
			}
			uid := strings.Split(tc.passwd, ":")[2]
			userSlice := "user-" + uid + ".slice"
			want := []string{"usermod --expiredate 1 alice", "systemctl is-active " + unit, "systemctl stop " + unit,
				"systemctl is-active " + userSlice, "systemctl stop " + userSlice, "rpm -q nftables"}
			if len(f.commands) < len(want) || !slices.Equal(f.commands[:len(want)], want) {
				t.Fatalf("identity conflict did not allow the lockdown first: %v", f.commands)
			}
			for _, call := range f.commands[len(want):] {
				if strings.Contains(strings.ReplaceAll(call, f.root, ""), "alice") {
					t.Fatalf("command for conflicting user after lockdown: %q", call)
				}
			}
			f.expectAbsent(linux.KeysDir + "/alice")
		})
	}
}

func TestApplyReassignsDeletedUsersFiles(t *testing.T) {
	// R-KT2R-PL1W
	f := newHardeningFixture(t)
	f.put("/srv/named/project/file", "keep", 0o600)
	f.put("/srv/foreign/project", "keep", 0o600)
	f.put("/srv/mismatch/project", "keep", 0o600)
	f.put("/outside/file", "keep", 0o600)
	if err := os.Symlink(f.path("/outside"), f.path("/srv/named/link")); err != nil {
		t.Fatal(err)
	}
	stat, ok := f.stat("/srv/named/project/file").Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("cannot read the temporary files' owner")
	}
	uid := int64(stat.Uid)
	const namedLogins = "named:x:25001:25001::/srv/named:/usr/sbin/nologin\n" +
		"foreign:x:25002:25002::/srv/foreign:/usr/sbin/nologin\n" +
		"mismatch:x:25003:25003::/home/mismatch:/usr/sbin/nologin\n"
	f.put("/etc/passwd", fmt.Sprintf("retired:x:%d:%d::/home/retired:/bin/bash\n", uid, uid)+namedLogins, 0o644)
	f.put("/etc/group", fmt.Sprintf("shell:x:990:retired\nretired:x:%d:\nabsent:x:25004:\n", uid), 0o644)
	f.put("/etc/shadow", "retired:!:1::::::\n", 0o600)
	const unitDir = "/etc/systemd/system/"
	retired := linux.PersonSlice("named", "retired")
	absent := linux.PersonSlice("named", "absent")
	f.put(unitDir+retired, "retired", 0o644)
	f.put(unitDir+absent, "absent", 0o644)
	f.put(linux.KeysDir+"/retired", "retired", 0o644)
	f.put(linux.KeysDir+"/absent", "absent", 0o644)
	owners := map[string]string{}
	for file, owner := range map[string]string{
		"/srv/named": "named", "/srv/named/project": "named", "/srv/named/project/file": "named", "/srv/named/link": "named",
		"/srv/foreign": "root", "/srv/foreign/project": "root", "/srv/mismatch": "root", "/srv/mismatch/project": "root",
	} {
		owners[f.path(file)] = owner
	}
	for i := range linux.ChownBatch {
		file := fmt.Sprintf("/srv/named/project/file-%d", i)
		f.put(file, "keep", 0o600)
		owners[f.path(file)] = "named"
	}
	var got []string
	chowned := 0
	f.observe = func(c linux.Command) {
		if c.Name != "chown" || !slices.Contains(c.Args, "--") {
			return
		}
		if len(c.Args) < 4 || c.Args[0] != "--no-dereference" || c.Args[2] != "--" {
			t.Fatalf("unexpected file reassignment: %+v", c)
		}
		if chowned == 0 {
			got = append(got, "chown")
		}
		f.expectFile(linux.KeysDir+"/retired", "retired", 0o644)
		owner, batch := c.Args[1], c.Args[3:]
		if len(batch) > linux.ChownBatch {
			t.Fatalf("chown batch has %d paths", len(batch))
		}
		for _, file := range batch {
			if owners[file] != owner {
				t.Fatalf("ownership of %s reassigned to %q, want %q", file, owner, owners[file])
			}
			delete(owners, file)
			chowned++
		}
	}
	f.answer = func(call string) (linux.Result, bool) {
		switch call {
		case "usermod --expiredate 1 retired", "systemctl is-active " + retired, "systemctl stop " + retired, "groupdel retired":
			got = append(got, call)
		case "userdel --remove retired":
			f.expectAbsent(linux.KeysDir + "/retired")
			got = append(got, call)
			f.put("/etc/passwd", namedLogins, 0o644)
		}
		return linux.Result{}, false
	}
	f.apply(protocol.DesiredState{
		DataVolume: "vol-12345678",
		Accounts: []protocol.Account{
			{Slug: "named", GID: 25001, State: protocol.AccountRemoved},
			{Slug: "mismatch", GID: 25003, State: protocol.AccountRemoved},
		},
		Users: []protocol.User{
			{Name: "retired", Account: "named", UID: uid, State: protocol.UserDeleted},
			{Name: "absent", Account: "named", UID: 25004, State: protocol.UserDeleted},
		},
	})
	want := []string{"usermod --expiredate 1 retired", "systemctl is-active " + retired, "systemctl stop " + retired, "chown", "userdel --remove retired", "groupdel retired"}
	if !slices.Equal(got, want) || len(owners) != 0 || chowned <= linux.ChownBatch {
		t.Fatalf("deletion steps = %v, want %v, reassigned %d paths, missing paths %v", got, want, chowned, owners)
	}
	f.expectAbsent(linux.KeysDir + "/absent")
	f.expectAbsent(unitDir + retired)
	f.expectAbsent(unitDir + absent)
	f.forbidCalls("userdel foreign", "userdel --remove foreign", "groupdel foreign", "userdel --remove absent", "groupdel absent")
}

func TestApplyLeavesGroupAlreadyRemovedByUserdel(t *testing.T) {
	// R-KT2R-PL1W
	f := newHardeningFixture(t)
	f.put("/srv/unrelated/file", "keep", 0o600)
	fileOwner := f.stat("/srv/unrelated/file").Sys().(*syscall.Stat_t).Uid
	uid := int64(fileOwner) + 1000
	f.put("/etc/passwd", fmt.Sprintf("retired:x:%d:%d::/home/retired:/bin/bash\n", uid, uid), 0o644)
	f.put("/etc/group", fmt.Sprintf("shell:x:990:retired\nretired:x:%d:\n", uid), 0o644)
	f.put("/etc/shadow", "retired:!:1:::::1:\n", 0o600)
	f.put(linux.KeysDir+"/retired", "retired", 0o644)
	f.answer = func(call string) (linux.Result, bool) {
		if call == "userdel --remove retired" {
			f.put("/etc/passwd", "", 0o644)
			f.put("/etc/group", "shell:x:990:\n", 0o644)
		}
		return linux.Result{}, false
	}
	f.apply(protocol.DesiredState{
		DataVolume: "vol-12345678",
		Users:      []protocol.User{{Name: "retired", Account: "pear", UID: uid, State: protocol.UserDeleted}},
	})
	if !slices.Contains(f.commands, "userdel --remove retired") {
		t.Fatal("did not run userdel")
	}
	f.forbidCalls("groupdel retired", "chown --no-dereference root -- ")
	f.expectAbsent(linux.KeysDir + "/retired")
}

func TestApplySecuresUnlistedUsersKeysAndPreservesOtherFiles(t *testing.T) {
	// R-XU24-HHN7 R-KWQG-UW9Z R-KXYD-8O0O
	f := newHardeningFixture(t)
	const passwd = "retained:x:25001:25001::/home/retained:/bin/bash\nforeign:x:25002:25002::/srv/foreign:/usr/sbin/nologin\n"
	const group = "shell:x:990:retained\nforeign:x:25002:\nretained:x:25001:\n"
	f.put("/etc/passwd", passwd, 0o644)
	f.put("/etc/group", group, 0o644)
	f.put("/etc/shadow", "retained:!:1::::::\n", 0o600)
	unit := linux.PersonSlice("foreign", "retained")
	const unitDir = "/etc/systemd/system/"
	f.put(unitDir+unit, "unlisted", 0o666)
	f.put("/home/retained/file", "keep", 0o660)
	f.put("/srv/foreign/file", "keep", 0o660)
	f.put("/etc/accountsd.env", "keep", 0o662)
	f.put(linux.KeysDir+"/retained", "old key bytes", 0o676)
	keyBefore := f.stat(linux.KeysDir + "/retained")
	f.put("/outside/keep", "keep", 0o666)
	const linkedKey = linux.KeysDir + "/linked"
	if err := os.Symlink(f.path("/outside/keep"), f.path(linkedKey)); err != nil {
		t.Fatal(err)
	}
	keyLink, err := os.Lstat(f.path(linkedKey))
	if err != nil {
		t.Fatal(err)
	}
	f.apply(protocol.DesiredState{DataVolume: "vol-12345678"})
	for _, path := range []string{
		linux.DaemonPath, "/etc/accountsd.env", linux.SessionHelper, linux.HostKeyDir,
		filepath.Join(linux.HostKeyDir, "ssh_host_ed25519_key"), filepath.Join(linux.HostKeyDir, "ssh_host_ed25519_key.pub"),
		linux.KeysDir, linux.KeysDir + "/retained", linkedKey, "/etc/pam.d/sshd", unitDir + linux.PoolSlice,
	} {
		if !slices.Contains(f.commands, "chown --no-dereference root:root "+f.path(path)) {
			t.Errorf("root ownership was not requested for %s", path)
		}
	}
	if !slices.Contains(f.commands, "usermod --expiredate 1 retained") || !slices.Contains(f.commands, "systemctl stop "+unit) {
		t.Fatalf("unlisted shell member was not locked down: %v", f.commands)
	}
	f.forbidCalls("groupdel", "userdel", "chown retained", "chown foreign", "chown --no-dereference foreign", "chown --no-dereference retained")
	f.expectFile("/etc/passwd", passwd, 0o644)
	f.expectFile("/etc/group", group, 0o644)
	f.expectFile(unitDir+unit, "unlisted", 0o666)
	f.expectFile("/home/retained/file", "keep", 0o660)
	f.expectFile("/srv/foreign/file", "keep", 0o660)
	f.expectFile("/etc/accountsd.env", "keep", 0o640)
	f.expectFile(linux.KeysDir+"/retained", "old key bytes", 0o654)
	assertUnchanged(t, f.path(linux.KeysDir+"/retained"), keyBefore)
	f.expectFile("/outside/keep", "keep", 0o666)
	linkAfter, err := os.Lstat(f.path(linkedKey))
	if err != nil || !os.SameFile(keyLink, linkAfter) || keyLink.Mode() != linkAfter.Mode() {
		t.Fatalf("key symlink changed: %v", err)
	}
}

func TestApplySlicesAndPAM(t *testing.T) {
	// R-CJM9-M9BJ R-U9X8-WR7R R-BU0D-L2QY
	f := newHardeningFixture(t)
	const unitDir = "/etc/systemd/system/"
	const pamFile = "/etc/pam.d/sshd"
	const sharedPAMPath = "/etc/pam.d/password-auth"
	const preface = "# preface\r\nsession optional pam_succeed_if.so quiet user != root\n"
	const passwordPreface = "# session optional pam_systemd.so\r\n" +
		"session optional pam_other.so pam_systemd.so # ignored \\\n" +
		"session InClUdE pam_systemd.so\n" +
		"session SuBsTaCk /etc/pam.d/pam_systemd.so\n"
	const directSystemd = "\tsession optional pam_systemd.so\n"
	const absoluteSystemd = "-session [success=ok default=ignore] /usr/lib64/security/pam_systemd.so\r\n"
	const passwordSystemd = "-session     optional      pam_systemd.so\n"
	const continuedSystemd = "SeSsIoN \\ \t\n# between parts\n\n\tOpTiOnAl \\\n\tpam_systemd.so\n"
	longSystemd := "session [success=ok \\\n" + strings.Repeat("  ", 2200) + "default=ignore]\tpam_systemd.so"
	lastLine := "session optional pam_other.so \\\n\t" + strings.Repeat("long-option ", 400)
	f.put(pamFile, preface+directSystemd+absoluteSystemd+"\n"+lastLine, 0o666)
	f.put(sharedPAMPath, passwordPreface+passwordSystemd+continuedSystemd+longSystemd, 0o666)
	f.put("/etc/group", "shell:x:990:alice\ncit-ron:x:20000:alice\nalice:x:20001:\n", 0o644)
	f.put("/etc/passwd", "cit-ron:x:20000:20000::/srv/cit-ron:/usr/sbin/nologin\nalice:x:20001:20001::/home/alice:/bin/bash\n", 0o644)
	f.put("/srv/cit-ron/.git/config", "data", 0o600)
	f.put("/home/alice/file", "data", 0o600)
	f.put(unitDir+linux.PersonSlice("pear", "inert"), "keep", 0o644)
	f.put(unitDir+linux.AccountSlice("pear"), "remove", 0o644)
	f.put(unitDir+linux.PersonSlice("pear", "deleted"), "remove", 0o644)
	active := protocol.DesiredState{DataVolume: "vol-12345678", Accounts: []protocol.Account{
		{Slug: "cit-ron", GID: 20000, State: protocol.AccountActive},
		{Slug: "pear", GID: 20010, State: protocol.AccountRemoved},
	}, Users: []protocol.User{
		{Name: "alice", Account: "cit-ron", UID: 20001, State: protocol.UserActive},
		{Name: "deleted", Account: "pear", UID: 20011, State: protocol.UserDeleted},
	}}
	f.apply(active)
	f.expectFile(unitDir+linux.PoolSlice, "[Slice]\nCPUWeight=50\n", 0o644)
	f.expectFile(unitDir+linux.AccountSlice("cit-ron"), "[Slice]\nCPUWeight=100\nMemoryHigh=1.5G\nMemoryMax=2G\nTasksMax=512\n", 0o644)
	person := linux.PersonSlice("cit-ron", "alice")
	f.expectFile(unitDir+person, "[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n", 0o644)
	f.expectFile(unitDir+linux.PersonSlice("pear", "inert"), "keep", 0o644)
	f.expectAbsent(unitDir + linux.AccountSlice("pear"))
	f.expectAbsent(unitDir + linux.PersonSlice("pear", "deleted"))
	if target, err := os.Readlink(f.path(linux.SessionHelper)); err != nil || target != linux.DaemonPath {
		t.Fatalf("helper link = %q, %v", target, err)
	}
	linkBefore, err := os.Lstat(f.path(linux.SessionHelper))
	if err != nil {
		t.Fatal(err)
	}
	const skipSystemd = "session [success=1 default=ignore] pam_succeed_if.so quiet user ingroup " + linux.ShellGroup + "\n"
	const helper = "session [success=ignore default=1] pam_succeed_if.so quiet user ingroup " + linux.ShellGroup + "\n" +
		"session required pam_exec.so type=open_session " + linux.SessionHelper + "\n"
	wantPAM := preface + skipSystemd + directSystemd + skipSystemd + absoluteSystemd + "\n" + helper + lastLine
	wantPasswordAuth := passwordPreface + skipSystemd + passwordSystemd + skipSystemd + continuedSystemd + skipSystemd + longSystemd
	f.expectFile(pamFile, wantPAM, 0o644)
	f.expectFile(sharedPAMPath, wantPasswordAuth, 0o644)
	before := f.stat(pamFile)
	passwordBefore := f.stat(sharedPAMPath)
	f.commands = nil
	f.observe = func(c linux.Command) {
		if filepath.Base(c.Name) == linux.SessionName {
			t.Fatalf("repeated apply ran the session helper: %+v", c)
		}
		for _, arg := range c.Args {
			for _, path := range []string{pamFile, sharedPAMPath, linux.SessionHelper} {
				if strings.Contains(arg, path) && (c.Name != "chown" || !slices.Equal(c.Args, []string{"--no-dereference", "root:root", f.path(path)})) {
					t.Fatalf("repeated apply issued a placement command: %+v", c)
				}
			}
		}
	}
	f.apply(active)
	f.expectFile(pamFile, wantPAM, 0o644)
	f.expectFile(sharedPAMPath, wantPasswordAuth, 0o644)
	assertUnchanged(t, f.path(pamFile), before)
	assertUnchanged(t, f.path(sharedPAMPath), passwordBefore)
	linkAfter, err := os.Lstat(f.path(linux.SessionHelper))
	if err != nil || !os.SameFile(linkBefore, linkAfter) {
		t.Fatalf("rewrote session helper: %v", err)
	}
	f.forbidCalls("systemctl daemon-reload")
}

func TestApplyLeavesAbsentPasswordAuth(t *testing.T) {
	// R-U9X8-WR7R
	f := newHardeningFixture(t)
	f.apply(protocol.DesiredState{DataVolume: "vol-12345678"})
	f.expectAbsent("/etc/pam.d/password-auth")
}

func TestApplyReloadsChangedSlicesBeforePAMFailure(t *testing.T) {
	// R-CJM9-M9BJ
	f := newHardeningFixture(t)
	state := protocol.DesiredState{}
	f.apply(state)

	const sharedPAMPath = "/etc/pam.d/password-auth"
	if err := os.Symlink("password-auth", f.path(sharedPAMPath)); err != nil {
		t.Fatal(err)
	}
	const pool = "/etc/systemd/system/" + linux.PoolSlice
	f.put(pool, "[Slice]\nCPUWeight=25\n", 0o644)
	f.commands = nil
	reloads := 0
	f.answer = func(call string) (linux.Result, bool) {
		if call == "systemctl daemon-reload" {
			reloads++
			f.expectFile(pool, "[Slice]\nCPUWeight=50\n", 0o644)
		}
		return linux.Result{}, false
	}
	err := f.host.Apply(t.Context(), state)
	if err == nil || !strings.Contains(err.Error(), sharedPAMPath) || reloads != 1 {
		t.Fatalf("Apply returned %v after %d reloads, want a password-auth error after one reload", err, reloads)
	}

	if err := os.Remove(f.path(sharedPAMPath)); err != nil {
		t.Fatal(err)
	}
	f.apply(state)
	if reloads != 1 {
		t.Fatalf("retry reloaded unchanged slices, total reloads = %d", reloads)
	}
}
