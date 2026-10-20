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
	"github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/cli"
	"github.com/ikigenba/ikigenba/campaigns/internal/pages"
	"github.com/ikigenba/ikigenba/campaigns/internal/settings"
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

// R-AD64-P8DC R-AEE1-3041 R-UXIC-W9FW R-UYQ9-A16L R-UZY5-NSXA R-V3LU-T45D R-965H-1PD4 R-97DD-FH3T R-98L9-T8UI R-V4TR-6VW2 R-8YU2-R2WY
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
			want = "campaigns: unknown " + kind + " '" + c.arg + "'\n\nsee 'campaigns --help' for usage\n"
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

// R-V162-1KNZ R-V2DY-FCEO R-92HR-WE51 R-93PO-A5VQ R-IH2K-401Y R-IIAG-HRSN
func TestManifestAndFragment(t *testing.T) {
	if strings.TrimSpace(pages.Description) == "" || strings.ContainsAny(pages.Description, "\"\\") {
		t.Fatal("description")
	}
	for _, b := range []byte(pages.Description) {
		if b < 32 || b == 127 {
			t.Fatal("description control")
		}
	}
	want := "app = \"campaigns\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = true\nsecrets = []\n\n[env]\nCAMPAIGNS_DEFAULT_DESTINATION = \"https://michaelgreenly.dev/\"\nCAMPAIGNS_LINK_PREFIX = \"https://michaelgreenly.dev/c/?t=\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/campaigns.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"application\"\n"
	if cli.Manifest != want {
		t.Fatal("manifest bytes")
	}
	s := settings.Defaults()
	expectedEnvironment := "[env]\nCAMPAIGNS_DEFAULT_DESTINATION = \"" + s.DefaultDestination + "\"\nCAMPAIGNS_LINK_PREFIX = \"" + s.LinkPrefix + "\"\n\n"
	manifestText := cli.Manifest
	if !strings.Contains(manifestText, expectedEnvironment) {
		t.Fatal("manifest defaults")
	}
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, e := fs.ReadFile(campaigns.Etc(), name)
		if e != nil || string(got) != want {
			t.Fatalf("%s %q %v", name, got, e)
		}
	}
	if cli.NginxConf != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("fragment bytes")
	}
}

// R-V61N-KNMR R-V79J-YFDG R-9B12-KSBW R-9C8Y-YK2L
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"empty", "database", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			p, out, errout := inert(t, []string{"db", "status"})
			cfg := db.Config{Path: filepath.Join(p.Dir, "state", "campaigns.db"), Migrations: campaigns.Migrations()}
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
				if code != cli.ExitServerFailed || errout.String() != "campaigns: "+strings.ReplaceAll(beforeErr.Error(), "\n", " ")+"\n" || errout.n != 1 {
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

// R-B0C7-YVGJ R-QAT0-M6RZ R-QD8T-DQ9D
func TestStartFailures(t *testing.T) {
	for _, mode := range []string{"setting", "absent", "multiple", "database"} {
		t.Run(mode, func(t *testing.T) {
			p, out, errout := inert(t, nil)
			p.Pid = 42
			p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "DRAIN_SECONDS":
					if mode == "setting" {
						return "bad", true
					}
				case "LISTEN_PID":
					if mode == "setting" {
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
			want := "campaigns: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
			code := cli.ExitUsage
			if mode == "setting" {
				_, e := settings.Read(p.LookupEnv)
				want = "campaigns: " + e.Error() + "\n"
			}
			if mode == "multiple" {
				want = "campaigns: 2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
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
				_, e = db.Open(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "campaigns.db"), Migrations: campaigns.Migrations()})
				if e == nil {
					t.Fatal("expected error")
				}
				want = "campaigns: cannot open database state/campaigns.db: " + strings.ReplaceAll(e.Error(), "\n", " ") + "\n"
				code = cli.ExitServerFailed
			}
			if got := cli.Run(context.Background(), p); got != code || out.Len() != 0 || errout.String() != want || errout.n != 1 {
				t.Fatal(got, out.String(), errout.String(), want, errout.n)
			}
		})
	}
	var _ error = &settings.Error{}
}
