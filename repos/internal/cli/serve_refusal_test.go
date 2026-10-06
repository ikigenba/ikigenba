package cli_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

// R-PK7O-8WUI R-PLFK-MOL7 R-PMNH-0GBW R-PP39-RZTA R-PQB6-5RJZ
// R-PNVD-E82L R-Y2L6-GBNK
func TestServeRefusalOrderAndNoEffects(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"setting", map[string]string{"READ_SLOTS": "abc", "LISTEN_FDS": "99", "PATH": ""}, "repos: READ_SLOTS is 'abc', not a positive whole number of operations\n"},
		{"none", map[string]string{}, "repos: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
		{"many", map[string]string{"LISTEN_PID": "71", "LISTEN_FDS": "0002"}, "repos: 0002 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"},
		{"huge", map[string]string{"LISTEN_PID": "71", "LISTEN_FDS": "9999999999999999999999999"}, "repos: 9999999999999999999999999 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"},
	}
	for _, fds := range []string{"", "0", "000", "-1", "+1", "1x", " 1"} {
		cases = append(cases, struct {
			name string
			env  map[string]string
			want string
		}{"invalid-" + fds, map[string]string{"LISTEN_PID": "71", "LISTEN_FDS": fds}, "repos: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"})
	}
	cases = append(cases, struct {
		name string
		env  map[string]string
		want string
	}{"wrong-pid", map[string]string{"LISTEN_PID": "071", "LISTEN_FDS": "1"}, "repos: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newServeFixture(t)
			f.env = tc.env
			var seen []string
			f.p.LookupEnv = func(k string) (string, bool) { seen = append(seen, k); v, ok := f.env[k]; return v, ok }
			f.p.Inherit = func(uintptr) (net.Listener, error) { t.Error("inherit called"); return nil, errors.New("unexpected") }
			f.p.Unsetenv = func(string) error { t.Error("unset called"); return nil }
			f.p.Environ = func() []string { t.Error("git started"); return nil }
			f.p.Banner = func(page.User) page.Banner { t.Error("banner called"); return page.Banner{} }
			f.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Error("MCP called"); return nil }
			f.p.Sink = serveSink(t, func(context.Context, telemetry.Event) error { t.Error("event delivered"); return nil })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if code := cli.Run(ctx, f.p); code != cli.ExitUsage {
				t.Fatalf("exit %d", code)
			}
			if f.stdout.text() != "" || f.stderr.text() != tc.want || len(f.stderr.lines()) != 1 {
				t.Fatalf("streams %q / %q (%d calls)", f.stdout.text(), f.stderr.text(), len(f.stderr.lines()))
			}
			for i, k := range seen {
				if k == "LISTEN_PID" || k == "LISTEN_FDS" {
					if i < 9 {
						t.Fatalf("activation before settings: %v", seen)
					}
				}
				if k == "PATH" || k == "NOTIFY_SOCKET" || k == "IKIGENBA_SERVICES" {
					t.Fatalf("unexpected lookup %s", k)
				}
			}
			assertEmptyDirectory(t, f.dir)
		})
	}
}

// R-PRJ2-JJAO R-PSQY-XB1D R-L2IR-ITCM R-PV6R-OUIR R-PNVD-E82L
func TestServeTakesExactlyDescriptorThreeBeforeGit(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "inherit-failure", false: "missing-git"}[fail], func(t *testing.T) {
			f := newServeFixture(t)
			f.set("LISTEN_FDS", "0001")
			f.set("PATH", f.dir)
			var removed []string
			var descriptors []uintptr
			taken := false
			f.p.Unsetenv = func(key string) error { removed = append(removed, key); return errors.New("ignored unset") }
			lookup := f.p.LookupEnv
			f.p.LookupEnv = func(k string) (string, bool) {
				if k == "PATH" && !taken {
					t.Error("PATH looked up before inherit")
				}
				return lookup(k)
			}
			f.listen(t)
			ln := f.listener
			f.p.Inherit = func(fd uintptr) (net.Listener, error) {
				descriptors = append(descriptors, fd)
				taken = true
				if fail {
					return nil, errors.New("descriptor broken")
				}
				return ln, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if code := cli.Run(ctx, f.p); code != cli.ExitServerFailed {
				t.Fatalf("exit %d", code)
			}
			want := "repos: git not found on PATH\n"
			if fail {
				want = "repos: descriptor broken\n"
			}
			if f.stderr.text() != want || f.stdout.text() != "" || len(f.stderr.lines()) != 1 {
				t.Fatalf("streams %q / %q", f.stdout.text(), f.stderr.text())
			}
			removedKeys := make(map[string]bool)
			for _, key := range removed {
				switch key {
				case "LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES":
					removedKeys[key] = true
				default:
					t.Fatalf("unexpected removed key %q", key)
				}
			}
			if len(removedKeys) != 3 {
				t.Fatalf("missing activation keys: removed %v", removed)
			}
			if len(descriptors) != 1 || descriptors[0] != 3 {
				t.Fatalf("descriptors %v", descriptors)
			}
			assertEmptyDirectory(t, f.dir)
		})
	}
}

