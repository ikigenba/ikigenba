package cli_test

import (
	"bytes"
	"context"
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
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/pages"
	"github.com/ikigenba/ikigenba/gmail/internal/settings"
)

type recordingWriter struct {
	bytes.Buffer
	calls int
}

func (w *recordingWriter) Write(p []byte) (int, error) { w.calls++; return w.Buffer.Write(p) }

type forbiddenIO struct{ t *testing.T }

func (f forbiddenIO) Read([]byte) (int, error) { f.t.Error("unexpected random read"); return 0, io.EOF }
func (f forbiddenIO) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("unexpected HTTP call")
	return nil, fmt.Errorf("forbidden")
}

type forbiddenTrail struct{ t *testing.T }

func (f forbiddenTrail) Deliver(context.Context, telemetry.Event) error {
	f.t.Error("unexpected telemetry call")
	return nil
}

type forbiddenBus struct{ t *testing.T }

func (f forbiddenBus) Deliver(context.Context, events.Event) error {
	f.t.Error("unexpected event call")
	return nil
}

func commandProcess(t *testing.T, args []string, out, errout io.Writer) cli.Process {
	t.Helper()
	return cli.Process{Args: args, Stdout: out, Stderr: errout, Dir: t.TempDir(), Version: "chosen-display",
		LookupEnv: func(string) (string, bool) { t.Error("environment looked up"); return "", false },
		Unsetenv:  func(string) error { t.Error("environment unset"); return nil },
		Inherit: func(uintptr) (net.Listener, error) {
			t.Error("listener inherited")
			return nil, fmt.Errorf("forbidden")
		},
		Now:    func() time.Time { t.Error("clock read"); return time.Time{} },
		Sleep:  func(context.Context, time.Duration) { t.Error("sleep called") },
		After:  func(time.Duration) <-chan time.Time { t.Error("timer called"); return nil },
		Banner: func(page.User) page.Banner { t.Error("banner called"); return page.Banner{} },
		MCP:    func(*telemetry.Writer) *mcp.Server { t.Error("MCP constructed"); return nil },
		Rand:   forbiddenIO{t}, HTTP: &http.Client{Transport: forbiddenIO{t}}, Sink: forbiddenTrail{t}, EventSink: forbiddenBus{t},
	}
}

// R-WHU5-V54U R-WJ22-8WVJ R-WLHV-0GCX
func TestManifestConsistency(t *testing.T) {
	if !strings.Contains(cli.Manifest, "description = \""+pages.Description+"\"\n") {
		t.Fatal("description differs")
	}
	s := settings.Defaults()
	want := fmt.Sprintf("[env]\nTRANSFER_MAX_BYTES = \"%d\"\nTRANSFER_MAX_ACTIVE = \"%d\"\nTRANSFER_MAX_QUEUED = \"%d\"\nTRANSFER_KEEP_DAYS = \"%d\"\nTRANSFER_KEEP_COUNT = \"%d\"\nDRAFT_KEEP_DAYS = \"%d\"\nDRAFT_KEEP_COUNT = \"%d\"\n\n", s.TransferMaxBytes, s.TransferMaxActive, s.TransferMaxQueued, s.TransferKeepDays, s.TransferKeepCount, s.DraftKeepDays, s.DraftKeepCount)
	start := strings.Index(cli.Manifest, "[env]\n")
	if start < 0 {
		t.Fatal("env missing")
	}
	end := strings.Index(cli.Manifest[start:], "\n\n")
	if end < 0 {
		t.Fatal("env terminator missing")
	}
	if got := cli.Manifest[start : start+end+2]; got != want {
		t.Fatalf("env = %q, want %q", got, want)
	}
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, err := fs.ReadFile(app.Etc(), name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("embedded %s differs", name)
		}
	}
}

// R-WMPR-E83M R-WNXN-RZUB R-WRLC-XB2E
func TestCommands(t *testing.T) {
	const success uint8 = cli.ExitSuccess
	const failed uint8 = cli.ExitServerFailed
	const usage uint8 = cli.ExitUsage
	if success != 0 || failed != 1 || usage != 2 {
		t.Fatal("exit constants differ")
	}
	for _, test := range []struct {
		args          []string
		version, want string
	}{{[]string{"--version"}, "chosen-display", "chosen-display\n"}, {[]string{"--version"}, "", "\n"}, {[]string{"manifest"}, "", cli.Manifest}, {[]string{"--help"}, "", cli.Usage}} {
		var out, errout recordingWriter
		p := commandProcess(t, test.args, &out, &errout)
		p.Version = test.version
		if code := cli.Run(context.Background(), p); code != cli.ExitSuccess {
			t.Fatalf("code = %d", code)
		}
		if out.String() != test.want || errout.Len() != 0 {
			t.Fatalf("streams = %q, %q", out.String(), errout.String())
		}
		entries, err := os.ReadDir(p.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatal("command changed directory")
		}
		// The command also needs no serving fields at all.
		out.Reset()
		errout.Reset()
		minimal := cli.Process{Args: test.args, Version: test.version, Stdout: &out, Stderr: &errout, Dir: p.Dir}
		if code := cli.Run(context.Background(), minimal); code != cli.ExitSuccess || out.String() != test.want || errout.Len() != 0 {
			t.Fatalf("minimal process: code %d, streams %q %q", code, out.String(), errout.String())
		}
	}
}

