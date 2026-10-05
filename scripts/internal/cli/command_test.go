package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
)

// R-J9LV-A4MF R-JC1O-1O3T
func TestVersion(t *testing.T) {
	version := &cli.Version
	const numeric = `(0|[1-9][0-9]*)`
	const identifier = `(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)`
	expression := `^v` + numeric + `\.` + numeric + `\.` + numeric + `(-` + identifier + `(\.` + identifier + `)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`
	if !regexp.MustCompile(expression).MatchString(*version) {
		t.Fatalf("invalid release version %q", *version)
	}
}

// R-T9F8-CWZB R-TAN4-QOQ0 R-TD2X-I87E R-TFIQ-9ROS R-TGQM-NJFH
func TestCommandConstants(t *testing.T) {
	const usage string = cli.Usage
	const manifest string = cli.Manifest
	const sum = cli.ExitSuccess + cli.ExitServerFailed + cli.ExitUsage
	var codes = []int{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}
	// Assignment to uint proves these are untyped, rather than typed int constants.
	var unsigned = []uint{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}
	if sum != 3 || codes[0] != 0 || codes[1] != 1 || codes[2] != 2 || unsigned[2] != 2 {
		t.Fatal("wrong exit codes")
	}
	wantUsage := "Usage: scripts [command]\n\nRun Python scripts from the suite's repositories, with MCP tools at /mcp\nand pages for scripts and their runs at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"
	if usage != wantUsage {
		t.Fatalf("usage %q", usage)
	}
	wantManifest := "app = \"scripts\"\ndescription = \"Python scripts run from the suite's repositories\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nREPOS_DIR = \"../repos/state/repos\"\nTREE_MAX_BYTES = \"268435456\"\nOUTPUT_MAX_BYTES = \"1048576\"\nOPERATION_SECONDS = \"600\"\nSCRIPT_SECONDS = \"600\"\nRUN_KEEP_DAYS = \"15\"\nRUN_KEEP_COUNT = \"10\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/scripts.db\"\n\n[resources]\ncpu_weight = 50\nmemory_max = \"1G\"\nio_weight = 50\n"
	if manifest != wantManifest {
		t.Fatalf("manifest %q", manifest)
	}
	if !strings.Contains(manifest, "\ndescription = \""+pages.Description+"\"\n") {
		t.Fatal("description differs")
	}
	d := settings.Defaults()
	env := fmt.Sprintf("[env]\nREPOS_DIR = %q\nTREE_MAX_BYTES = \"%d\"\nOUTPUT_MAX_BYTES = \"%d\"\nOPERATION_SECONDS = \"%d\"\nSCRIPT_SECONDS = \"%d\"\nRUN_KEEP_DAYS = \"%d\"\nRUN_KEEP_COUNT = \"%d\"\n\n", d.ReposDir, d.TreeMaxBytes, d.OutputMaxBytes, d.OperationSeconds, d.ScriptSeconds, d.RunKeepDays, d.RunKeepCount)
	start := strings.Index(manifest, "[env]\n")
	end := strings.Index(manifest[start:], "\n\n") + start + 2
	if manifest[start:end] != env {
		t.Fatalf("env differs: %q", manifest[start:end])
	}
}

// R-TJ6F-F2WV R-TKEB-SUNK R-TLM8-6ME9 R-TMU4-KE4Y R-TO20-Y5VN R-TP9X-BXMC R-TRPQ-3H3Q R-TEAT-VZY3
func TestCommandRun(t *testing.T) {
	tests := []struct {
		args           []string
		out, offending string
	}{
		{[]string{"--version"}, cli.Version + "\n", ""},
		{[]string{"manifest"}, cli.Manifest, ""},
		{[]string{"--help"}, cli.Usage, ""},
		{[]string{"bogus"}, "", "bogus"},
		{[]string{"--bogus"}, "", "--bogus"},
		{[]string{""}, "", ""},
		{[]string{"-"}, "", "-"},
		{[]string{"bogus", "--help"}, "", "bogus"},
		{[]string{"--bogus", "manifest"}, "", "--bogus"},
	}
	for _, recognised := range []string{"manifest", "--help", "--version"} {
		for _, extra := range []string{"extra", "--help", ""} {
			tests = append(tests, struct {
				args           []string
				out, offending string
			}{[]string{recognised, extra, "trailing"}, "", extra})
		}
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.args), func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr writeCounter
			dir := t.TempDir()
			code := cli.Run(context.Background(), cli.Process{Args: tc.args, Stdout: &stdout, Stderr: &stderr, Dir: dir})
			wantCode := cli.ExitSuccess
			wantErr := ""
			if tc.out == "" {
				wantCode = cli.ExitUsage
				kind := "command"
				if strings.HasPrefix(tc.offending, "-") {
					kind = "option"
				}
				wantErr = fmt.Sprintf("scripts: unknown %s '%s'\n\nsee 'scripts --help' for usage\n", kind, tc.offending)
			}
			if code != wantCode || stdout.String() != tc.out || stderr.String() != wantErr {
				t.Fatalf("got %d %q %q; want %d %q %q", code, stdout.String(), stderr.String(), wantCode, tc.out, wantErr)
			}
			if code != cli.ExitSuccess && code != cli.ExitServerFailed && code != cli.ExitUsage {
				t.Fatalf("invalid exit %d", code)
			}
			wantWrites := 0
			if wantErr != "" {
				wantWrites = 1
			}
			if stderr.writes != wantWrites {
				t.Fatalf("diagnostic writes %d", stderr.writes)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("command changed directory: %v %v", entries, err)
			}
		})
	}
}

type writeCounter struct {
	bytes.Buffer
	writes int
}

func (w *writeCounter) Write(p []byte) (int, error) { w.writes++; return w.Buffer.Write(p) }

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("command read randomness")
	return 0, io.EOF
}

type forbiddenSink struct{ t *testing.T }

func (s forbiddenSink) Deliver(context.Context, telemetry.Event) error {
	s.t.Fatal("command delivered event")
	return nil
}

// R-S37R-687C
func TestCommandsLeaveProcessUntouched(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"--help"}, {"bogus"}, {"manifest", "--extra"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			forbidden := func() { t.Fatal("command touched process seam") }
			dir := t.TempDir()
			capture := &forbiddenSink{t}
			p := cli.Process{
				Args: args, Stdout: io.Discard, Stderr: io.Discard, Dir: dir, Database: filepath.Join(dir, "catalog.db"),
				LookupEnv:   func(string) (string, bool) { forbidden(); return "", false },
				Environ:     func() []string { forbidden(); return nil },
				Unsetenv:    func(string) error { forbidden(); return nil },
				Inherit:     func(uintptr) (net.Listener, error) { forbidden(); return nil, nil },
				Now:         func() time.Time { forbidden(); return time.Time{} },
				Sleep:       func(context.Context, time.Duration) { forbidden() },
				After:       func(time.Duration) <-chan time.Time { forbidden(); return nil },
				ScriptAfter: func(time.Duration) <-chan time.Time { forbidden(); return nil },
				Banner:      func(page.User) page.Banner { forbidden(); return page.Banner{} },
				MCP:         func(*telemetry.Writer) *mcp.Server { forbidden(); return nil },
				Rand:        forbiddenReader{t}, Sink: capture,
			}
			_ = cli.Run(context.Background(), p)
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("command changed directory: %v %v", entries, err)
			}
		})
	}
}
