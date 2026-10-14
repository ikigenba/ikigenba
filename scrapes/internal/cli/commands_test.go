package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
)

type recordingWriter struct {
	bytes.Buffer
	calls int
}

func (w *recordingWriter) Write(p []byte) (int, error) { w.calls++; return w.Buffer.Write(p) }

func (w *recordingWriter) Reset() { w.Buffer.Reset(); w.calls = 0 }

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

// R-CWPG-TCYT R-G7Z1-ZK5D
func TestManifestConsistency(t *testing.T) {
	if !strings.Contains(cli.Manifest, "description = \""+pages.Description+"\"\n") {
		t.Fatal("description differs")
	}
	s := settings.Defaults()
	want := fmt.Sprintf("[env]\nSCRAPE_KEEP_RUNS = \"%d\"\nSCRAPE_WRITE_BUDGET = \"%d\"\nSCRAPE_PRESSURE_LIMIT = \"%d\"\nFMCSA_STATES = \"%s\"\nFMCSA_MIN_POWER_UNITS = \"%d\"\nFMCSA_STATUS = \"%s\"\nPPP_FILES = \"%s\"\nPPP_STATES = \"%s\"\nMN_PARCELS_COUNTIES = \"%s\"\n\n", s.KeepRuns, s.WriteBudget, s.PressureLimit, strings.Join(s.FMCSAStates, ","), s.FMCSAMinPowerUnits, s.FMCSAStatus, strings.Join(s.PPPFiles, ","), strings.Join(s.PPPStates, ","), strings.Join(s.MNParcelsCounties, ","))
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
}

// R-D0D5-YO6W R-D1L2-CFXL R-D58R-HR5O
func TestCommands(t *testing.T) {
	const success uint8 = cli.ExitSuccess
	const failed uint8 = cli.ExitServerFailed
	const usage uint8 = cli.ExitUsage
	const successFloat float64 = cli.ExitSuccess
	const failedFloat float64 = cli.ExitServerFailed
	const usageFloat float64 = cli.ExitUsage
	if success != 0 || failed != 1 || usage != 2 || successFloat != 0 || failedFloat != 1 || usageFloat != 2 {
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

// R-D2SY-Q7OA R-D40V-3ZEZ R-D58R-HR5O
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
		want := "scrapes: unknown " + kind + " '" + test.offending + "'\n\nsee 'scrapes --help' for usage\n"
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

// R-0LA3-O8TQ R-0MI0-20KF R-D58R-HR5O R-D0D5-YO6W
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"empty", "seeded", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var out, errout recordingWriter
			p := commandProcess(t, []string{"db", "status"}, &out, &errout)
			path := filepath.Join(p.Dir, "state", "scrapes.db")
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
				if code != cli.ExitServerFailed || errout.calls != 1 || errout.String() != "scrapes: "+strings.ReplaceAll(expectedErr.Error(), "\n", " ")+"\n" {
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
			if minimalCode := cli.Run(context.Background(), minimal); minimalCode != code || out.String() != want.String() || (expectedErr == nil && errout.Len() != 0) || (expectedErr != nil && (errout.calls != 1 || errout.String() != "scrapes: "+strings.ReplaceAll(expectedErr.Error(), "\n", " ")+"\n")) {
				t.Fatalf("minimal db status: code %d, output %q", minimalCode, out.String())
			}
		})
	}
}

// R-CU9O-1THF
func TestUsageConstant(t *testing.T) {
	const want = "Usage: scrapes [command]\n\nPublic records for agents: MCP tools at /mcp start scrapers and read\ntheir runs and rows, with a page of runs at /, and each run's rows\nstreamed to siblings, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	const got = cli.Usage
	if got != want {
		t.Fatalf("constant mismatch: %q", cli.Usage)
	}
}

// R-G6R5-LSEO
func TestManifestConstant(t *testing.T) {
	const want = "app = \"scrapes\"\ndescription = \"Public records about businesses in range, collected per source as rows per run\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nSCRAPE_KEEP_RUNS = \"2\"\nSCRAPE_WRITE_BUDGET = \"4096000\"\nSCRAPE_PRESSURE_LIMIT = \"10\"\nFMCSA_STATES = \"MN,WI\"\nFMCSA_MIN_POWER_UNITS = \"5\"\nFMCSA_STATUS = \"A\"\nPPP_FILES = \"public_150k_plus_240930.csv\"\nPPP_STATES = \"MN,WI\"\nMN_PARCELS_COUNTIES = \"Carlton,Chisago,Isanti,Mille Lacs,Aitkin,St. Louis,Anoka,Washington,Sherburne,Stearns,Benton,Morrison,Crow Wing\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/scrapes.db\"\n\n[resources]\nmemory_max = \"256M\"\n\n[home]\ngroup = \"application\"\n"
	const got = cli.Manifest
	if got != want {
		t.Fatalf("constant mismatch: %q", cli.Manifest)
	}
}

// R-6ZOZ-QI3C
func TestNginxConfConstant(t *testing.T) {
	const want = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"
	const got = cli.NginxConf
	if got != want {
		t.Fatalf("constant mismatch: %q", cli.NginxConf)
	}
}

// R-D58R-HR5O
func TestCommandsPreserveExistingFiles(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}, {"manifest"}, {"db"}, {"db", "status", "extra"}, {"bogus"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			var out, errout recordingWriter
			p := commandProcess(t, args, &out, &errout)
			nested := filepath.Join(p.Dir, "owned")
			if err := os.Mkdir(nested, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(nested, "fixture")
			content := []byte("caller supplied bytes")
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatal(err)
			}
			code := cli.Run(context.Background(), p)
			if code != cli.ExitSuccess && code != cli.ExitUsage {
				t.Fatalf("code = %d", code)
			}
			root, err := os.OpenRoot(p.Dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			got, err := root.ReadFile("owned/fixture")
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("fixture changed: %q, %v", got, err)
			}
			entries, err := os.ReadDir(p.Dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "owned" {
				t.Fatalf("root changed: %v, %v", entries, err)
			}
			entries, err = os.ReadDir(nested)
			if err != nil || len(entries) != 1 || entries[0].Name() != "fixture" {
				t.Fatalf("nested directory changed: %v, %v", entries, err)
			}
		})
	}
}
