package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
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
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/cli"
	"github.com/ikigenba/ikigenba/gdrive/internal/pages"
	"github.com/ikigenba/ikigenba/gdrive/internal/settings"
)

const contractUsage = cli.Usage
const contractManifest = cli.Manifest
const contractNginx = cli.NginxConf
const contractDial = cli.DialTimeout
const contractTLS = cli.TLSHandshakeTimeout
const contractHeaders = cli.ResponseHeaderTimeout
const contractSweep = cli.SweepInterval

type writes struct {
	bytes.Buffer
	n int
}

func (w *writes) Write(b []byte) (int, error) { w.n++; return w.Buffer.Write(b) }

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("randomness used")
	return 0, errors.New("forbidden")
}

type forbiddenTransport struct{ t *testing.T }

func (r forbiddenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Fatal("HTTP used")
	return nil, errors.New("forbidden")
}

// R-A3WD-73LM R-A549-KVCB R-A6C5-YN30 R-A7K2-CETP R-A9ZV-3YB3
func TestCommands(t *testing.T) {
	var success, failed, usage float64 = cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage
	if success != 0 || failed != 1 || usage != 2 {
		t.Fatal("exit constants")
	}
	cases := []struct {
		args     []string
		out, arg string
	}{{[]string{"--version"}, "chosen\n", ""}, {[]string{"manifest"}, cli.Manifest, ""}, {[]string{"--help"}, cli.Usage, ""}, {[]string{"bogus"}, "", "bogus"}, {[]string{"--bogus"}, "", "--bogus"}, {[]string{"db"}, "", "db"}, {[]string{"db", "bogus"}, "", "bogus"}, {[]string{"db", "status", "extra"}, "", "extra"}, {[]string{"manifest", "extra"}, "", "extra"}, {[]string{"--version", "--extra"}, "", "--extra"}, {[]string{"--help", "extra"}, "", "extra"}}
	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			var stderr writes
			fail := func() { t.Fatal("process hook used") }
			p := cli.Process{Args: c.args, Dir: dir, Version: "chosen", Stdout: &out, Stderr: &stderr, LookupEnv: func(string) (string, bool) { fail(); return "", false }, Unsetenv: func(string) error { fail(); return nil }, Inherit: func(uintptr) (net.Listener, error) { fail(); return nil, nil }, Now: func() time.Time { fail(); return time.Time{} }, Sleep: func(context.Context, time.Duration) { fail() }, After: func(time.Duration) <-chan time.Time { fail(); return nil }, Rand: forbiddenReader{t}, HTTP: &http.Client{Transport: forbiddenTransport{t}}, Banner: func(page.User) page.Banner { fail(); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { fail(); return nil }, Sink: &telemetry.Capture{}, EventSink: &events.Capture{}}
			code := cli.Run(context.Background(), p)
			wantCode := cli.ExitSuccess
			wantErr := ""
			if c.arg != "" {
				wantCode = cli.ExitUsage
				kind := "command"
				if strings.HasPrefix(c.arg, "-") {
					kind = "option"
				}
				wantErr = fmt.Sprintf("gdrive: unknown %s '%s'\n\nsee 'gdrive --help' for usage\n", kind, c.arg)
				if stderr.n != 1 {
					t.Fatalf("writes %d", stderr.n)
				}
			}
			if code != wantCode || out.String() != c.out || stderr.String() != wantErr {
				t.Fatalf("%d %q %q", code, out.String(), stderr.String())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("directory changed %v %v", entries, err)
			}
			if len(p.Sink.(*telemetry.Capture).Events()) != 0 || len(p.EventSink.(*events.Capture).Events()) != 0 {
				t.Fatal("event delivered")
			}
		})
	}
	var out bytes.Buffer
	if cli.Run(context.Background(), cli.Process{Args: []string{"--version"}, Stdout: &out, Stderr: io.Discard}) != cli.ExitSuccess || out.String() != "\n" {
		t.Fatal("empty version")
	}
}

