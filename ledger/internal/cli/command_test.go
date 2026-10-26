package cli_test

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/cli"
	"github.com/ikigenba/ikigenba/ledger/internal/pages"
	"github.com/ikigenba/ikigenba/ledger/internal/settings"
)

const (
	_         = cli.Usage
	_         = cli.Manifest
	_         = cli.NginxConf
	_ int8    = cli.ExitSuccess
	_ uint64  = cli.ExitServerFailed
	_ float64 = cli.ExitUsage
)

type writes struct {
	bytes.Buffer
	n int
}

func (w *writes) Write(b []byte) (int, error) { w.n++; return w.Buffer.Write(b) }

type forbidden struct{ t *testing.T }

func (f forbidden) Read([]byte) (int, error) { f.t.Fatal("randomness used"); return 0, io.EOF }

type noTelemetry struct{ t *testing.T }

func (f noTelemetry) Deliver(context.Context, telemetry.Event) error {
	f.t.Fatal("telemetry used")
	return nil
}

type noEvents struct{ t *testing.T }

func (f noEvents) Deliver(context.Context, events.Event) error { f.t.Fatal("events used"); return nil }
func inert(t *testing.T, args []string) (cli.Process, *writes, *writes) {
	t.Helper()
	out, errout := &writes{}, &writes{}
	p := cli.Process{Args: args, Dir: t.TempDir(), Stdout: out, Stderr: errout, Version: "test identity", LookupEnv: func(string) (string, bool) { t.Fatal("environment used"); return "", false }, Unsetenv: func(string) error { t.Fatal("unset used"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit used"); return nil, nil }, Now: func() time.Time { t.Fatal("clock used"); return time.Time{} }, Sleep: func(context.Context, time.Duration) { t.Fatal("pause used") }, Rand: forbidden{t}, Sink: noTelemetry{t}, EventSink: noEvents{t}, Banner: func(page.User) page.Banner { t.Fatal("banner used"); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP used"); return nil }}
	return p, out, errout
}

// R-V7LM-RPEE R-VCH8-ASD6 R-VDP4-OK3V R-VEX1-2BUK R-VG4X-G3L9 R-VHCT-TVBY R-VIKQ-7N2N R-VJSM-LETC R-VM8F-CYAQ
func TestCommands(t *testing.T) {
	if cli.ExitSuccess != 0 || cli.ExitServerFailed != 1 || cli.ExitUsage != 2 {
		t.Fatal("exit codes")
	}
	if cli.Usage == "" || !strings.HasSuffix(cli.Usage, "\n") {
		t.Fatal("usage shape")
	}
	cases := []struct {
		args []string
		want string
		code int
		arg  string
	}{{[]string{"--version"}, "test identity\n", 0, ""}, {[]string{"manifest"}, cli.Manifest, 0, ""}, {[]string{"--help"}, cli.Usage, 0, ""}, {[]string{"db"}, "", 2, "db"}, {[]string{"bogus"}, "", 2, "bogus"}, {[]string{"--bogus"}, "", 2, "--bogus"}, {[]string{"db", "bogus"}, "", 2, "bogus"}, {[]string{"db", "status", "extra"}, "", 2, "extra"}, {[]string{"manifest", "extra"}, "", 2, "extra"}, {[]string{"--help", "--extra"}, "", 2, "--extra"}, {[]string{"--version", "extra", "other"}, "", 2, "extra"}}
	for _, c := range cases {
		p, out, errout := inert(t, c.args)
		if got := cli.Run(context.Background(), p); got != c.code {
			t.Fatalf("%v: %d", c.args, got)
		}
		if out.String() != c.want {
			t.Fatalf("stdout %q", out.String())
		}
		want := ""
		if c.arg != "" {
			kind := "command"
			if strings.HasPrefix(c.arg, "-") {
				kind = "option"
			}
			want = "ledger: unknown " + kind + " '" + c.arg + "'\n\nsee 'ledger --help' for usage\n"
			if errout.n != 1 {
				t.Fatal("diagnostic writes", errout.n)
			}
		}
		if errout.String() != want {
			t.Fatalf("stderr %q", errout.String())
		}
		entries, e := os.ReadDir(p.Dir)
		if e != nil || len(entries) != 0 {
			t.Fatal(entries, e)
		}
	}
	p, o, e := inert(t, []string{"--version"})
	p.Version = ""
	if cli.Run(context.Background(), p) != 0 || o.String() != "\n" || e.Len() != 0 {
		t.Fatal("empty identity")
	}
}

