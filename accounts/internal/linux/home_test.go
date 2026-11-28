package linux_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
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
	// R-4D9G-DG9M R-GMLV-J349
	for _, tc := range []struct {
		name   string
		empty  bool
		locked bool
	}{
		{name: "new home"},
		{name: "empty restored home", empty: true},
		{name: "locked user", locked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, state := newHomeFixture(t)
			if tc.locked {
				state.Users[0].State = protocol.UserLocked
			}
			if tc.empty {
				if err := os.MkdirAll(f.path(homeDir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			body, err := accounts.Home().Open("AGENTS.md")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = body.Close() }()
			wantBytes, err := io.ReadAll(machine.FillHome(body, homeUser, homeAccount))
			if err != nil {
				t.Fatal(err)
			}
			var commands []string
			teeArgs := []string{"-u", homeUser, "--", "tee", f.path(homeFile)}
			chmodArgs := []string{"-u", homeUser, "--", "chmod", "0644", f.path(homeFile)}
			f.observe = func(c linux.Command) {
				if slices.Contains(c.Args, homeUser) || slices.Contains(c.Args, f.path(homeFile)) || slices.Contains(c.Args, homeFile) {
					commands = append(commands, strings.Join(append([]string{c.Name}, c.Args...), " "))
				}
				if c.Name == "runuser" && slices.Equal(c.Args, chmodArgs) {
					f.expectAbsent(homeKeys)
				}
				if c.Name == "runuser" && slices.Equal(c.Args, teeArgs) {
					f.expectAbsent(homeKeys)
					f.expectAbsent(homeFile)
					if c.Stdin == nil || c.Stdout != io.Discard {
						t.Fatal("tee must receive stdin and discard stdout")
					}
					got, err := io.ReadAll(c.Stdin)
					if err != nil || !bytes.Equal(got, wantBytes) {
						t.Fatalf("tee input = %q, %v, want %q", got, err, wantBytes)
					}
				}
			}
			f.apply(state)
			want := []string{
				"groupadd --gid 20001 daffy", homeUseradd(f, tc.locked),
				"runuser " + strings.Join(teeArgs, " "), "runuser " + strings.Join(chmodArgs, " "),
			}
			if !slices.Equal(commands, want) {
				t.Fatalf("user commands = %q, want %q", commands, want)
			}
			f.expectAbsent(homeFile)
			f.expectFile(homeKeys, "", 0o644)
			assertNoHomeCommand(t, f, "git")

			f.put("/etc/passwd", homeCreated, 0o644)
			f.observe = nil
			f.commands = nil
			f.apply(state)
			f.forbidCalls("useradd ")
			assertNoHomeCommand(t, f, "git", "tee", "mkdir", "chmod")
			f.expectAbsent(homeFile)
		})
	}
}

func TestApplyPreservesExistingHomeTargets(t *testing.T) {
	// R-4D9G-DG9M
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
			assertNoHomeCommand(t, f, "tee", "chmod")
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
	// R-GMLV-J349
	f, state := newHomeFixture(t)
	f.put("/etc/passwd", homeCreated, 0o644)
	f.apply(state)
	f.forbidCalls("useradd ")
	assertNoHomeCommand(t, f, "git", "tee", "mkdir", "chmod")
	f.expectAbsent(homeFile)
}

func TestApplyStopsAtHomeCommandFailure(t *testing.T) {
	// R-4D9G-DG9M R-GMLV-J349
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

func TestApplyStopsWhenHomeTargetCannotBeRead(t *testing.T) {
	// R-4D9G-DG9M
	f, state := newHomeFixture(t)
	if err := os.Mkdir(f.path("/home"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(homeUser, f.path(homeDir)); err != nil {
		t.Fatal(err)
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