// R-9WKY-WH5G R-9XSV-A8W5 R-9Z0R-O0MU R-A08O-1SDJ R-A1GK-FK48 R-A2OG-TBUX
func TestContractFiles(t *testing.T) {
	if contractUsage != "Usage: gdrive [command]\n\nGoogle Drive files, pulled into and pushed from artifacts: MCP tools at\n/mcp record transfers that move the bytes on the host, with a page of\ntransfers at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n" {
		t.Fatal("usage")
	}
	want := `app = "gdrive"
description = "Google Drive files, pulled into and pushed from artifacts"
default = false
mcp = true
guests = false
secrets = []

[env]
TRANSFER_MAX_BYTES = "1073741824"
TRANSFER_MAX_ACTIVE = "2"
TRANSFER_MAX_QUEUED = "10"
TRANSFER_KEEP_DAYS = "15"
TRANSFER_KEEP_COUNT = "100"

[database]
engine = "sqlite"
path = "state/gdrive.db"

[resources]
memory_max = "128M"

[home]
group = "core"
`
	if contractManifest != want || !strings.Contains(cli.Manifest, `description = "`+pages.Description+`"`) {
		t.Fatal("manifest")
	}
	s := settings.Defaults()
	table := fmt.Sprintf("[env]\nTRANSFER_MAX_BYTES = \"%d\"\nTRANSFER_MAX_ACTIVE = \"%d\"\nTRANSFER_MAX_QUEUED = \"%d\"\nTRANSFER_KEEP_DAYS = \"%d\"\nTRANSFER_KEEP_COUNT = \"%d\"\n\n", s.TransferMaxBytes, s.TransferMaxActive, s.TransferMaxQueued, s.TransferKeepDays, s.TransferKeepCount)
	published := cli.Manifest
	if !strings.Contains(published, table) {
		t.Fatal("defaults")
	}
	if contractNginx != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("nginx")
	}
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, err := fs.ReadFile(gdrive.Etc(), name)
		if err != nil || string(got) != want {
			t.Fatalf("%s %v", name, err)
		}
	}
}

// R-AB7R-HQ1S R-ACFN-VHSH R-A9ZV-3YB3
func TestStatus(t *testing.T) {
	for _, kind := range []string{"empty", "database", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "gdrive.db")
			switch kind {
			case "database":
				d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: gdrive.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var want, out bytes.Buffer
			var stderr writes
			fail := func() { t.Fatal("process hook used by status") }
			capture, bus := &telemetry.Capture{}, &events.Capture{}
			process := cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &stderr, LookupEnv: func(string) (string, bool) { fail(); return "", false }, Unsetenv: func(string) error { fail(); return nil }, Inherit: func(uintptr) (net.Listener, error) { fail(); return nil, nil }, Now: func() time.Time { fail(); return time.Time{} }, Sleep: func(context.Context, time.Duration) { fail() }, After: func(time.Duration) <-chan time.Time { fail(); return nil }, Rand: forbiddenReader{t}, HTTP: &http.Client{Transport: forbiddenTransport{t}}, Banner: func(page.User) page.Banner { fail(); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { fail(); return nil }, Sink: capture, EventSink: bus}
			err := db.Status(context.Background(), db.Config{Path: path, Migrations: gdrive.Migrations()}, &want)
			code := cli.Run(context.Background(), process)
			if len(capture.Events()) != 0 || len(bus.Events()) != 0 {
				t.Fatal("status delivered events")
			}
			if out.String() != want.String() {
				t.Fatalf("stdout %q want %q", out.String(), want.String())
			}
			if err == nil {
				if code != cli.ExitSuccess || stderr.Len() != 0 {
					t.Fatalf("%d %q", code, stderr.String())
				}
			} else {
				if code != cli.ExitServerFailed || stderr.n != 1 || stderr.String() != "gdrive: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n" {
					t.Fatalf("%d %q %v", code, stderr.String(), err)
				}
			}
			if kind == "empty" {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal("status created files")
				}
			}
		})
	}
}

// R-QVJB-Q9NJ R-QWR8-41E8 R-QZ70-VKVM
func TestDefaultHTTP(t *testing.T) {
	if contractDial != 10*time.Second || contractTLS != 10*time.Second || contractHeaders != 60*time.Second || contractSweep != time.Hour {
		t.Fatal("durations")
	}
	a, b := cli.DefaultHTTP(), cli.DefaultHTTP()
	for _, c := range []*http.Client{a, b} {
		tr, ok := c.Transport.(*http.Transport)
		if !ok || tr == nil || tr == http.DefaultTransport || c.Timeout != 0 || tr.TLSHandshakeTimeout != cli.TLSHandshakeTimeout || tr.ResponseHeaderTimeout != cli.ResponseHeaderTimeout {
			t.Fatalf("client %+v", c)
		}
	}
	if a == b {
		t.Fatal("shared client")
	}
}
