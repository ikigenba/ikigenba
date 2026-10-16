package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
	"github.com/ikigenba/ikigenba/candidates/internal/settings"
)

type writeCount struct {
	bytes.Buffer
	calls int
}

func (w *writeCount) Write(b []byte) (int, error) { w.calls++; return w.Buffer.Write(b) }

type countingRead struct{ calls *atomic.Int64 }

func (r countingRead) Read(b []byte) (int, error) { r.calls.Add(1); clear(b); return len(b), nil }

type countingTrail struct{ calls *atomic.Int64 }

func (s countingTrail) Deliver(context.Context, telemetry.Event) error { s.calls.Add(1); return nil }

type countingBus struct{ calls *atomic.Int64 }

func (s countingBus) Deliver(context.Context, events.Event) error { s.calls.Add(1); return nil }

// R-VR0S-Y3YB R-VS8P-BVP0 R-39QN-IG1B R-3AYJ-W7S0 R-3C6G-9ZIP R-743U-7UR8 R-75BQ-LMHX
func TestPublishedFiles(t *testing.T) {
	usage := "Usage: candidates [command]\n\nCandidate businesses for agents: MCP tools at /mcp keep the owner's cohorts,\nread each candidate with its evidence and record the owner's rulings,\nwith the cohorts and the passes at /, on the socket systemd\npasses in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	manifest := "app = \"candidates\"\ndescription = \"Candidate businesses resolved from the records scrapes collects, judged and read through the owner's cohorts\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[env]\nCANDIDATES_KEEP_PASSES = \"20\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/candidates.db\"\n\n[resources]\nmemory_max = \"256M\"\n\n[home]\ngroup = \"application\"\n"
	if cli.Usage != usage || cli.Manifest != manifest || cli.NginxConf != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("published contract differs")
	}
	lines := strings.Split(cli.Manifest, "\n")
	if lines[1] != "description = \""+pages.Description+"\"" || lines[8] != "CANDIDATES_KEEP_PASSES = \""+strconv.FormatInt(settings.Defaults().KeepPasses, 10)+"\"" {
		t.Fatal("manifest data differs")
	}

	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		b, err := fs.ReadFile(candidates.Etc(), name)
		if err != nil || string(b) != want {
			t.Fatal(name, err)
		}
	}
}

// R-0XLL-49VE R-101D-VTCS R-3DEC-NR9E R-3EM9-1J03 R-3FU5-FAQS R-3H21-T2HH R-3I9Y-6U86
func TestCommands(t *testing.T) {
	if cli.ExitSuccess != 0 || cli.ExitServerFailed != 1 || cli.ExitUsage != 2 {
		t.Fatal("exit codes")
	}
	for _, tc := range []struct {
		args     []string
		out, arg string
	}{{[]string{"--version"}, "test-version\n", ""}, {[]string{"manifest"}, cli.Manifest, ""}, {[]string{"--help"}, cli.Usage, ""}, {[]string{"bogus"}, "", "bogus"}, {[]string{"--bogus"}, "", "--bogus"}, {[]string{"db"}, "", "db"}, {[]string{"db", "bogus"}, "", "bogus"}, {[]string{"db", "status", "extra"}, "", "extra"}, {[]string{"manifest", "extra"}, "", "extra"}, {[]string{"--version", "--extra"}, "", "--extra"}, {[]string{"bogus", "extra"}, "", "bogus"}} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var out bytes.Buffer
			var stderr writeCount
			dir := t.TempDir()
			var calls atomic.Int64
			p := cli.Process{Args: tc.args, Stdout: &out, Stderr: &stderr, Version: "test-version", Dir: dir,
				LookupEnv: func(string) (string, bool) { calls.Add(1); return "", false }, Unsetenv: func(string) error { calls.Add(1); return nil }, Inherit: func(uintptr) (net.Listener, error) { calls.Add(1); return nil, nil }, Now: func() time.Time { calls.Add(1); return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) { calls.Add(1) }, After: func(time.Duration) <-chan time.Time { calls.Add(1); return nil }, Rand: countingRead{&calls}, Sink: countingTrail{&calls}, EventSink: countingBus{&calls}, Banner: func(page.User) page.Banner { calls.Add(1); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { calls.Add(1); return nil }}
			code := cli.Run(context.Background(), p)
			if calls.Load() != 0 {
				t.Fatal("command used process dependencies", calls.Load())
			}
			wantCode := cli.ExitSuccess
			wantErr := ""
			if tc.arg != "" {
				wantCode = cli.ExitUsage
				kind := "command"
				if strings.HasPrefix(tc.arg, "-") {
					kind = "option"
				}
				wantErr = "candidates: unknown " + kind + " '" + tc.arg + "'\n\nsee 'candidates --help' for usage\n"
			}
			if code != wantCode || out.String() != tc.out || stderr.String() != wantErr {
				t.Fatal(code, out.String(), stderr.String())
			}
			if wantErr != "" && stderr.calls != 1 {
				t.Fatal(stderr.calls)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal(entries, err)
			}
		})
	}
	var out, stderr bytes.Buffer
	p := cli.Process{[]string{"--version"}, nil, nil, 0, &out, &stderr, "", nil, nil, nil, nil, nil, "", nil, nil, nil, nil}
	if cli.Run(context.Background(), p) != cli.ExitSuccess || out.String() != "\n" {
		t.Fatal(out.String())
	}
}

