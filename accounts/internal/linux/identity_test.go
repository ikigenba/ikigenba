package linux_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestApplyInvalidStateDoesNothing(t *testing.T) {
	// R-09JX-8LBZ
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
		"volume id": func(s *protocol.DesiredState) { s.DataVolume = "/dev/invalid" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newHardeningFixture(t)
			s := protocol.DesiredState{DataVolume: "vol-12345678", Accounts: []protocol.Account{acct}, Users: []protocol.User{user}}
			change(&s)
			if err := f.host.Apply(t.Context(), s); !errors.Is(err, machine.ErrInvalidState) {
				t.Fatalf("invalid Apply = %v", err)
			}
			f.expectCalls()
			f.expectAbsent("/etc/nftables")
			f.expectAbsent("/etc/systemd")
		})
	}
}

func fixtureKey(comment string) string {
	wire := []byte{0, 0, 0, 11, 's', 's', 'h', '-', 'e', 'd', '2', '5', '5', '1', '9', 0, 0, 0, 32}
	wire = append(wire, make([]byte, 32)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire) + " " + comment
}

func TestApplyLockBeforeAnyHardeningFailure(t *testing.T) {
	// R-BV89-YUHN
	f := newHardeningFixture(t)
	const locked = "shell:x:990:alice,bob,zoe\n"
	f.put("/etc/group", locked, 0o644)
	f.put("/etc/passwd", "alice:x:20001:20001::/home/alice:/bin/bash\nbob:x:20002:20002::/home/bob:/bin/bash\nzoe:x:20003:20003::/home/zoe:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "alice:!:1::::::\nbob:!:1:::::1:\nzoe:!:1::::::\n", 0o600)
	const unitDir = "/etc/systemd/system/"
	alice, bob, zoe := linux.PersonSlice("co-op", "alice"), linux.PersonSlice("co-op", "bob"), linux.PersonSlice("co-op", "zoe")
	for _, unit := range []string{alice, bob, zoe} {
		f.put(unitDir+unit, "fixture", 0o644)
	}
	f.put(linux.KeysDir+"/zoe", "old keys", 0o644)
	f.put("/home/zoe/keep", "old home", 0o640)
	f.answer = func(call string) (linux.Result, bool) {
		if call == "systemctl is-active "+bob {
			return linux.Result{ExitCode: 3}, true
		}
		if call == "rpm -q nftables" {
			return linux.Result{ExitCode: 7}, true
		}
		return linux.Result{}, false
	}
	f.applyFails(protocol.DesiredState{
		Accounts: []protocol.Account{{Slug: "co-op", GID: 20000, State: protocol.AccountActive}},
		Users: []protocol.User{
			{Name: "alice", Account: "co-op", UID: 20001, State: protocol.UserLocked},
			{Name: "bob", Account: "co-op", UID: 20002, State: protocol.UserDeleted},
		},
	}, "rpm -q nftables", "rpm")
	f.expectCalls("usermod --expiredate 1 alice", "systemctl is-active "+alice, "systemctl stop "+alice,
		"systemctl is-active "+bob, "usermod --expiredate 1 zoe", "systemctl is-active "+zoe, "systemctl stop "+zoe, "rpm -q nftables")
	f.expectFile(linux.KeysDir+"/zoe", "old keys", 0o644)
	f.expectFile("/home/zoe/keep", "old home", 0o640)
}

func TestApplyAccountsAndUsers(t *testing.T) {
	// R-BQSP-OL9R R-CM22-DSSX R-0FNF-5G1G R-CPPR-J410 R-CQXN-WVRP R-0GVB-J7S5 R-CTDG-OF93 R-82VL-4Z30 R-CX15-TQH6 R-0JB4-AR9J R-SM70-07MK
	f := newHardeningFixture(t)
	f.put("/etc/group", "shell:x:990:retained\nwillow:x:21000:\nperry:x:21001:maia\nmaia:x:21003:\nwheel:x:10:maia\n", 0o644)
	f.put("/etc/passwd", "perry:x:21001:21001::/srv/perry:/usr/sbin/nologin\nwillow:x:21000:21000::/srv/willow:/usr/sbin/nologin\nmaia:x:21003:21003::/home/maia:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "maia:!:1:::::1:\n", 0o600)
	f.put("/srv/willow/retained", "do not remove", 0o600)
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
	gitQuery := "git config --file " + f.path("/etc/gitconfig") + " --get-all safe.directory"
	f.answer = func(call string) (linux.Result, bool) {
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
		"lsblk --pairs --noheadings --output NAME,PKNAME,FSTYPE,MOUNTPOINT",
		"lsblk --nodeps --pairs --noheadings --output NAME,SERIAL",
		"sshd -t -f " + f.path("/etc/ssh/sshd_config"),
		"systemctl reload sshd.service",
		"groupadd --system --gid 21002 citron",
		"useradd --system --uid 21002 --gid citron --home-dir /srv/citron --no-create-home --shell /usr/sbin/nologin citron",
		"chown citron:citron " + f.path("/srv/citron"),
		"setfacl -d -m g::rwx,o::- " + f.path("/srv/citron"),
		"runuser -u citron -- git init --shared=group " + f.path("/srv/citron"),
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
			f.forbidCalls("groupadd --system --gid", "useradd", "chown", "git config")
			f.expectAbsent("/srv/citron")
		})
	}
}