// R-YB4H-4PUF
func TestServeStoreFailureBeforeOtherEffects(t *testing.T) {
	for _, kind := range []string{"database", "root"} {
		t.Run(kind, func(t *testing.T) {
			f := newServeFixture(t)
			f.notification(t)
			f.listen(t)
			if kind == "database" {
				if err := os.WriteFile(filepath.Join(f.dir, "state"), []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				// First run leaves a sound empty catalog; then replace only its root.
				f.launch(t)
				f.ready(t)
				f.stop(t, "prepare", cli.ExitSuccess)
				if err := os.Remove(filepath.Join(f.dir, "state", "repos")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.dir, "state", "repos"), []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				f.stderr = &serveOutput{t: t}
				f.stdout = &serveOutput{t: t}
				f.p.Stderr = f.stderr
				f.p.Stdout = f.stdout
				f.capture = &serveCapture{t: t}
				f.p.Sink = f.capture
				f.mcpCalls.Store(0)
				f.listen(t)
			}
			g, err := git.Find(filepath.Dir(f.gitPath), f.p.Environ)
			if err != nil {
				t.Fatal(err)
			}
			d, expectedError := db.Open(t.Context(), statusConfig(f.dir))
			if expectedError == nil {
				_, expectedError = store.Open(t.Context(), d, store.Config{Root: filepath.Join(f.dir, "state", "repos"), Git: g, Now: f.p.Now, Rand: f.random})
				_ = d.Close()
			}
			if expectedError == nil {
				t.Fatal("fixture did not produce Store.Open failure")
			}

			if code := cli.Run(t.Context(), f.p); code != cli.ExitServerFailed {
				t.Fatalf("exit %d", code)
			}
			prefix := "repos: cannot open database state/repos.db: "
			fixture := "state"
			if kind == "root" {
				prefix = "repos: cannot create directory state/repos: "
				fixture = "state/repos"
			}
			if f.stderr.text() != prefix+expectedError.Error()+"\n" || len(f.stderr.lines()) != 1 || f.stdout.text() != "" {
				t.Fatalf("diagnostic %q", f.stderr.text())
			}
			if f.mcpCalls.Load() != 0 || f.bannerCalls.Load() != 0 || len(f.capture.Events()) != 0 {
				t.Fatalf("other effect after failure")
			}
			fixtureRoot, err := os.OpenRoot(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = fixtureRoot.Close() }()
			b, err := fixtureRoot.ReadFile(fixture)
			if err != nil || string(b) != "untouched" {
				t.Fatalf("fixture changed: %q %v", b, err)
			}
			noServeNotification(t, f.notify)
		})
	}
}

// R-YCCD-IHL4
func TestServeCancelledBeforeStoreIsSilent(t *testing.T) {
	f := newServeFixture(t)
	f.notification(t)
	f.listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := cli.Run(ctx, f.p); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	if f.stdout.text() != "" || f.stderr.text() != "" || f.mcpCalls.Load() != 0 || len(f.capture.Events()) != 0 {
		t.Fatalf("cancelled start effects: %q %q", f.stdout.text(), f.stderr.text())
	}
	assertEmptyDirectory(t, f.dir)
	noServeNotification(t, f.notify)
}
