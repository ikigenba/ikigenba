package cli

import (
	"errors"
	"strings"
	"testing"
)

const expectedUsage = "Usage: agent-monitor [options]\n\nObserve the coding agents on this machine through their logs and hooks.\n\nOptions:\n  -h, --help      print this help\n  -V, --version   print the version\n\nExit codes:\n  0  success\n  1  the output could not be written\n  2  usage error\n"

type runRecorder struct {
	writes []string
	err    error
}

func (w *runRecorder) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	if w.err != nil {
		return 0, w.err
	}
	return len(p), nil
}

func checkRun(t *testing.T, args []string, wantCode ExitCode, wantOut, wantErr string) {
	t.Helper()
	var stdout, stderr runRecorder
	code := Run(args, &stdout, &stderr)
	if code != wantCode {
		t.Errorf("Run(%q) code = %d, want %d", args, code, wantCode)
	}
	if got := strings.Join(stdout.writes, ""); got != wantOut {
		t.Errorf("Run(%q) stdout = %q, want %q", args, got, wantOut)
	}
	if got := strings.Join(stderr.writes, ""); got != wantErr {
		t.Errorf("Run(%q) stderr = %q, want %q", args, got, wantErr)
	}
}

// R-2NZU-D5QZ
func TestRunGreeting(t *testing.T) {
	checkRun(t, nil, ExitSuccess, "hello, world\n", "")
}

// R-2P7Q-QXHO R-2VB8-NS75 R-HD1J-W1WR R-HE9G-9TNG
func TestRunArgumentClasses(t *testing.T) {
	originalVersion := Version
	Version = "test-version"
	defer func() { Version = originalVersion }()
	const hint = "\n\nsee 'agent-monitor --help' for usage\n"
	for _, tc := range []struct {
		arg, product string
	}{
		{"--help", expectedUsage},
		{"-h", expectedUsage},
		{"--version", "test-version\n"},
		{"-V", "test-version\n"},
	} {
		t.Run("known_"+tc.arg, func(t *testing.T) {
			checkRun(t, []string{tc.arg}, ExitSuccess, tc.product, "")
		})
	}
	for _, arg := range []string{"-", "--", "-hV", "-Vh", "--help=x", "--version=x", "--HELP", "-H"} {
		t.Run("option_"+arg, func(t *testing.T) {
			checkRun(t, []string{arg}, ExitUsage, "", "agent-monitor: unknown option '"+arg+"'"+hint)
		})
	}
	for _, arg := range []string{"", "status", "help", "écho"} {
		t.Run("command_"+arg, func(t *testing.T) {
			checkRun(t, []string{arg}, ExitUsage, "", "agent-monitor: unknown command '"+arg+"'"+hint)
		})
	}
	checkRun(t, []string{"--", "--help"}, ExitUsage, "", "agent-monitor: unknown option '--'"+hint)
}

// R-2QFN-4P8D
func TestRunOnlyFirstArgumentMatters(t *testing.T) {
	originalVersion := Version
	Version = "tail-test-version"
	defer func() { Version = originalVersion }()
	for _, tc := range []struct {
		first   string
		code    ExitCode
		out     string
		errText string
	}{
		{"--help", ExitSuccess, expectedUsage, ""},
		{"-h", ExitSuccess, expectedUsage, ""},
		{"--version", ExitSuccess, "tail-test-version\n", ""},
		{"-V", ExitSuccess, "tail-test-version\n", ""},
		{"--", ExitUsage, "", "agent-monitor: unknown option '--'\n\nsee 'agent-monitor --help' for usage\n"},
		{"status", ExitUsage, "", "agent-monitor: unknown command 'status'\n\nsee 'agent-monitor --help' for usage\n"},
		{"", ExitUsage, "", "agent-monitor: unknown command ''\n\nsee 'agent-monitor --help' for usage\n"},
	} {
		first := tc.first
		var baseOut, baseErr runRecorder
		baseCode := Run([]string{first}, &baseOut, &baseErr)
		if baseCode != tc.code || strings.Join(baseOut.writes, "") != tc.out || strings.Join(baseErr.writes, "") != tc.errText {
			t.Errorf("Run(%q) = (%d, %q, %q), want (%d, %q, %q)", first, baseCode, baseOut.writes, baseErr.writes, tc.code, tc.out, tc.errText)
		}
		for _, tail := range [][]string{{"--help"}, {"-hV", "status", "\xff"}, {"", "--version"}} {
			var out, errOut runRecorder
			args := append([]string{first}, tail...)
			code := Run(args, &out, &errOut)
			if code != baseCode || strings.Join(out.writes, "") != strings.Join(baseOut.writes, "") || strings.Join(errOut.writes, "") != strings.Join(baseErr.writes, "") {
				t.Errorf("Run(%q) differs from Run(%q): code %d/%d, stdout %q/%q, stderr %q/%q", args, first, code, baseCode, out.writes, baseOut.writes, errOut.writes, baseErr.writes)
			}
		}
	}
}

