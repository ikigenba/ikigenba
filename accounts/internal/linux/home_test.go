package linux_test

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	homeTemplateBody = "<user>, <account>, (<user>), account, <nope>.\n"
	homeUser         = "daffy"
	homeAccount      = "acme"
	homeDir          = "/home/" + homeUser
	homeFile         = homeDir + "/AGENTS.md"
	homeKeys         = linux.KeysDir + "/" + homeUser
	homeAccountEntry = "acme:x:20000:20000::/srv/acme:/usr/sbin/nologin\n"
	homeCreated      = homeAccountEntry + "daffy:x:20001:20001::/home/daffy:/bin/bash\n"
)

func newHomeFixture(t *testing.T) (*hardeningFixture, protocol.DesiredState) {
	t.Helper()
	f := newHardeningFixture(t)
	f.template["home/AGENTS.md"] = homeTemplateBody
	f.put("/etc/group", "shell:x:990:\nacme:x:20000:\n", 0o644)
	f.put("/etc/passwd", homeAccountEntry, 0o644)
	f.put("/srv/acme/.git/config", "existing", 0o644)
	return f, protocol.DesiredState{
		DataVolume: "vol-12345678",
		Accounts:   []protocol.Account{{Slug: homeAccount, GID: 20000, State: protocol.AccountActive}},
		Users:      []protocol.User{{Name: homeUser, Account: homeAccount, UID: 20001, State: protocol.UserActive}},
	}
}

func homeUseradd(f *hardeningFixture, locked bool) string {
	cmd := "useradd --uid 20001 --gid daffy --groups shell,acme --create-home --home-dir " + f.path(homeDir) +
		" --skel " + f.path(linux.SkelDir) + " --key HOME_MODE=0700 --shell /bin/bash"
	if locked {
		cmd += " --expiredate 1"
	}
	return cmd + " " + homeUser
}

func assertNoHomeCommand(t *testing.T, f *hardeningFixture, programs ...string) {
	t.Helper()
	for _, call := range f.commands {
		if !strings.Contains(call, homeDir) {
			continue
		}
		for _, program := range programs {
			if strings.HasPrefix(call, program+" ") || strings.Contains(call, " -- "+program+" ") {
				t.Errorf("unexpected home command %q", call)
			}
		}
	}
}

func TestApplyWritesNewHomeAsUser(t *testing.T) {
	// R-X9BT-ZE1E R-XAJQ-D5S3 R-XBRM-QXIS
	for _, tc := range []struct {
		name   string
		locked bool
	}{{"active", false}, {"locked", true}} {
		t.Run(tc.name, func(t *testing.T) {
			f, state := newHomeFixture(t)
			if tc.locked {
				state.Users[0].State = protocol.UserLocked
			}
			f.template["home/config/z"] = "<account> <user>"
			var commands []string
			f.observe = func(c linux.Command) {
				call := f.commands[len(f.commands)-1]
				if slices.Contains(c.Args, homeUser) {
					commands = append(commands, call)
				}
				if !strings.HasPrefix(call, "runuser -u "+homeUser+" -- ") {
					return
				}
				f.expectAbsent(homeKeys)
				path := c.Args[len(c.Args)-1]
				f.expectAbsent(strings.TrimPrefix(path, f.root))
				if c.Args[3] != "tee" {
					return
				}
				if c.Stdin == nil || c.Stdout != io.Discard {
					t.Fatal("tee must receive stdin and discard stdout")
				}
				want := "acme daffy"
				if path == f.path(homeFile) {
					want = "daffy, acme, (daffy), account, <nope>.\n"
				}
				got, err := io.ReadAll(c.Stdin)
				if err != nil || string(got) != want {
					t.Fatalf("tee input = %q, %v, want %q", got, err, want)
				}
			}
			f.apply(state)
			want := []string{
				"groupadd --gid 20001 daffy", homeUseradd(f, tc.locked),
				"runuser -u daffy -- tee " + f.path(homeFile), "runuser -u daffy -- chmod 0644 " + f.path(homeFile),
				"runuser -u daffy -- mkdir -m 0755 " + f.path(homeDir+"/config"),
				"runuser -u daffy -- tee " + f.path(homeDir+"/config/z"), "runuser -u daffy -- chmod 0644 " + f.path(homeDir+"/config/z"),
			}
			if !slices.Equal(commands, want) {
				t.Fatalf("user commands = %q, want %q", commands, want)
			}
			f.expectAbsent(homeFile)
			f.expectAbsent(homeDir + "/config")
			f.expectFile(homeKeys, "", 0o644)
			assertNoHomeCommand(t, f, "git")
			report, err := f.host.Inspect(t.Context(), state)
			wantHomes := []protocol.Home{{UID: 20001, Commit: fixtureCommit}}
			if err != nil || !slices.Equal(report.Homes, wantHomes) {
				t.Fatalf("homes = %v, %v, want %v", report.Homes, err, wantHomes)
			}

			f.put("/etc/passwd", homeCreated, 0o644)
			f.observe = nil
			f.commands = nil
			f.apply(state)
			f.forbidCalls("useradd ", "runuser -u "+linux.CloneUser+" ")
			assertNoHomeCommand(t, f, "git", "tee", "mkdir", "chmod")
			f.expectAbsent(homeFile)
		})
	}
}

