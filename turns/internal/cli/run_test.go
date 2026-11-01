package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

var expectedUsage = map[string]string{
	"": `Usage: turns [options] <command> [arguments]

Push this account's agent session turns to an ikigenba space. Never run as root.

Commands:
  configure store the space and token turns pushes with
  install   install the user timer that pushes every five minutes
  push      push new turns to the space
  status    show what is here and what the space holds
  version   print the version
  help      print this help

Options:
  -h, --help     print this help
  --version      print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error
  3  refused: turns must not run as root
  4  refused: not configured

Run 'turns <command> --help' for details on a command.
`,
	"version": `Usage: turns version

Print the version turns was released as.

Options:
  -h, --help   print this help
`,
	"help": `Usage: turns help

Print the usage text, the same as 'turns --help'.

Options:
  -h, --help   print this help
`,
	"configure": `Usage: turns configure [<space>] [--token <token>]

Store the space turns pushes to and the personal token it pushes with, in
$XDG_CONFIG_HOME/turns/ (~/.config/turns/ by default). The token is read as
one line from stdin unless --token gives it. Running it again replaces both,
or, with the space omitted, only the token. On a host the space may be
omitted; the host's own space is used.

Options:
  --token <token>   take the token from the argument instead of stdin
  -h, --help        print this help
`,
	"push": `Usage: turns push [--dry-run]

Read this home's Claude Code, Codex CLI and Grok CLI session files, ask the
space which turns it already holds, and push the turns it lacks. Session
files are never changed, and no text leaves the machine except a first
command's name and argument.

Options:
  --dry-run    report what would be pushed and push nothing
  -h, --help   print this help
`,
	"install": `Usage: turns install

Write turns.service and turns.timer under ~/.config/systemd/user/, reload the
user manager, and enable and start the timer, which runs 'turns push' every
five minutes. On a host whose /etc/systemd/user/ already holds the units, only
enable and start the timer. Safe to run again.

Options:
  -h, --help   print this help
`,
	"status": `Usage: turns status

Print the space and token turns uses, when it last pushed, and per tool how
many sessions and turns are on this machine and how many the space holds.
Pushes nothing.

Options:
  -h, --help   print this help
`,
}

func assertCLI(t testing.TB, code int, out, errText string, wantCode int, wantOut, wantErr string) {
	t.Helper()
	if code != wantCode || out != wantOut || errText != wantErr {
		t.Errorf("got (%d,%q,%q), want (%d,%q,%q)", code, out, errText, wantCode, wantOut, wantErr)
	}
}
func usageDiagnostic(command, summary string) string {
	if command != "" {
		command = " " + command
	}
	return "turns: " + summary + "\n\nsee 'turns" + command + " --help' for usage\n"
}

func inertCLI(t *testing.T, args []string, uid int, wantCode int, wantOut, wantErr string) {
	t.Helper()
	deps, root := syntheticDeps(t)
	deps.EUID = uid
	processes, requests := 0, 0
	deps.Exec = func(context.Context, seam.Cmd) (seam.Result, error) {
		processes++
		return seam.Result{}, fmt.Errorf("unexpected process")
	}
	deps.HTTP = &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, fmt.Errorf("unexpected request")
	})}
	// Readable synthetic files make the unchanged-tree check sensitive to writes/removals.
	for _, dir := range []string{deps.Getenv("HOME"), deps.Getenv("XDG_CONFIG_HOME"), deps.Getenv("XDG_STATE_HOME"), deps.Etc} {
		writeFixture(t, filepath.Join(dir, "sentinel"), "unchanged", 0640)
	}
	before := snapshotTree(t, root)
	stdin := &recordingReader{Reader: strings.NewReader("unused secret\n")}
	code, out, errText := runCLI(t, deps, args, stdin)
	assertCLI(t, code, out, errText, wantCode, wantOut, wantErr)
	if stdin.Reads != 0 {
		t.Errorf("read stdin %d times", stdin.Reads)
	}
	if processes != 0 || requests != 0 {
		t.Errorf("inert invocation made %d processes and %d requests", processes, requests)
	}
	assertTreeEqual(t, before, snapshotTree(t, root))
}