// R-5OBI-O5OM R-5PJF-1XFB
func TestDatabaseStatus(t *testing.T) {
	for _, state := range []string{"empty", "migrated", "broken"} {
		dir := t.TempDir()
		if state == "broken" {
			if err := os.Mkdir(filepath.Join(dir, "state"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state", "candidates.db"), []byte("not a database"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		cfg := db.Config{Path: filepath.Join(dir, "state", "candidates.db"), Migrations: candidates.Migrations()}
		if state == "migrated" {
			handle, err := db.Open(context.Background(), db.Config{Path: cfg.Path, Migrations: cfg.Migrations, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			if err = handle.Close(); err != nil {
				t.Fatal(err)
			}
		}
		var want bytes.Buffer
		err := db.Status(context.Background(), cfg, &want)
		var out bytes.Buffer
		var stderr writeCount
		code := cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &stderr})
		if out.String() != want.String() {
			t.Fatal(out.String(), want.String())
		}
		if err == nil {
			if code != cli.ExitSuccess || stderr.Len() != 0 {
				t.Fatal(code, stderr.String())
			}
			entries, e := os.ReadDir(dir)
			if e != nil || (state == "empty" && len(entries) != 0) {
				t.Fatal(entries, e)
			}
		} else if code != cli.ExitServerFailed || stderr.calls != 1 || stderr.String() != "candidates: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n" {
			t.Fatal(code, stderr.String(), err)
		}
	}
}

// R-1LZK-ROPA R-3SLO-QC36
func TestEarlyRefusals(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{{map[string]string{"DRAIN_SECONDS": "bad\nvalue"}, "candidates: " + (&settings.Error{Name: "DRAIN_SECONDS", Value: "bad\nvalue", Want: settings.WantSeconds}).Error() + "\n"}, {map[string]string{"DRAIN_SECONDS": "bad"}, "candidates: " + (&settings.Error{Name: "DRAIN_SECONDS", Value: "bad", Want: settings.WantSeconds}).Error() + "\n"}, {nil, "candidates: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"}, {map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "2"}, "candidates: 2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"}} {
		var out bytes.Buffer
		var stderr writeCount
		dir := t.TempDir()
		code := cli.Run(context.Background(), cli.Process{LookupEnv: func(k string) (string, bool) {
			if tc.env["DRAIN_SECONDS"] != "" && k != "DRAIN_SECONDS" {
				t.Fatal("lookup after refusal", k)
			}
			v, ok := tc.env[k]
			return v, ok
		}, Pid: 123, Dir: dir, Stdout: &out, Stderr: &stderr})
		if code != cli.ExitUsage || out.Len() != 0 || stderr.String() != tc.want || stderr.calls != 1 {
			t.Fatal(code, stderr.String())
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatal(entries, err)
		}
	}
}

// R-3W9D-VNB9
func TestDatabaseOpenRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	_, openErr := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "candidates.db"), Migrations: candidates.Migrations(), Now: now, Service: pages.ServiceName})
	if openErr == nil {
		t.Fatal("fixture opened")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var out bytes.Buffer
	var stderr writeCount
	code := cli.Run(context.Background(), cli.Process{LookupEnv: func(k string) (string, bool) {
		switch k {
		case "LISTEN_PID":
			return "123", true
		case "LISTEN_FDS":
			return "1", true
		}
		return "", false
	}, Pid: 123, Dir: dir, Stdout: &out, Stderr: &stderr, Now: now, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Fatal(fd)
		}
		return ln, nil
	}})
	if code != cli.ExitServerFailed || out.Len() != 0 || stderr.calls != 1 || stderr.String() != "candidates: cannot open database state/candidates.db: "+strings.ReplaceAll(openErr.Error(), "\n", " ")+"\n" {
		t.Fatal(code, stderr.String(), openErr)
	}
}