// R-WP5K-5RL0 R-WQDG-JJBP R-WRLC-XB2E
func TestCommandErrors(t *testing.T) {
	for _, test := range []struct {
		args      []string
		offending string
	}{{[]string{"bogus"}, "bogus"}, {[]string{"--bogus"}, "--bogus"}, {[]string{"db"}, "db"}, {[]string{"db", "bogus"}, "bogus"}, {[]string{"db", "status", "extra"}, "extra"}, {[]string{"manifest", "extra"}, "extra"}, {[]string{"--version", "--extra"}, "--extra"}, {[]string{"--help", "other", "more"}, "other"}, {[]string{"unknown", "--later"}, "unknown"}, {[]string{"db", "--bad", "later"}, "--bad"}, {[]string{""}, ""}} {
		var out, errout recordingWriter
		p := commandProcess(t, test.args, &out, &errout)
		kind := "command"
		if strings.HasPrefix(test.offending, "-") {
			kind = "option"
		}
		want := "gmail: unknown " + kind + " '" + test.offending + "'\n\nsee 'gmail --help' for usage\n"
		if code := cli.Run(context.Background(), p); code != cli.ExitUsage {
			t.Fatalf("%v code = %d", test.args, code)
		}
		if out.Len() != 0 || errout.String() != want || errout.calls != 1 {
			t.Fatalf("%v streams = %q, %q (%d writes)", test.args, out.String(), errout.String(), errout.calls)
		}
		entries, err := os.ReadDir(p.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatal("error changed directory")
		}
	}
}

// R-WST9-B2T3 R-WU15-OUJS R-WRLC-XB2E R-WMPR-E83M
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"empty", "seeded", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var out, errout recordingWriter
			p := commandProcess(t, []string{"db", "status"}, &out, &errout)
			path := filepath.Join(p.Dir, "state", "gmail.db")
			switch mode {
			case "seeded":
				d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: app.Migrations(), Now: func() time.Time { return time.Unix(1700000000, 0) }})
				if err != nil {
					t.Fatal(err)
				}
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var want bytes.Buffer
			expectedErr := db.Status(context.Background(), db.Config{Path: path, Migrations: app.Migrations()}, &want)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code := cli.Run(ctx, p)
			if out.String() != want.String() {
				t.Fatalf("status = %q, want %q", out.String(), want.String())
			}
			if expectedErr == nil {
				if code != cli.ExitSuccess || errout.Len() != 0 {
					t.Fatalf("code/diagnostic = %d %q", code, errout.String())
				}
			} else {
				if code != cli.ExitServerFailed || errout.calls != 1 || errout.String() != "gmail: "+strings.ReplaceAll(expectedErr.Error(), "\n", " ")+"\n" {
					t.Fatalf("code/diagnostic = %d %q (%d)", code, errout.String(), errout.calls)
				}
			}
			if mode == "empty" {
				entries, err := os.ReadDir(p.Dir)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatal("status created database")
				}
			}
			out.Reset()
			errout.Reset()
			minimal := cli.Process{Args: p.Args, Stdout: &out, Stderr: &errout, Dir: p.Dir}
			if minimalCode := cli.Run(context.Background(), minimal); minimalCode != code || out.String() != want.String() {
				t.Fatalf("minimal db status: code %d, output %q", minimalCode, out.String())
			}
		})
	}
}

// R-WE6G-PTWR
func TestUsageConstant(t *testing.T) {
	const want = "Usage: gmail [command]\n\nGmail for agents: MCP tools at /mcp read mail, compose and send drafts,\nand record transfers that move attachments on the host, with a page of\ndrafts and transfers at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	if cli.Usage != want {
		t.Fatalf("constant mismatch: %q", cli.Usage)
	}
}

// R-WGM9-HDE5
func TestManifestConstant(t *testing.T) {
	const want = "app = \"gmail\"\ndescription = \"Gmail messages read, attachments pulled into artifacts, drafts composed and sent\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nTRANSFER_MAX_BYTES = \"52428800\"\nTRANSFER_MAX_ACTIVE = \"2\"\nTRANSFER_MAX_QUEUED = \"10\"\nTRANSFER_KEEP_DAYS = \"15\"\nTRANSFER_KEEP_COUNT = \"100\"\nDRAFT_KEEP_DAYS = \"15\"\nDRAFT_KEEP_COUNT = \"100\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/gmail.db\"\n\n[resources]\nmemory_max = \"256M\"\n\n[home]\ngroup = \"core\"\n"
	if cli.Manifest != want {
		t.Fatalf("constant mismatch: %q", cli.Manifest)
	}
}

// R-WK9Y-MOM8
func TestNginxConfConstant(t *testing.T) {
	const want = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"
	if cli.NginxConf != want {
		t.Fatalf("constant mismatch: %q", cli.NginxConf)
	}
}