// R-V8Y8-L4IA.
func TestExitConstants(t *testing.T) {
	// Constant aliases require constant expressions. Both numeric widths accept
	// untyped integer constants; a constant with either concrete type cannot be
	// assigned to the other width without an explicit conversion.
	const (
		ok            = ExitOK
		failed        = ExitFailed
		usage         = ExitUsage
		root          = ExitRoot
		notConfigured = ExitNotConfigured
	)
	narrow := []int8{ok, failed, usage, root, notConfigured}
	wide := []int64{ok, failed, usage, root, notConfigured}
	want := []int64{0, 1, 2, 3, 4}
	for i, expected := range want {
		if int64(narrow[i]) != expected || wide[i] != expected {
			t.Errorf("exit constant %d = (%d,%d), want %d", i, narrow[i], wide[i], expected)
		}
	}
}

// R-LR9B-R2VA R-VCLX-QFQD R-VDTU-47H2 R-LW4X-A5U2.
func TestVersion(t *testing.T) {
	const version = Version
	if !regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(version) {
		t.Fatalf("invalid version %q", version)
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { inertCLI(t, args, 1000, ExitOK, version+"\n", "") })
	}
}

// R-VIPF-NAFU R-LSH8-4ULZ R-LTP4-IMCO R-LW4X-A5U2.
func TestTopLevelHelpAndVersionPrecedence(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"--version", "--help"}, {"-h", "--version"}, {"--help", "--bogus"}, {"--bogus", "--help"}, {"-h", "--bogus"}, {"--bogus", "-h"}, {"--help", "push", "extra"}, {"--help", "bogus"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { inertCLI(t, args, 1000, ExitOK, expectedUsage[""], "") })
	}
	for _, args := range [][]string{{"--version", "push"}, {"--version", "bogus", "extra"}, {"--version", "--version", "bogus"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { inertCLI(t, args, 1000, ExitOK, Version+"\n", "") })
	}
}

// R-VF1Q-HZ7R R-VMD4-SLNX R-R0DG-DJCA R-R1LC-RB2Z R-LW4X-A5U2.
func TestTopLevelRefusals(t *testing.T) {
	cases := []struct {
		args    []string
		summary string
	}{{nil, "no command given"}, {[]string{"bogus"}, "unknown command 'bogus'"}, {[]string{"-V"}, "unknown option '-V'"}, {[]string{"--bogus"}, "unknown option '--bogus'"}, {[]string{"--version", "--bogus"}, "unknown option '--bogus'"}, {[]string{"--bogus", "--version"}, "unknown option '--bogus'"}, {[]string{"--"}, "unknown option '--'"}, {[]string{""}, "unknown command ''"}}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.args), func(t *testing.T) { inertCLI(t, tc.args, 1000, ExitUsage, "", usageDiagnostic("", tc.summary)) })
	}
}

// R-VG9M-VQYG R-VHHJ-9IP5 R-VUWF-GZUS R-VW4B-URLH R-LZSM-FH25 R-M10I-T8SU.
func TestEveryCommandHelpWins(t *testing.T) {
	for _, command := range []string{"configure", "install", "push", "status", "version", "help"} {
		for _, flag := range []string{"-h", "--help"} {
			for _, prefix := range [][]string{nil, {"--bogus"}, {"extra"}, {"--token"}} {
				args := append([]string{command}, prefix...)
				args = append(args, flag)
				t.Run(strings.Join(args, " "), func(t *testing.T) { inertCLI(t, args, 1000, ExitOK, expectedUsage[command], "") })
			}
		}
	}
}

