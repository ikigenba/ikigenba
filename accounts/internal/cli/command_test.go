package cli_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const (
	_         = cli.Usage
	_         = cli.Manifest
	_         = cli.NginxConf
	_ int8    = cli.ExitSuccess
	_ uint64  = cli.ExitServerFailed
	_ float64 = cli.ExitUsage
	_ uint64  = cli.ExitSuccess
	_ float64 = cli.ExitSuccess
	_ int8    = cli.ExitServerFailed
	_ float64 = cli.ExitServerFailed
	_ int8    = cli.ExitUsage
	_ uint64  = cli.ExitUsage
)

// R-C9ZY-YLL2 R-CB7V-CDBR
var _ func(context.Context, cli.Process) int = cli.Run
var _ = struct {
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
}(cli.Process{})

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

func inert(t *testing.T, args []string) (cli.Process, *writes, *writes) {
	t.Helper()
	out, errout := &writes{}, &writes{}
	p := cli.Process{Args: args, Dir: t.TempDir(), Stdout: out, Stderr: errout, Version: "test identity", LookupEnv: func(string) (string, bool) { t.Fatal("environment used"); return "", false }, Unsetenv: func(string) error { t.Fatal("unset used"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit used"); return nil, nil }, Now: func() time.Time { t.Fatal("clock used"); return time.Time{} }, Sleep: func(context.Context, time.Duration) { t.Fatal("pause used") }, Rand: forbidden{t}, Sink: noTelemetry{t}, Banner: func(page.User) page.Banner { t.Fatal("banner used"); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP used"); return nil }}
	return p, out, errout
}

// R-S0NP-MJ4U R-S1VM-0AVJ R-S33I-E2M8 R-S5JB-5M3M R-V7AZ-FWPF R-S7Z3-X5L0 R-S970-AXBP R-SAEW-OP2E R-CB7V-CDBR
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
			want = "accounts: unknown " + kind + " '" + c.arg + "'\n\nsee 'accounts --help' for usage\n"
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

// R-V4V6-OD81 R-E307-S9VA R-M6FC-GN0B
func TestManifestAndFragment(t *testing.T) {
	want := "app = \"accounts\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/accounts.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"
	if cli.Manifest != want {
		t.Fatal("manifest bytes")
	}
	if cli.NginxConf != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("fragment bytes")
	}
}

// R-SBMT-2GT3 R-SCUP-G8JS R-SE2L-U0AH R-SFAI-7S16
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"empty", "database", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			p, out, errout := inert(t, []string{"db", "status"})
			cfg := db.Config{Path: filepath.Join(p.Dir, "state", "accounts.db"), Migrations: accounts.Migrations()}
			switch mode {
			case "database":
				d, e := db.Open(context.Background(), db.Config{Path: cfg.Path, Migrations: cfg.Migrations, Now: func() time.Time { return fixedTime }})
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
				if code != cli.ExitServerFailed || errout.String() != "accounts: "+strings.ReplaceAll(beforeErr.Error(), "\n", " ")+"\n" || errout.n != 1 {
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

// R-DBSU-THKL R-QXPU-XGK3 R-R05N-P01H
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
			want := "accounts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
			code := cli.ExitUsage
			if strings.HasPrefix(mode, "setting") {
				_, e := settings.Read(p.LookupEnv)
				want = "accounts: " + e.Error() + "\n"
			}
			if mode == "multiple" {
				want = "accounts: 2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
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
				_, e = db.Open(context.Background(), db.Config{Path: filepath.Join(p.Dir, "state", "accounts.db"), Migrations: accounts.Migrations()})
				if e == nil {
					t.Fatal("expected error")
				}
				want = "accounts: cannot open database state/accounts.db: " + strings.ReplaceAll(e.Error(), "\n", " ") + "\n"
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

// R-QXPU-XGK3
func TestNoSocketVariants(t *testing.T) {
	for _, values := range []map[string]string{
		{}, {"LISTEN_FDS": "1"}, {"LISTEN_PID": "other", "LISTEN_FDS": "1"}, {"LISTEN_PID": "42"},
		{"LISTEN_PID": "42", "LISTEN_FDS": ""}, {"LISTEN_PID": "42", "LISTEN_FDS": "0"}, {"LISTEN_PID": "42", "LISTEN_FDS": "00"},
		{"LISTEN_PID": "42", "LISTEN_FDS": "one"}, {"LISTEN_PID": "42", "LISTEN_FDS": "+1"}, {"LISTEN_PID": "42", "LISTEN_FDS": "-1"}, {"LISTEN_PID": "42", "LISTEN_FDS": "1 "},
		{"LISTEN_PID": "42", "LISTEN_FDS": "02"}, {"LISTEN_PID": "42", "LISTEN_FDS": "999999999999999999999999999999"},
	} {
		p, out, errout := inert(t, nil)
		p.Pid = 42
		p.LookupEnv = func(k string) (string, bool) {
			switch k {
			case "DRAIN_SECONDS", "ACCOUNTS_HOST_COMMAND", "IKIGENBA_SANDBOX", "LISTEN_PID", "LISTEN_FDS":
			default:
				t.Error("unexpected lookup", k)
			}
			v, ok := values[k]
			return v, ok
		}
		want := "accounts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
		if fds := values["LISTEN_FDS"]; fds == "02" || strings.HasPrefix(fds, "999") {
			want = "accounts: " + fds + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
		}
		if code := cli.Run(context.Background(), p); code != cli.ExitUsage || out.Len() != 0 || errout.String() != want || errout.n != 1 {
			t.Fatal(values, code, out.String(), errout.String())
		}
		entries, e := os.ReadDir(p.Dir)
		if e != nil || len(entries) != 0 {
			t.Fatal(entries, e)
		}
	}
}