func TestApplyDoesNotLockAnIdentityMismatch(t *testing.T) {
	// R-0GVB-J7S5
	f := newHardeningFixture(t)
	f.put("/etc/group", "shell:x:990:alice\ncitron:x:20000:alice\nalice:x:20002:\n", 0o644)
	f.put("/etc/passwd", "alice:x:20002:20002::/home/alice:/bin/bash\n", 0o644)
	f.put("/etc/shadow", "alice:!:1::::::\n", 0o600)
	f.put("/etc/systemd/system/"+linux.PersonSlice("citron", "alice"), "existing", 0o644)
	err := f.host.Apply(t.Context(), protocol.DesiredState{
		DataVolume: "vol-12345678",
		Accounts:   []protocol.Account{{Slug: "citron", GID: 20000, State: protocol.AccountActive}},
		Users:      []protocol.User{{Name: "alice", Account: "citron", UID: 20001, State: protocol.UserLocked}},
	})
	if !errors.Is(err, machine.ErrIdentity) {
		t.Fatalf("conflicting identity Apply = %v, want ErrIdentity", err)
	}
	f.expectCalls()
}

func TestApplySlicesAndSession(t *testing.T) {
	// R-CJM9-M9BJ R-SJR7-8O56 R-0D7M-DWK2 R-0EFI-ROAR R-BU0D-L2QY
	f := newHardeningFixture(t)
	const unitDir = "/etc/systemd/system/"
	const pamFile = "/etc/pam.d/sshd"
	const initialPAM = "session optional pam_systemd.so\n# an original line\n"
	f.put(pamFile, initialPAM, 0o644)
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
	if linux.SessionName != "accountsd-session" || linux.SessionHelper != "/usr/local/libexec/accountsd-session" {
		t.Fatal("session helper constants differ")
	}
	if target, err := os.Readlink(f.path(linux.SessionHelper)); err != nil || target != linux.DaemonPath {
		t.Fatalf("helper link = %q, %v", target, err)
	}
	pam, err := os.ReadFile(f.path(pamFile))
	if err != nil || !strings.Contains(string(pam), initialPAM) {
		t.Fatalf("PAM config lost the original lines: %s, %v", pam, err)
	}
	for _, word := range []string{"ingroup " + linux.ShellGroup, "session required pam_exec.so", linux.SessionHelper} {
		if !strings.Contains(string(pam), word) {
			t.Fatalf("PAM config missing %q", word)
		}
	}
	before, err := os.Stat(f.path(pamFile))
	if err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	f.apply(active)
	assertUnchanged(t, f.path(pamFile), before)
	f.forbidCalls("systemctl daemon-reload")
	var calls []linux.Command
	code := 0
	env := linux.Env{Root: f.root, Now: func() time.Time { return time.Unix(0, 0) }, Execute: func(_ context.Context, c linux.Command) (linux.Result, error) {
		calls = append(calls, c)
		return linux.Result{ExitCode: code}, nil
	}}
	lookup := func(kind, user string) func(string) string {
		return func(key string) string {
			switch key {
			case "PAM_TYPE":
				return kind
			case "PAM_USER":
				return user
			}
			t.Fatalf("unexpected PAM environment lookup %q", key)
			return ""
		}
	}
	for _, pair := range [][2]string{{"close_session", "alice"}, {"open_session", "inert"}, {"open_session", ""}} {
		if err := linux.Session(t.Context(), env, lookup(pair[0], pair[1]), 42); err != nil || len(calls) != 0 {
			t.Fatalf("Session(%v) = %v with %d commands", pair, err, len(calls))
		}
	}
	if err := linux.Session(t.Context(), env, lookup("open_session", "alice"), 42); err != nil {
		t.Fatal(err)
	}
	expected := linux.Command{Name: "busctl", Args: []string{
		"call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager", "StartTransientUnit",
		"ssa(sv)a(sa(sv))", "accountsd-session-42.scope", "fail", "2", "PIDs", "au", "1", "42", "Slice", "s", person, "0",
	}}
	if !reflect.DeepEqual(calls, []linux.Command{expected}) {
		t.Fatalf("session commands = %+v", calls)
	}
	code = 9
	var resultErr *linux.CommandError
	if err := linux.Session(t.Context(), env, lookup("open_session", "alice"), 42); !errors.As(err, &resultErr) || resultErr.Result.ExitCode != 9 {
		t.Fatalf("Session command failure = %v", err)
	}
}