// R-R2T9-52TO R-W3FQ-5E1N R-R415-IUKD R-R6GY-AE1R R-R7OU-O5SG R-3QC7-4O1X R-WBZ0-TS8I.
func TestCommandArgumentFrame(t *testing.T) {
	for _, command := range []string{"configure", "install", "push", "status", "version", "help"} {
		t.Run(command+" unknown", func(t *testing.T) {
			inertCLI(t, []string{command, "--bogus"}, 1000, ExitUsage, "", usageDiagnostic(command, "unknown option '--bogus'"))
		})
		if command != "configure" {
			t.Run(command+" positional", func(t *testing.T) {
				inertCLI(t, []string{command, "extra"}, 1000, ExitUsage, "", usageDiagnostic(command, command+" takes no arguments"))
			})
		}
	}
	cases := []struct {
		args    []string
		summary string
	}{{[]string{"configure", "us.ikigenba.dev", "extra"}, "unexpected argument 'extra'"}, {[]string{"configure", "BAD", "extra"}, "unexpected argument 'extra'"}, {[]string{"push", "extra", "--bogus"}, "push takes no arguments"}, {[]string{"push", "--bogus", "extra"}, "unknown option '--bogus'"}, {[]string{"version", "--version"}, "unknown option '--version'"}}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) { inertCLI(t, tc.args, 1000, ExitUsage, "", usageDiagnostic(tc.args[0], tc.summary)) })
	}
}

// R-LXCT-NXKR R-LYKQ-1PBG.
func TestRootRefusalComesFirst(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"--version"}, {"help"}, {"version"}, {"bogus"}, {"--bogus"}, {"push", "--help"}, {"push", "--bogus"}, {"configure", "us.ikigenba.dev", "extra"}, {"configure", "us.ikigenba.dev", "--token", "secret"}, {"install"}, {"push"}, {"status"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) { inertCLI(t, args, 0, ExitRoot, "", "turns: must not run as root\n") })
	}
}

// R-KENG-MY2L.
func TestNilEnvironmentIsEmpty(t *testing.T) {
	for _, args := range [][]string{{"configure", "a.b", "--token=x"}, {"push"}, {"status"}, {"install"}, {"version"}} {
		deps, _ := syntheticDeps(t)
		deps.Getenv = nil
		code, out, errText := runCLI(t, deps, args, nil)
		deps.Getenv = func(string) string { return "" }
		wantCode, wantOut, wantErr := runCLI(t, deps, args, nil)
		assertCLI(t, code, out, errText, wantCode, wantOut, wantErr)
	}
}

func configureEmptyPush(t testing.TB, deps seam.Deps) {
	t.Helper()
	writeFixture(t, filepath.Join(deps.Getenv("XDG_CONFIG_HOME"), "turns", "config"), "space=a.b\n", 0600)
	writeFixture(t, filepath.Join(deps.Getenv("XDG_CONFIG_HOME"), "turns", "token"), "synthetic-token", 0600)
}

// R-SIXR-4O3V.
func TestNilClockWritesCurrentUTC(t *testing.T) {
	deps, _ := syntheticDeps(t)
	configureEmptyPush(t, deps)
	deps.Now = nil
	oldLocal := time.Local
	time.Local = time.FixedZone("fixture offset", 5*60*60)
	t.Cleanup(func() { time.Local = oldLocal })
	before := time.Now().Truncate(time.Second)
	code, _, errText := runCLI(t, deps, []string{"push"}, nil)
	after := time.Now()
	if code != ExitOK || errText != "" {
		t.Fatalf("push failed: %d %s", code, errText)
	}
	data, err := os.ReadFile(filepath.Join(deps.Getenv("XDG_STATE_HOME"), "turns", "last-push"))
	if err != nil {
		t.Fatal(err)
	}
	moment, err := time.Parse("2006-01-02T15:04:05Z\n", string(data))
	if err != nil {
		t.Fatal(err)
	}
	if moment.Before(before) || moment.After(after) {
		t.Errorf("moment %s outside [%s,%s]", moment, before, after)
	}
}

