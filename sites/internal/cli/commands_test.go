package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
)

// R-SK95-2S11
func TestExitConstants(t *testing.T) {
	const sum = cli.ExitSuccess + cli.ExitServerFailed + cli.ExitUsage
	values := [3]int{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}
	var narrow uint8 = sum
	if values != [3]int{0, 1, 2} || narrow != 3 {
		t.Fatal("exit constants")
	}
}

// R-XAL8-2XV4 R-ZEN8-2CX9 R-SMOX-UBIF R-KKIR-A82X R-CMRM-1YT2 R-Z7BT-RQH3
func TestPublicText(t *testing.T) {
	const service = pages.ServiceName
	const description = pages.Description
	if service != "sites" || description == "" || strings.ContainsAny(description, "\n\"\\") {
		t.Fatal("service text")
	}
	wantUsage := "Usage: sites [command]\n\nServe static sites from the suite's repositories at /<slug>/, MCP tools at\n/mcp, and a landing page at /, on the socket systemd passes in. With no\ncommand, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	const usage = cli.Usage
	const manifest = cli.Manifest
	if usage != wantUsage {
		t.Fatal("usage text")
	}
	wantManifest := "app = \"sites\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = true\nsecrets = []\n\n[env]\nREPOS_DIR = \"../repos/state/repos\"\nSITE_MAX_BYTES = \"268435456\"\nOPERATION_SECONDS = \"600\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/sites.db\"\n\n[resources]\nmemory_max = \"128M\"\n"
	if manifest != wantManifest {
		t.Fatal("manifest text")
	}
	if !strings.Contains(manifest, "\ndescription = \""+pages.Description+"\"\n") {
		t.Fatal("description differs")
	}
	defaults := settings.Defaults()
	env := "[env]\nREPOS_DIR = \"" + defaults.ReposDir + "\"\nSITE_MAX_BYTES = \"" + strconv.FormatInt(defaults.SiteMaxBytes, 10) + "\"\nOPERATION_SECONDS = \"" + strconv.FormatInt(defaults.OperationSeconds, 10) + "\"\n\n"
	start := strings.Index(manifest, "[env]\n")
	end := strings.Index(manifest[start:], "\n\n") + start + 2
	if manifest[start:end] != env {
		t.Fatal("manifest defaults differ")
	}
}

type observedWriter struct {
	bytes.Buffer
	calls int
}

func (w *observedWriter) Write(p []byte) (int, error) { w.calls++; return w.Buffer.Write(p) }

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) { r.t.Fatal("Rand used"); return 0, io.EOF }

type forbiddenSink struct{ t *testing.T }

func (s forbiddenSink) Deliver(context.Context, telemetry.Event) error {
	s.t.Fatal("Sink used")
	return nil
}

// R-W5WF-5G86 R-WEFP-TUF1 R-SRKJ-DEH7 R-SSSF-R67W R-XBT4-GPLT R-XD10-UHCI R-SXO1-A96O R-XE8X-8937 R-WHBM-WG2G R-WX6B-VGPH
func TestCommands(t *testing.T) {
	tests := []struct {
		args     []string
		out, err string
		exit     int
	}{
		{[]string{"--version"}, "test display" + "\n", "", cli.ExitSuccess},
		{[]string{"manifest"}, cli.Manifest, "", cli.ExitSuccess},
		{[]string{"--help"}, cli.Usage, "", cli.ExitSuccess},
		{[]string{"db"}, "", "sites: unknown command 'db'\n\nsee 'sites --help' for usage\n", cli.ExitUsage},
		{[]string{"db", "bogus"}, "", "sites: unknown command 'bogus'\n\nsee 'sites --help' for usage\n", cli.ExitUsage},
		{[]string{"db", "status", "extra"}, "", "sites: unknown command 'extra'\n\nsee 'sites --help' for usage\n", cli.ExitUsage},
		{[]string{"db", "status", "--extra", "last"}, "", "sites: unknown option '--extra'\n\nsee 'sites --help' for usage\n", cli.ExitUsage},
	}
	for _, first := range []string{"bogus", "--bogus", "-", "", "--version", "manifest", "--help"} {
		for _, extra := range [][]string{nil, {"another"}, {"--extra"}, {"--help", "third"}} {
			args := append([]string{first}, extra...)
			known := first == "--version" || first == "manifest" || first == "--help"
			if known && len(extra) == 0 {
				continue
			}
			arg := first
			if known {
				arg = extra[0]
			}
			kind := "command"
			if strings.HasPrefix(arg, "-") {
				kind = "option"
			}
			tests = append(tests, struct {
				args     []string
				out, err string
				exit     int
			}{args, "", fmt.Sprintf("sites: unknown %s '%s'\n\nsee 'sites --help' for usage\n", kind, arg), cli.ExitUsage})
		}
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.args), func(t *testing.T) {
			dir := t.TempDir()
			var stdout, stderr observedWriter
			forbidden := func() { t.Fatal("process seam used") }
			p := cli.Process{
				Version: "test display", Args: tc.args, Stdout: &stdout, Stderr: &stderr, Dir: dir, Pid: 42,
				LookupEnv: func(string) (string, bool) { forbidden(); return "", false },
				Environ:   func() []string { forbidden(); return nil }, Unsetenv: func(string) error { forbidden(); return nil },
				Inherit: func(uintptr) (net.Listener, error) { forbidden(); return nil, nil },
				Now:     func() time.Time { forbidden(); return time.Time{} },
				Sleep:   func(context.Context, time.Duration) { forbidden() }, After: func(time.Duration) <-chan time.Time { forbidden(); return nil },
				Rand: forbiddenReader{t}, Sink: forbiddenSink{t},
				Banner: func(page.User) page.Banner { forbidden(); return page.Banner{} },
				MCP:    func(*telemetry.Writer) *mcp.Server { forbidden(); return nil },
			}
			declared := struct {
				Run func(context.Context, cli.Process) int
			}{Run: cli.Run}
			run := declared.Run
			if exit := run(context.Background(), p); exit != tc.exit {
				t.Fatalf("exit=%d want=%d", exit, tc.exit)
			}
			if stdout.String() != tc.out || stderr.String() != tc.err {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if tc.exit != cli.ExitSuccess && (stderr.calls != 1 || stdout.calls != 0) {
				t.Fatalf("write calls stdout=%d stderr=%d", stdout.calls, stderr.calls)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("directory changed: %v %v", entries, err)
			}
			// The four fields alone also suffice, with every optional seam unset.
			stdout.Reset()
			stderr.Reset()
			if exit := run(context.Background(), cli.Process{Version: "test display", Args: tc.args, Stdout: &stdout, Stderr: &stderr, Dir: dir}); exit != tc.exit || stdout.String() != tc.out || stderr.String() != tc.err {
				t.Fatal("minimal process differs")
			}
		})
	}
}

// R-WEFP-TUF1
func TestEmptyDisplayVersion(t *testing.T) {
	var out, stderr bytes.Buffer
	if cli.Run(context.Background(), cli.Process{Args: []string{"--version"}, Version: "", Dir: t.TempDir(), Stdout: &out, Stderr: &stderr}) != cli.ExitSuccess || out.String() != "\n" || stderr.Len() != 0 {
		t.Fatal(out.String(), stderr.String())
	}
}