// R-HALR-4IFD
func TestRunEscapesDiagnosticArguments(t *testing.T) {
	const hint = "'\n\nsee 'agent-monitor --help' for usage\n"
	cases := []struct {
		arg, escaped string
	}{
		{"bogus", "bogus"},
		{"a\tb", "a\\tb"},
		{"it's", "it\\'s"},
		{"a\\b", "a\\\\b"},
		{"a\nb\rc", "a\\nb\\rc"},
		{"\x00\x1f\x7f", "\\x00\\x1f\\x7f"},
		{"é", "é"},
		{"\xff\xc0\xaf", "\\xff\\xc0\\xaf"},
		{"\x1b[31m", "\\x1b[31m"},
		{"\u2028\u2029\u202e\u0085\u00a0", "\\u2028\\u2029\\u202e\\u0085\\u00a0"},
		{"\U000e0001", "\\U000e0001"},
		{"😀", "😀"},
	}
	for _, tc := range cases {
		t.Run(tc.escaped, func(t *testing.T) {
			checkRun(t, []string{tc.arg}, ExitUsage, "", "agent-monitor: unknown command '"+tc.escaped+hint)
		})
	}
}

// R-2WJ5-1JXU R-32MM-YENB
func TestRunWritesWholeProductsAndDiagnosticsOnce(t *testing.T) {
	originalVersion := Version
	Version = "write-test-version"
	defer func() { Version = originalVersion }()
	for _, tc := range []struct {
		args    []string
		product string
	}{
		{nil, "hello, world\n"},
		{[]string{"--help"}, expectedUsage},
		{[]string{"--version"}, "write-test-version\n"},
	} {
		var out, errOut runRecorder
		if code := Run(tc.args, &out, &errOut); code != ExitSuccess {
			t.Errorf("Run(%q) code = %d, want success", tc.args, code)
		}
		if len(out.writes) != 1 || out.writes[0] != tc.product || len(errOut.writes) != 0 {
			t.Errorf("Run(%q) writes stdout %q, stderr %q", tc.args, out.writes, errOut.writes)
		}
	}
	for _, tc := range []struct {
		arg, diagnostic string
	}{
		{"-x", "agent-monitor: unknown option '-x'\n\nsee 'agent-monitor --help' for usage\n"},
		{"status", "agent-monitor: unknown command 'status'\n\nsee 'agent-monitor --help' for usage\n"},
	} {
		var out, errOut runRecorder
		if code := Run([]string{tc.arg}, &out, &errOut); code != ExitUsage {
			t.Errorf("Run(%q) code = %d, want usage", tc.arg, code)
		}
		if len(out.writes) != 0 || len(errOut.writes) != 1 || errOut.writes[0] != tc.diagnostic {
			t.Errorf("Run(%q) writes stdout %q, stderr %q", tc.arg, out.writes, errOut.writes)
		}
	}
}

// R-2XR1-FBOJ R-2YYX-T3F8 R-31EQ-KMWM
func TestRunProductWriteFailure(t *testing.T) {
	writeErr := errors.New("disk full")
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"--version"}, {"-V"}} {
		for _, stderrErr := range []error{nil, errors.New("stderr closed")} {
			out := runRecorder{err: writeErr}
			errOut := runRecorder{err: stderrErr}
			if code := Run(args, &out, &errOut); code != ExitWriteFailed {
				t.Errorf("Run(%q) code = %d, want write failure", args, code)
			}
			if len(out.writes) != 1 || len(errOut.writes) != 1 || errOut.writes[0] != "agent-monitor: write error: disk full\n" {
				t.Errorf("Run(%q) writes stdout %q, stderr %q", args, out.writes, errOut.writes)
			}
		}
	}
}

// R-306U-6V5X R-31EQ-KMWM
func TestRunUsageDiagnosticWriteFailure(t *testing.T) {
	for _, arg := range []string{"-bad", "status"} {
		var out runRecorder
		errOut := runRecorder{err: errors.New("stderr closed")}
		if code := Run([]string{arg}, &out, &errOut); code != ExitUsage {
			t.Errorf("Run(%q) code = %d, want usage", arg, code)
		}
		if len(out.writes) != 0 || len(errOut.writes) != 1 {
			t.Errorf("Run(%q) writes stdout %q, stderr %q", arg, out.writes, errOut.writes)
		}
	}
}

// R-2MRX-ZE0A
func TestRunExitCodeClosedSet(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"--version"}, {"-V"}, {"-bad"}, {"status"}} {
		for _, failOut := range []bool{false, true} {
			for _, failErr := range []bool{false, true} {
				var out, errOut runRecorder
				if failOut {
					out.err = errors.New("output failed")
				}
				if failErr {
					errOut.err = errors.New("diagnostic failed")
				}
				code := Run(args, &out, &errOut)
				want := ExitSuccess
				if len(args) != 0 && (args[0] == "-bad" || args[0] == "status") {
					want = ExitUsage
				} else if failOut {
					want = ExitWriteFailed
				}
				if code != want {
					t.Errorf("Run(%q), failOut=%t, failErr=%t returned %d, want %d", args, failOut, failErr, code, want)
				}
			}
		}
	}
}
