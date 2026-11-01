package cli_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/settings"
)

type writes struct {
	body  bytes.Buffer
	calls int
}

func (w *writes) Write(p []byte) (int, error) { w.calls++; return w.body.Write(p) }

type unusedSink struct{}

func (unusedSink) Deliver(context.Context, telemetry.Event) error { panic("command used sink") }

type unusedReader struct{}

func (unusedReader) Read([]byte) (int, error) { panic("command used randomness") }

func commandProcess(t *testing.T, args []string) (cli.Process, *writes, *writes) {
	t.Helper()
	out, errout := new(writes), new(writes)
	p := cli.Process{
		Args: args, Stdout: out, Stderr: errout, Version: "test-display", Dir: t.TempDir(), Pid: 241,
		LookupEnv: func(string) (string, bool) { panic("command read environment") },
		Unsetenv:  func(string) error { panic("command removed environment") },
		Inherit:   func(uintptr) (net.Listener, error) { panic("command inherited descriptor") },
		Now:       func() time.Time { panic("command read clock") },
		Sleep:     func(context.Context, time.Duration) { panic("command paused") },
		Rand:      unusedReader{}, Sink: unusedSink{},
		Banner: func(page.User) page.Banner { panic("command drew banner") },
		MCP:    func(*telemetry.Writer) *mcp.Server { panic("command made MCP server") },
	}
	return p, out, errout
}

// R-1XHL-ZRK4 R-1YPI-DJAT
func TestProcessAndRunShape(t *testing.T) {
	p, out, errout := commandProcess(t, []string{"--version"})
	var shape struct {
		Args      []string
		LookupEnv func(key string) (string, bool)
		Unsetenv  func(key string) error
		Pid       int
		Stdout    io.Writer
		Stderr    io.Writer
		Version   string
		Inherit   func(fd uintptr) (net.Listener, error)
		Now       func() time.Time
		Sleep     func(ctx context.Context, d time.Duration)
		Rand      io.Reader
		Dir       string
		Sink      telemetry.Sink
		Banner    func(u page.User) page.Banner
		MCP       func(w *telemetry.Writer) *mcp.Server
	} = p
	invoke := func(run func(context.Context, cli.Process) int) int {
		return run(context.Background(), cli.Process(shape))
	}
	if got := invoke(cli.Run); got != cli.ExitSuccess || out.body.String() != p.Version+"\n" || errout.calls != 0 {
		t.Fatalf("Run = %d; output %q / %q", got, out.body.String(), errout.body.String())
	}
}

// R-71FG-TMKY R-72ND-7EBN R-73V9-L62C
func TestExitConstants(t *testing.T) {
	small := [3]int8{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}
	large := [3]int64{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}
	if small != [3]int8{0, 1, 2} || large != [3]int64{0, 1, 2} {
		t.Fatalf("exit constants = %v, %v", small, large)
	}
}

// R-6WJV-AJM6 R-76B2-CPJQ R-79YR-I0RT
func TestProductCommandsDoNotUseRuntime(t *testing.T) {
	const help string = cli.Usage
	if help == "" || help[len(help)-1] != '\n' {
		t.Fatal("Usage must be non-empty and end in newline")
	}
	for _, tc := range []struct{ arg, version, want string }{
		{"--version", "display from caller", "display from caller\n"},
		{"--version", "", "\n"}, {"manifest", "unused", cli.Manifest}, {"--help", "unused", cli.Usage},
	} {
		t.Run(tc.arg+tc.version, func(t *testing.T) {
			p, out, errout := commandProcess(t, []string{tc.arg})
			p.Version = tc.version
			code := cli.Run(context.Background(), p)
			assertCode(t, code)
			if code != cli.ExitSuccess || out.body.String() != tc.want || errout.calls != 0 {
				t.Fatalf("Run = %d, stdout %q, stderr %q", code, out.body.String(), errout.body.String())
			}
			assertEmpty(t, p.Dir)
			// Only the four fields required for product commands are necessary.
			out, errout = new(writes), new(writes)
			if code := cli.Run(context.Background(), cli.Process{Args: p.Args, Stdout: out, Stderr: errout, Version: p.Version, Dir: p.Dir}); code != cli.ExitSuccess || out.body.String() != tc.want || errout.calls != 0 {
				t.Fatalf("minimal command = %d, %q, %q", code, out.body.String(), errout.body.String())
			}
		})
	}
}