// R-KH39-EHJZ.
func TestNilHostnameMatchesOSHostname(t *testing.T) {
	deps, _ := syntheticDeps(t)
	configureEmptyPush(t, deps)
	writeFixture(t, filepath.Join(deps.Getenv("HOME"), ".claude", "projects", "fixture", "session.jsonl"), `{"type":"user","cwd":"/synthetic/work","timestamp":"2026-11-01T14:00:00Z","message":{"content":"synthetic prompt"}}`+"\n", 0600)
	var hosts []string
	deps.HTTP = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Params struct {
				Arguments struct {
					Args struct {
						Sessions []map[string]any `json:"sessions"`
					} `json:"args"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		refs := make([]map[string]any, len(request.Params.Arguments.Args.Sessions))
		for i, entry := range request.Params.Arguments.Args.Sessions {
			refs[i] = map[string]any{"tool": entry["tool"], "id": entry["id"]}
			if host, ok := entry["host"].(string); ok {
				hosts = append(hosts, host)
			}
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"structuredContent": map[string]any{"sessions": refs}}})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	deps.Hostname = nil
	code, out, errText := runCLI(t, deps, []string{"push"}, nil)
	deps.Hostname = os.Hostname
	wantCode, wantOut, wantErr := runCLI(t, deps, []string{"push"}, nil)
	assertCLI(t, code, out, errText, wantCode, wantOut, wantErr)
	if code != ExitOK || len(hosts) != 2 || hosts[0] != hosts[1] {
		t.Errorf("nil hostname did not match os.Hostname: code=%d hosts=%q", code, hosts)
	}
}

// R-KIB5-S9AO.
func TestNilHTTPUsesDefaultClient(t *testing.T) {
	deps, _ := syntheticDeps(t)
	configureEmptyPush(t, deps)
	// A complete synthetic Claude user turn is enough to cause the latest query.
	writeFixture(t, filepath.Join(deps.Getenv("HOME"), ".claude", "projects", "fixture", "session.jsonl"), `{"type":"user","cwd":"/synthetic/work","timestamp":"2026-11-01T14:00:00Z","message":{"content":"synthetic prompt"}}`+"\n", 0600)
	calls := 0
	old := http.DefaultClient.Transport
	oldTransport := http.DefaultTransport
	http.DefaultTransport = testTransport(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected default transport; DefaultClient must be used")
		return nil, fmt.Errorf("unexpected default transport")
	})
	http.DefaultClient.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("arbitrary refusal")), Header: http.Header{}}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = old; http.DefaultTransport = oldTransport })
	deps.HTTP = nil
	code, _, errText := runCLI(t, deps, []string{"push", "--dry-run"}, nil)
	if calls != 1 || code != ExitFailed || !strings.Contains(errText, "arbitrary refusal") {
		t.Errorf("default HTTP not used: calls=%d code=%d stderr=%q", calls, code, errText)
	}
}

// R-3U7Q-4MGN R-WBZ0-TS8I.
func TestNilExecUsesSyntheticPATHProcess(t *testing.T) {
	deps, root := syntheticDeps(t)
	deps.Exec = nil
	bin := filepath.Join(root, "fake-bin")
	record := filepath.Join(root, "process-record")
	script := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$TURNS_FIXTURE_VALUE\" \"$#\" \"$1\" \"$2\" > \"$TURNS_FIXTURE_RECORD\"\nif IFS= read -r input; then printf 'input:%s\\n' \"$input\" >> \"$TURNS_FIXTURE_RECORD\"; fi\nprintf 'arbitrary output\\n'\nprintf 'first detail\\nsecond detail\\n' >&2\nexit 7\n"
	writeFixture(t, filepath.Join(bin, "systemctl"), script, 0700)
	t.Setenv("PATH", bin)
	t.Setenv("TURNS_FIXTURE_RECORD", record)
	t.Setenv("TURNS_FIXTURE_VALUE", "environment fixture")
	code, out, errText := runCLI(t, deps, []string{"install"}, strings.NewReader("must not reach subprocess\n"))
	assertCLI(t, code, out, errText, ExitFailed, "", "turns: could not enable turns.timer\n\n> first detail\n> second detail\n")
	want := strings.Join([]string{deps.Dir, "environment fixture", "2", "--user", "daemon-reload", ""}, "\n")
	assertContents(t, record, want)
}
