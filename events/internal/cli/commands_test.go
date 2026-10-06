package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/cli"
	"github.com/ikigenba/ikigenba/events/internal/pages"
	"github.com/ikigenba/ikigenba/events/internal/settings"
)

type writes struct {
	bytes.Buffer
	Calls int
}

func (w *writes) Write(p []byte) (int, error)       { w.Calls++; return w.Buffer.Write(p) }
func (w *writes) WriteString(p string) (int, error) { return w.Write([]byte(p)) }

// R-ZTIK-BU7H R-ZUQG-PLY6 R-0AL5-OML7 R-0BT2-2EBW R-0D0Y-G62L R-0E8U-TXTA
// R-0GON-LHAO R-0HWJ-Z91D R-0KCC-QSIR R-0LK9-4K9G
func TestConstants(t *testing.T) {
	versionPointer := &cli.Version
	if *versionPointer != cli.Version {
		t.Fatal("version variable")
	}
	const usageConstant = cli.Usage
	const manifestConstant = cli.Manifest
	const nginxConstant = cli.NginxConf
	if usageConstant != cli.Usage || manifestConstant != cli.Manifest || nginxConstant != cli.NginxConf {
		t.Fatal("constant values")
	}
	type definedExit uint8
	var success definedExit = cli.ExitSuccess
	var failure definedExit = cli.ExitFailure
	var usage definedExit = cli.ExitUsage
	if success != 0 || failure != 1 || usage != 2 {
		t.Fatal("untyped constants")
	}

	if !regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`).MatchString(cli.Version) {
		t.Fatal(cli.Version)
	}
	const exitExpression = cli.ExitSuccess + cli.ExitFailure + cli.ExitUsage
	if exitExpression != 3 || cli.ExitSuccess != 0 || cli.ExitFailure != 1 || cli.ExitUsage != 2 {
		t.Fatal("exit values")
	}
	wantUsage := "Usage: events [command]\n\nServe the suite's internal event bus: emit at /emit, MCP tools at /mcp, and a\nlanding page at /, on the socket systemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	if cli.Usage != wantUsage {
		t.Fatal(cli.Usage)
	}
	wantManifest := "app = \"events\"\ndescription = \"The suite's internal event bus\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nEVENTS_DEPTH_MAX = \"8\"\nEVENTS_DELIVERY_TIMEOUT_SECONDS = \"5\"\nEVENTS_DELIVERY_ATTEMPTS = \"10\"\nEVENTS_INFLIGHT_MAX = \"4\"\nEVENTS_RETENTION_DAYS = \"2\"\nEVENTS_DECLARATIONS_SECONDS = \"60\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/events.db\"\n"
	if cli.Manifest != wantManifest || cli.NginxConf != "location = /emit { return 404; }\n" {
		t.Fatal("deployment constants")
	}
	manifest := cli.Manifest
	if !strings.Contains(manifest, "description = "+strconv.Quote(pages.Description)+"\n") {
		t.Fatal("description")
	}
	d := settings.Defaults()
	values := []int64{d.DepthMax, d.DeliveryTimeoutSeconds, d.DeliveryAttempts, d.InflightMax, d.RetentionDays, d.DeclarationsSeconds}
	keys := []string{"EVENTS_DEPTH_MAX", "EVENTS_DELIVERY_TIMEOUT_SECONDS", "EVENTS_DELIVERY_ATTEMPTS", "EVENTS_INFLIGHT_MAX", "EVENTS_RETENTION_DAYS", "EVENTS_DECLARATIONS_SECONDS"}
	var env strings.Builder
	env.WriteString("[env]\n")
	for i, key := range keys {
		env.WriteString(key + " = " + strconv.Quote(strconv.FormatInt(values[i], 10)) + "\n")
	}
	env.WriteString("\n")
	if !strings.Contains(manifest, env.String()) {
		t.Fatal("defaults")
	}
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, err := fs.ReadFile(events.Etc(), "etc/"+name)
		if err != nil || string(got) != want {
			t.Fatal(name, string(got), err)
		}
	}
}

// R-0FGR-7PJZ R-0MS5-IC05 R-0O01-W3QU R-0P7Y-9VHJ R-0QFU-NN88
// R-0RNR-1EYX R-0SVN-F6PM R-0U3J-SYGB R-0VBG-6Q70 R-12MU-HCN6
func TestCommands(t *testing.T) {
	cases := []struct {
		args     []string
		out, err string
		code     int
	}{
		{[]string{"--version"}, cli.Version + "\n", "", 0}, {[]string{"manifest"}, cli.Manifest, "", 0}, {[]string{"--help"}, cli.Usage, "", 0},
	}
	for _, args := range [][]string{{"bogus"}, {"--bogus"}, {"db"}, {"db", "bogus"}, {"db", "status", "extra"}, {"manifest", "extra"}, {"--version", "-x"}, {"--help", "extra", "more"}} {
		arg := args[0]
		if len(args) > 1 {
			arg = args[1]
		}
		if len(args) > 2 && args[0] == "db" && args[1] == "status" {
			arg = args[2]
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		cases = append(cases, struct {
			args     []string
			out, err string
			code     int
		}{args, "", "events: unknown " + kind + " '" + arg + "'\n\nsee 'events --help' for usage\n", 2})
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			var errw writes
			p := guardedProcess(t)
			p.Args, p.Stdout, p.Stderr, p.Dir = tc.args, &out, &errw, dir

			code := cli.Run(context.Background(), p)
			if code != tc.code || out.String() != tc.out || errw.String() != tc.err {
				t.Fatalf("got %d %q %q", code, out.String(), errw.String())
			}
			if tc.code != 0 && errw.Calls != 1 {
				t.Fatal(errw.Calls)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal(entries, err)
			}
		})
	}
}

// R-Q26R-L9UR R-0XR8-Y9OE R-0YZ5-C1F3 R-Q3EN-Z1LG R-HM3M-MEVY R-11EY-3KWH
func TestDatabaseStatus(t *testing.T) {
	for _, kind := range []string{"absent", "empty_state", "valid", "newer", "bad"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "events.db")
			if kind == "empty_state" || kind == "bad" {
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "valid" || kind == "newer" {
				d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: events.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
				if err != nil {
					t.Fatal(err)
				}
				if kind == "newer" {
					err = d.Write(context.Background(), func(tx *sql.Tx) error {
						_, e := tx.ExecContext(context.Background(), "INSERT INTO schema_migrations(version, applied_at) VALUES (2, 'future')")
						return e
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "bad" {
				if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var want bytes.Buffer
			reference := db.Status(context.Background(), db.Config{Path: path, Migrations: events.Migrations()}, &want)
			var out bytes.Buffer
			var errw writes
			p := guardedProcess(t)
			p.Args, p.Dir, p.Stdout, p.Stderr = []string{"db", "status"}, dir, &out, &errw
			code := cli.Run(context.Background(), p)
			if out.String() != want.String() {
				t.Fatal(out.String(), want.String())
			}
			if reference == nil {
				if code != 0 || errw.Len() != 0 {
					t.Fatal(code, errw.String())
				}
			} else {
				if code != 1 || errw.Calls != 1 || errw.String() != "events: "+reference.Error()+"\n" {
					t.Fatal(code, errw.String(), reference)
				}
				if kind == "newer" && !strings.Contains(errw.String(), "0002") {
					t.Fatal(errw.String())
				}
			}
			var after bytes.Buffer
			_ = db.Status(context.Background(), db.Config{Path: path, Migrations: events.Migrations()}, &after)
			if after.String() != want.String() {
				t.Fatal("status changed")
			}
			if kind == "absent" {
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal(entries)
				}
			}
			if kind == "empty_state" {
				entries, _ := os.ReadDir(filepath.Dir(path))
				if len(entries) != 0 {
					t.Fatal(entries)
				}
			}
		})
	}
}