// R-77IY-QHAF R-78QV-4914 R-79YR-I0RT
func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		arg  string
	}{
		{[]string{"bogus"}, "bogus"}, {[]string{"--bogus"}, "--bogus"}, {[]string{""}, ""},
		{[]string{"db"}, "db"}, {[]string{"db", "bogus"}, "bogus"},
		{[]string{"db", "status", "extra"}, "extra"}, {[]string{"db", "status", "-extra", "ignored"}, "-extra"},
		{[]string{"manifest", "extra"}, "extra"}, {[]string{"--version", "-v"}, "-v"},
		{[]string{"--help", "--help"}, "--help"}, {[]string{"bogus", "--version"}, "bogus"},
		{[]string{"db", "other", "status"}, "other"},
	} {
		t.Run(strings.Join(tc.args, "/"), func(t *testing.T) {
			p, out, errout := commandProcess(t, tc.args)
			code := cli.Run(context.Background(), p)
			assertCode(t, code)
			kind := "command"
			if strings.HasPrefix(tc.arg, "-") {
				kind = "option"
			}
			want := "sessions: unknown " + kind + " '" + tc.arg + "'\n\nsee 'sessions --help' for usage\n"
			if code != cli.ExitUsage || out.calls != 0 || errout.calls != 1 || errout.body.String() != want {
				t.Fatalf("Run = %d; writes %d/%d, stderr %q; want %q", code, out.calls, errout.calls, errout.body.String(), want)
			}
			assertEmpty(t, p.Dir)
		})
	}
}

// R-7B6N-VSII R-7CEK-9K97 R-7DMG-NBZW R-7EUD-13QL R-79YR-I0RT
func TestDatabaseStatus(t *testing.T) {
	t.Setenv(services.Variable, "")
	for _, state := range []string{"absent", "applied", "invalid", "newline-error"} {
		t.Run(state, func(t *testing.T) {
			p, out, errout := commandProcess(t, []string{"db", "status"})
			cfg := db.Config{Path: filepath.Join(p.Dir, "state", "sessions.db"), Migrations: sessions.Migrations()}
			switch state {
			case "applied":
				cfg.Now = func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }
				d, err := db.Open(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.Mkdir(filepath.Dir(cfg.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Path, []byte("this is not a SQLite database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "newline-error":
				p.Dir = filepath.Join(p.Dir, "missing\ncomponent", "child")
				cfg.Path = filepath.Join(p.Dir, "state", "sessions.db")
				if err := os.WriteFile(filepath.Dir(p.Dir), []byte("parent is a file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var before, after bytes.Buffer
			expectedErr := db.Status(context.Background(), cfg, &before)
			code := cli.Run(context.Background(), p)
			assertCode(t, code)
			afterErr := db.Status(context.Background(), cfg, &after)
			if before.String() != after.String() || out.body.String() != before.String() || errorText(expectedErr) != errorText(afterErr) {
				t.Fatalf("status changed: before=%q/%v after=%q/%v output=%q", before.String(), expectedErr, after.String(), afterErr, out.body.String())
			}
			if expectedErr == nil {
				if code != cli.ExitSuccess || errout.calls != 0 {
					t.Fatalf("success = %d, %q", code, errout.body.String())
				}
			} else {
				want := "sessions: " + strings.ReplaceAll(expectedErr.Error(), "\n", " ") + "\n"
				if code != cli.ExitServerFailed || errout.calls != 1 || errout.body.String() != want {
					t.Fatalf("failure = %d, %d writes, %q; want %q", code, errout.calls, errout.body.String(), want)
				}
			}
			if state == "absent" {
				assertEmpty(t, p.Dir)
			}
		})
	}
}

// R-2UEW-BKKV
func TestInvalidSettingsStopBeforeRuntime(t *testing.T) {
	p, out, errout := commandProcess(t, nil)
	var keys []string
	p.LookupEnv = func(key string) (string, bool) {
		keys = append(keys, key)
		if key == "DRAIN_SECONDS" {
			return "05", true
		}
		panic("invalid settings reached runtime environment")
	}
	code := cli.Run(context.Background(), p)
	assertCode(t, code)
	want := "sessions: " + (&settings.Error{Name: "DRAIN_SECONDS", Value: "05", Want: settings.WantSeconds}).Error() + "\n"
	if code != cli.ExitUsage || out.calls != 0 || errout.calls != 1 || errout.body.String() != want || !slices.Equal(keys, []string{"DRAIN_SECONDS"}) {
		t.Fatalf("invalid settings = %d, %q, %q; keys %v", code, out.body.String(), errout.body.String(), keys)
	}
	assertEmpty(t, p.Dir)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory changed: %v", entries)
	}
}

// R-7535-YXT1
func assertCode(t *testing.T, code int) {
	t.Helper()
	if code != cli.ExitSuccess && code != cli.ExitServerFailed && code != cli.ExitUsage {
		t.Fatalf("unrecognized exit code %d", code)
	}
}