func TestApplyPreservesExistingHomeTargets(t *testing.T) {
	// R-XBRM-QXIS
	for _, kind := range []string{"file", "directory", "dangling link"} {
		t.Run(kind, func(t *testing.T) {
			f, state := newHomeFixture(t)
			const retained = "restored bytes"
			switch kind {
			case "file":
				f.put(homeFile, retained, 0o600)
			case "directory":
				f.put(homeFile+"/retained", retained, 0o600)
			case "dangling link":
				if err := os.MkdirAll(f.path(homeDir), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing", f.path(homeFile)); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(f.path(homeFile))
			if err != nil {
				t.Fatal(err)
			}
			f.apply(state)
			if !slices.Contains(f.commands, homeUseradd(f, false)) {
				t.Fatal("Apply did not create the user over the restored home")
			}
			assertNoHomeCommand(t, f, "git", "tee", "mkdir", "chmod")
			f.forbidCalls("runuser -u " + linux.CloneUser + " ")
			after, err := os.Lstat(f.path(homeFile))
			if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
				t.Fatalf("Apply changed the restored target: %v", err)
			}
			if kind == "file" {
				f.expectFile(homeFile, retained, 0o600)
			}
			if kind == "dangling link" {
				f.expectAbsent(homeDir + "/missing")
			}
		})
	}
}

func TestApplyLeavesExistingUserWithoutHomeFile(t *testing.T) {
	// R-X9BT-ZE1E
	f, state := newHomeFixture(t)
	f.put("/etc/passwd", homeCreated, 0o644)
	f.apply(state)
	f.forbidCalls("useradd ", "runuser -u "+linux.CloneUser+" ")
	assertNoHomeCommand(t, f, "git", "tee", "mkdir", "chmod")
	f.expectAbsent(homeFile)
}

func TestApplyStopsAtHomeCommandFailure(t *testing.T) {
	// R-XBRM-QXIS R-X9BT-ZE1E
	for _, label := range []string{"useradd", "tee", "chmod"} {
		t.Run(label, func(t *testing.T) {
			f, state := newHomeFixture(t)
			failed, program := homeUseradd(f, false), "useradd"
			if label != "useradd" {
				program = "runuser"
				failed = "runuser -u daffy -- " + label + " " + f.path(homeFile)
				if label == "chmod" {
					failed = "runuser -u daffy -- chmod 0644 " + f.path(homeFile)
				}
			}
			f.answer = func(call string) (linux.Result, bool) {
				return linux.Result{ExitCode: 7}, call == failed
			}
			f.applyFails(state, failed, program)
			f.expectAbsent(homeKeys)
			report, err := f.host.Inspect(t.Context(), state)
			if err != nil || len(report.Homes) != 0 {
				t.Fatalf("failed home installation recorded: %v, %v", report.Homes, err)
			}
			if label == "useradd" {
				assertNoHomeCommand(t, f, "tee", "mkdir", "chmod")
				return
			}

			f.put("/etc/passwd", homeCreated, 0o644)
			f.commands = nil
			f.apply(state)
			f.forbidCalls("useradd ")
			assertNoHomeCommand(t, f, "git", "tee", "mkdir", "chmod")
			f.expectAbsent(homeFile)
		})
	}
}

func TestApplySkipsOccupiedHomeTargetsAndSubtrees(t *testing.T) {
	// R-X9BT-ZE1E
	f, state := newHomeFixture(t)
	f.template["home/config/new"] = "new"
	f.template["home/tail"] = "new"
	f.template["home/zero"] = "new"
	f.observe = func(_ linux.Command) {
		if f.commands[len(f.commands)-1] != homeUseradd(f, false) {
			return
		}
		f.put(homeFile, "existing", 0o600)
		f.put(homeDir+"/config/keep", "existing", 0o600)
		if err := os.Symlink("missing", f.path(homeDir+"/zero")); err != nil {
			t.Fatal(err)
		}
	}
	f.apply(state)
	for _, call := range f.commands {
		if strings.HasPrefix(call, "runuser -u daffy -- ") &&
			!strings.HasSuffix(call, " "+f.path(homeDir+"/tail")) {
			t.Errorf("occupied entry or its child reached %q", call)
		}
	}
	if !slices.Contains(f.commands, "runuser -u daffy -- tee "+f.path(homeDir+"/tail")) {
		t.Fatal("walker skipped subsequent siblings of an occupied directory")
	}
	f.expectFile(homeFile, "existing", 0o600)
	f.expectFile(homeDir+"/config/keep", "existing", 0o600)
	f.expectAbsent(homeDir + "/config/new")
	info, err := os.Lstat(f.path(homeDir + "/zero"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling link changed: %v, %v", info, err)
	}
}

func TestApplyStopsWhenHomeCannotBeReadBeforeUseradd(t *testing.T) {
	// R-XBRM-QXIS
	f, state := newHomeFixture(t)
	f.put("/home", "not a directory", 0o600)
	err := f.host.Apply(t.Context(), state)
	if !errors.Is(err, syscall.ENOTDIR) || !strings.Contains(err.Error(), homeDir) {
		t.Fatalf("Apply = %v, want a filesystem failure naming %s", err, homeDir)
	}
	f.forbidCalls("groupadd --gid 20001", "useradd ", "runuser -u "+linux.CloneUser+" ")
	f.expectAbsent(homeKeys)
}

func TestApplyStopsWhenCloneHomeTargetCannotBeRead(t *testing.T) {
	// R-X9BT-ZE1E
	f, state := newHomeFixture(t)
	f.observe = func(_ linux.Command) {
		if f.commands[len(f.commands)-1] != homeUseradd(f, false) {
			return
		}
		if err := os.Mkdir(f.path("/home"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(homeUser, f.path(homeDir)); err != nil {
			t.Fatal(err)
		}
	}
	err := f.host.Apply(t.Context(), state)
	if !errors.Is(err, syscall.ELOOP) || !strings.Contains(err.Error(), homeFile) {
		t.Fatalf("Apply = %v, want a filesystem failure naming %s", err, homeFile)
	}
	if len(f.commands) == 0 || f.commands[len(f.commands)-1] != homeUseradd(f, false) {
		t.Fatalf("Apply did not stop after useradd: %v", f.commands)
	}
	f.expectAbsent(homeKeys)
}
