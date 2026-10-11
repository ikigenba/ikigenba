package cli_test

import (
	"bytes"
	"context"
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
	"github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/cli"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
)

type commandOutput struct {
	bytes.Buffer
	calls int
}

func (w *commandOutput) Write(b []byte) (int, error) { w.calls++; return w.Buffer.Write(b) }

// R-ZLR1-YQYS R-ZO6U-QAG6 R-ZPER-426V R-ZQMN-HTXK
func TestCommandConstants(t *testing.T) {
	if cli.Usage != "Usage: artifacts [command]\n\nKeep files agents and services share, each put and fetched by path at\n/files/<path>, with MCP tools at /mcp and a page of artifacts at /, on\nthe socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n" {
		t.Fatal(cli.Usage)
	}
	if cli.Manifest != "app = \"artifacts\"\ndescription = \""+pages.Description+"\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nARTIFACT_MAX_BYTES = \"1073741824\"\nIMPORT_SECONDS = \"600\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/artifacts.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n" {
		t.Fatal(cli.Manifest)
	}
	if cli.NginxConf != "client_max_body_size    0;\nproxy_request_buffering off;\nproxy_http_version      1.1;\nproxy_buffering         on;\nproxy_read_timeout      3600s;\nproxy_send_timeout      3600s;\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal(cli.NginxConf)
	}
	var _ uint8 = cli.ExitSuccess
	var _ uint8 = cli.ExitServerFailed
	var _ uint8 = cli.ExitUsage
	if cli.ExitSuccess != 0 || cli.ExitServerFailed != 1 || cli.ExitUsage != 2 {
		t.Fatal("exit values")
	}
}

// R-ZRUJ-VLO9
func TestEtcConstants(t *testing.T) {
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		b, e := fs.ReadFile(artifacts.Etc(), name)
		if e != nil || string(b) != want {
			t.Fatal(name, string(b), e)
		}
	}
}

// R-ZT2G-9DEY
func TestPrintCommands(t *testing.T) {
	for command, want := range map[string]string{"--version": "test-display\n", "manifest": cli.Manifest, "--help": cli.Usage} {
		var out, err commandOutput
		code := cli.Run(context.Background(), cli.Process{Args: []string{command}, Version: "test-display", Dir: t.TempDir(), Stdout: &out, Stderr: &err, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit"); return nil, nil }})
		if code != cli.ExitSuccess || out.String() != want || err.Len() != 0 {
			t.Fatal(code, out.String(), err.String())
		}
	}
}

// R-ZUAC-N55N R-ZVI9-0WWC
func TestUsageErrors(t *testing.T) {
	for _, c := range []struct {
		args []string
		bad  string
	}{{[]string{"bogus", "ignored"}, "bogus"}, {[]string{"--unknown"}, "--unknown"}, {[]string{"db"}, "db"}, {[]string{"db", "invalid"}, "invalid"}, {[]string{"db", "status", "--extra"}, "--extra"}, {[]string{"--help", "more"}, "more"}, {[]string{"manifest", "--extra"}, "--extra"}, {[]string{"--version", "more", "last"}, "more"}} {
		var out, err commandOutput
		code := cli.Run(context.Background(), cli.Process{Args: c.args, Dir: t.TempDir(), Stdout: &out, Stderr: &err, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit"); return nil, nil }})
		kind := "command"
		if strings.HasPrefix(c.bad, "-") {
			kind = "option"
		}
		want := "artifacts: unknown " + kind + " '" + c.bad + "'\n\nsee 'artifacts --help' for usage\n"
		if code != cli.ExitUsage || out.Len() != 0 || err.String() != want || err.calls != 1 {
			t.Fatal(c.args, code, out.String(), err.String(), err.calls)
		}
	}
}

type forbiddenRead struct{ t *testing.T }

func (f forbiddenRead) Read([]byte) (int, error) { f.t.Fatal("rand read"); return 0, nil }

// R-ZWQ5-EON1
func TestCommandsAvoidRuntime(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"--help"}, {"db", "status"}, {"unknown"}} {
		dir := t.TempDir()
		marker := filepath.Join(dir, "marker")
		if e := os.WriteFile(marker, []byte("unchanged"), 0600); e != nil {
			t.Fatal(e)
		}
		var out, err bytes.Buffer
		fail := func() { t.Fatal("runtime seam used") }
		p := cli.Process{Args: args, Dir: dir, Version: "display", Stdout: &out, Stderr: &err, LookupEnv: func(string) (string, bool) { fail(); return "", false }, Unsetenv: func(string) error { fail(); return nil }, Inherit: func(uintptr) (net.Listener, error) { fail(); return nil, nil }, Now: func() time.Time { fail(); return time.Time{} }, Sleep: func(context.Context, time.Duration) { fail() }, After: func(time.Duration) <-chan time.Time { fail(); return nil }, ImportAfter: func(time.Duration) <-chan time.Time { fail(); return nil }, Rand: forbiddenRead{t}, Sink: forbiddenTelemetry{t}, EventSink: forbiddenEvents{t}, Banner: func(page.User) page.Banner { fail(); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { fail(); return nil }}
		_ = cli.Run(context.Background(), p)
		entries, e := os.ReadDir(dir)
		b, readErr := fs.ReadFile(os.DirFS(dir), "marker")
		if e != nil || readErr != nil || len(entries) != 1 || string(b) != "unchanged" {
			t.Fatal(entries, e, readErr, string(b))
		}
	}
}

// R-ZXY1-SGDQ
func TestDatabaseStatus(t *testing.T) {
	for _, state := range []string{"absent", "ready", "invalid"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "artifacts.db")
			if state == "ready" {
				d, e := db.Open(context.Background(), db.Config{Path: path, Migrations: artifacts.Migrations(), Now: func() time.Time { return time.Unix(42, 0) }})
				if e != nil {
					t.Fatal(e)
				}
				if e = d.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if state == "invalid" {
				if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(path, []byte("invalid database"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			var want, out bytes.Buffer
			expected := db.Status(context.Background(), db.Config{Path: path, Migrations: artifacts.Migrations()}, &want)
			var err commandOutput
			code := cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &err, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit"); return nil, nil }})
			if out.String() != want.String() {
				t.Fatal(out.String(), want.String())
			}
			if expected == nil {
				if code != cli.ExitSuccess || err.Len() != 0 {
					t.Fatal(code, err.String())
				}
			} else if code != cli.ExitServerFailed || err.calls != 1 || err.String() != "artifacts: "+strings.ReplaceAll(expected.Error(), "\n", " ")+"\n" {
				t.Fatal(code, err.String(), expected)
			}
			if state == "absent" {
				entries, e := os.ReadDir(dir)
				if e != nil || len(entries) != 0 {
					t.Fatal(entries, e)
				}
			}
			if _, e := os.Stat(filepath.Join(dir, "state", "blobs")); !os.IsNotExist(e) {
				t.Fatal(e)
			}
		})
	}
}

type forbiddenTelemetry struct{ t *testing.T }

func (s forbiddenTelemetry) Deliver(context.Context, telemetry.Event) error {
	s.t.Fatal("telemetry delivery")
	return nil
}

type forbiddenEvents struct{ t *testing.T }

func (s forbiddenEvents) Deliver(context.Context, events.Event) error {
	s.t.Fatal("bus delivery")
	return nil
}
