package cli_test

import (
	"bytes"
	"context"
	"database/sql"
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
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/cli"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
)

type commandOutput struct {
	bytes.Buffer
	calls int
}

func (w *commandOutput) Write(b []byte) (int, error) { w.calls++; return w.Buffer.Write(b) }

type forbiddenCommandReader struct{ t *testing.T }

func (r forbiddenCommandReader) Read([]byte) (int, error) {
	r.t.Fatal("command read randomness")
	return 0, nil
}

type forbiddenCommandSink struct{ t *testing.T }

func (s forbiddenCommandSink) Deliver(context.Context, telemetry.Event) error {
	s.t.Fatal("command delivered telemetry")
	return nil
}

type forbiddenCommandEvents struct{ t *testing.T }

func (s forbiddenCommandEvents) Deliver(context.Context, events.Event) error {
	s.t.Fatal("command delivered bus event")
	return nil
}

// R-8WDR-ZS9M
const commandText = cli.Usage + cli.Manifest + cli.NginxConf

const commandExitSum = cli.ExitSuccess + cli.ExitServerFailed + cli.ExitUsage

func TestCommandConstants(t *testing.T) {
	if commandText == "" {
		t.Fatal("empty constant command text")
	}
	if commandExitSum != 3 || cli.ExitSuccess != 0 || cli.ExitServerFailed != 1 || cli.ExitUsage != 2 {
		t.Fatal("exit constants")
	}
	var _ uint8 = cli.ExitUsage
	var _ uint8 = cli.ExitSuccess
	var _ uint8 = cli.ExitServerFailed
	// R-8YTK-RBR0
	if !strings.Contains(cli.Manifest, "description = \""+pages.Description+"\"\n") {
		t.Fatal("description")
	}
	// R-901H-53HP R-919D-IV8E
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		b, err := fs.ReadFile(cron.Etc(), name)
		if err != nil || string(b) != want {
			t.Fatalf("%s: %q %v", name, b, err)
		}
	}
}

// R-92H9-WMZ3 R-93P6-AEPS R-94X2-O6GH R-964Z-1Y76 R-98KR-THOK R-99SO-79F9
// R-9B0K-L15Y R-9C8G-YSWN R-9DGD-CKNC R-9KRR-N73I R-8XLO-DK0B
func TestCommands(t *testing.T) {
	cases := []struct {
		args              []string
		version, out, arg string
	}{
		{args: []string{"--version"}, out: "\n"}, {args: []string{"--version"}, version: "test display", out: "test display\n"},
		{args: []string{"manifest"}, out: cli.Manifest}, {args: []string{"--help"}, out: cli.Usage},
		{args: []string{"bogus"}, arg: "bogus"}, {args: []string{"--bogus"}, arg: "--bogus"}, {args: []string{"db"}, arg: "db"}, {args: []string{"db", "bogus"}, arg: "bogus"},
		{args: []string{"db", "status", "extra"}, arg: "extra"}, {args: []string{"manifest", "x"}, arg: "x"}, {args: []string{"--help", "--x"}, arg: "--x"}, {args: []string{"--version", ""}, arg: ""},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "/"), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			var diag commandOutput
			p := cli.Process{Args: tc.args, Version: tc.version, Dir: dir, Stdout: &out, Stderr: &diag,
				LookupEnv: func(string) (string, bool) { t.Fatal("lookup"); return "", false }, Unsetenv: func(string) error { t.Fatal("unset"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit"); return nil, nil }, Now: func() time.Time { t.Fatal("clock"); return time.Time{} }, Sleep: func(context.Context, time.Duration) { t.Fatal("sleep") }, After: func(time.Duration) <-chan time.Time { t.Fatal("timer"); return nil }, Rand: forbiddenCommandReader{t}, Sink: forbiddenCommandSink{t}, EventSink: forbiddenCommandEvents{t}, Banner: func(page.User) page.Banner { t.Fatal("banner"); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { t.Fatal("mcp"); return nil }}
			code := cli.Run(context.Background(), p)
			want := cli.ExitSuccess
			if tc.out == "" {
				want = cli.ExitUsage
				kind := "command"
				if strings.HasPrefix(tc.arg, "-") {
					kind = "option"
				}
				expected := "cron: unknown " + kind + " '" + tc.arg + "'\n\nsee 'cron --help' for usage\n"
				if diag.String() != expected || diag.calls != 1 {
					t.Fatalf("diagnostic %q writes %d", diag.String(), diag.calls)
				}
			} else if diag.Len() != 0 {
				t.Fatal(diag.String())
			}
			if code != want || out.String() != tc.out {
				t.Fatalf("code %d output %q", code, out.String())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("command changed directory: %v %v", entries, err)
			}
			// Also prove the minimal Process sufficient, including without any inheritance hook.
			out.Reset()
			diag = commandOutput{}
			if c := cli.Run(context.Background(), cli.Process{Args: tc.args, Version: tc.version, Dir: dir, Stdout: &out, Stderr: &diag}); c != code || out.String() != tc.out {
				t.Fatal("minimal process differs")
			}
		})
	}
}