// R-V8TJ-5H53 R-VA1F-J8VS R-VB9B-X0MH R-6VVJ-1UX2 R-6X3F-FMNR
func TestManifestAndFragment(t *testing.T) {
	if strings.TrimSpace(pages.Description) == "" || strings.ContainsAny(pages.Description, "\"\\") {
		t.Fatal("description")
	}
	for _, b := range []byte(pages.Description) {
		if b < 32 || b == 127 {
			t.Fatal("description control")
		}
	}
	want := "app = \"ledger\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/ledger.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"application\"\n"
	if cli.Manifest != want {
		t.Fatal("manifest bytes")
	}

	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, e := fs.ReadFile(ledger.Etc(), name)
		if e != nil || string(got) != want {
			t.Fatalf("%s %q %v", name, got, e)
		}
	}
	if cli.NginxConf != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("fragment bytes")
	}
}

// R-VNGB-QQ1F R-VOO8-4HS4 R-VPW4-I9IT R-VR40-W19I
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"empty", "database", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			p, out, errout := inert(t, []string{"db", "status"})
			cfg := db.Config{Path: filepath.Join(p.Dir, "state", "ledger.db"), Migrations: ledger.Migrations()}
			switch mode {
			case "database":
				d, e := db.Open(context.Background(), cfg)
				if e != nil {
					t.Fatal(e)
				}
				if e = d.Close(); e != nil {
					t.Fatal(e)
				}
			case "invalid":
				if e := os.MkdirAll(filepath.Dir(cfg.Path), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(cfg.Path, []byte("invalid database"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			var before, after bytes.Buffer
			beforeErr := db.Status(context.Background(), cfg, &before)
			code := cli.Run(context.Background(), p)
			afterErr := db.Status(context.Background(), cfg, &after)
			if before.String() != out.String() || after.String() != before.String() {
				t.Fatal("status changed")
			}
			if (beforeErr == nil) != (afterErr == nil) {
				t.Fatal("status error changed")
			}
			if beforeErr != nil {
				if code != cli.ExitServerFailed || errout.String() != "ledger: "+strings.ReplaceAll(beforeErr.Error(), "\n", " ")+"\n" || errout.n != 1 {
					t.Fatal(code, errout.String())
				}
			} else if code != cli.ExitSuccess || errout.Len() != 0 {
				t.Fatal(code, errout.String())
			}
			if mode == "empty" {
				entries, e := os.ReadDir(p.Dir)
				if e != nil || len(entries) != 0 {
					t.Fatal(entries, e)
				}
			}
		})
	}
}

// R-74CT-1APN R-8M8G-N0XB R-8OO9-EKEP
func TestStartFailures(t *testing.T) {
	for _, mode := range []string{"setting", "setting_multiline", "absent", "multiple", "database"} {
		t.Run(mode, func(t *testing.T) {
			p, out, errout := inert(t, nil)
			p.Pid = 42
			p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "DRAIN_SECONDS":
					if strings.HasPrefix(mode, "setting") {
						if mode == "setting_multiline" {
							return "1\n", true
						}
						return "bad", true
					}
				case "LISTEN_PID":
					if strings.HasPrefix(mode, "setting") {
						t.Fatal("socket checked before settings")
					}
					return "42", true
				case "LISTEN_FDS":
					if mode == "multiple" {
						return "2", true
					}
					if mode == "database" {
						return "1", true
					}
				}
				return "", false
			}
			want := "ledger: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
			code := cli.ExitUsage
			if strings.HasPrefix(mode, "setting") {
				_, e := settings.Read(p.LookupEnv)
				want = "ledger: " + e.Error() + "\n"
			}
			if mode == "multiple" {
				want = "ledger: 2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
			}
			if mode == "database" {
				if e := os.WriteFile(filepath.Join(p.Dir, "state"), nil, 0600); e != nil {
					t.Fatal(e)
				}
				ln, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				p.Inherit = func(fd uintptr) (net.Listener, error) {
					if fd != 3 {
						t.Fatal(fd)
					}
					return ln, nil
				}
				p.Unsetenv = nil
				_, e = db.Open(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "ledger.db"), Migrations: ledger.Migrations()})
				if e == nil {
					t.Fatal("expected error")
				}
				want = "ledger: cannot open database state/ledger.db: " + strings.ReplaceAll(e.Error(), "\n", " ") + "\n"
				code = cli.ExitServerFailed
			}
			if got := cli.Run(context.Background(), p); got != code || out.Len() != 0 || errout.String() != want || errout.n != 1 {
				t.Fatal(got, out.String(), errout.String(), want, errout.n)
			}
			if mode != "database" {
				entries, e := os.ReadDir(p.Dir)
				if e != nil || len(entries) != 0 {
					t.Fatal(entries, e)
				}
			}
		})
	}
	var _ error = &settings.Error{}
}