// R-9B0K-L15Y
// R-9EO9-QCE1 R-9FW6-444Q R-9H42-HVVF R-9IBY-VNM4 R-9JJV-9FCT
// R-9DGD-CKNC R-9KRR-N73I R-8XLO-DK0B
func TestDatabaseStatusCommand(t *testing.T) {
	for _, kind := range []string{"absent", "applied", "future", "invalid", "directory", "empty-dir"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			cfg := db.Config{Path: filepath.Join(dir, "state", "cron.db"), Migrations: cron.Migrations(), Now: func() time.Time { return time.Date(2026, 10, 5, 14, 3, 7, 123456000, time.UTC) }}
			switch kind {
			case "applied", "future":
				d, err := db.Open(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "future" {
					if err = d.Write(context.Background(), func(tx *sql.Tx) error {
						_, e := tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(2,'future timestamp')")
						return e
					}); err != nil {
						t.Fatal(err)
					}
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.MkdirAll(filepath.Dir(cfg.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Path, []byte("not SQLite"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.MkdirAll(cfg.Path, 0700); err != nil {
					t.Fatal(err)
				}
			case "empty-dir":
				t.Chdir(dir)
				cfg.Path = filepath.Join("state", "cron.db")
			}
			var expected bytes.Buffer
			statusErr := db.Status(context.Background(), cfg, &expected)
			var out bytes.Buffer
			var diag commandOutput
			processDir := dir
			if kind == "empty-dir" {
				processDir = ""
			}
			// Cancellation cannot turn a read-only command into a cancelled report.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			p := cli.Process{Args: []string{"db", "status"}, Dir: processDir, Stdout: &out, Stderr: &diag,
				LookupEnv: func(string) (string, bool) { t.Fatal("lookup"); return "", false }, Unsetenv: func(string) error { t.Fatal("unset"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit"); return nil, nil }, Now: func() time.Time { t.Fatal("clock"); return time.Time{} }, Sleep: func(context.Context, time.Duration) { t.Fatal("sleep") }, After: func(time.Duration) <-chan time.Time { t.Fatal("timer"); return nil }, Rand: forbiddenCommandReader{t}, Sink: forbiddenCommandSink{t}, EventSink: forbiddenCommandEvents{t}, Banner: func(page.User) page.Banner { t.Fatal("banner"); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { t.Fatal("mcp"); return nil }}
			code := cli.Run(ctx, p)
			var minimalOut bytes.Buffer
			var minimalDiag commandOutput
			minimalCode := cli.Run(ctx, cli.Process{Args: p.Args, Dir: processDir, Stdout: &minimalOut, Stderr: &minimalDiag})
			if minimalCode != code || minimalOut.String() != out.String() || minimalDiag.String() != diag.String() || minimalDiag.calls != diag.calls {
				t.Fatal("minimal status differs")
			}

			if out.String() != expected.String() {
				t.Fatalf("output %q want %q", out.String(), expected.String())
			}
			if statusErr == nil {
				if code != cli.ExitSuccess || diag.Len() != 0 {
					t.Fatalf("success %d %s", code, &diag)
				}
			} else {
				want := "cron: " + strings.ReplaceAll(statusErr.Error(), "\n", " ") + "\n"
				if code != cli.ExitServerFailed || diag.String() != want || diag.calls != 1 {
					t.Fatalf("error %d %q writes %d", code, diag.String(), diag.calls)
				}
			}
			var after bytes.Buffer
			afterErr := db.Status(context.Background(), cfg, &after)
			if after.String() != expected.String() || (afterErr == nil) != (statusErr == nil) {
				t.Fatal("status changed")
			}
			if kind == "absent" || kind == "empty-dir" {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("created state: %v %v", entries, err)
				}
			}
		})
	}
}

func TestFixedCommandText(t *testing.T) {
	// R-8SQ2-UH1J
	if cli.Usage != "Usage: cron [command]\n\nKeep triggers that emit events on the suite's event bus on a schedule,\nwith MCP tools at /mcp and a page of triggers at /, on the socket\nsystemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n" {
		t.Fatal("Usage differs")
	}
	// R-TAT2-PQNZ
	if cli.Manifest != "app = \"cron\"\ndescription = \""+pages.Description+"\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/cron.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n" {
		t.Fatal("Manifest differs")
	}
	// R-8V5V-M0IX
	if cli.NginxConf != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("NginxConf differs")
	}
}
