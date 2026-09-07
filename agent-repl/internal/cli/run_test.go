package cli_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agent-repl/internal/cli"
	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
)

type failOnRead struct {
	t *testing.T
}

func (reader failOnRead) Read([]byte) (int, error) {
	reader.t.Fatal("Run read stdin during an early exit")
	return 0, io.ErrUnexpectedEOF
}

// R-U01R-JPKI
func TestRunIsCallableInProcessAndReturnsAnExitCode(t *testing.T) {
	type runSignature func(context.Context, []string, io.Reader, io.Writer, io.Writer, cli.Deps) int
	var run runSignature = cli.Run
	deps := cli.Deps{
		Home:   t.TempDir(),
		Getenv: func(string) string { return "test-api-key" },
		Now:    func() time.Time { return time.Unix(0, 0) },
		LogID:  "run-callable-test",
		Root:   t.TempDir(),
	}
	if got := run(t.Context(), nil, strings.NewReader(""), io.Discard, io.Discard, deps); got != 0 {
		t.Fatalf("Run(valid arguments) = %d, want 0", got)
	}
}

// R-OY61-JS2P
func TestDepsHasExactlyTheSpecifiedFields(t *testing.T) {
	interruptType := reflect.TypeOf((<-chan struct{})(nil))
	want := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Home", typeOf: reflect.TypeOf("")},
		{name: "Getenv", typeOf: reflect.TypeOf((func(string) string)(nil))},
		{name: "Now", typeOf: reflect.TypeOf((func() time.Time)(nil))},
		{name: "LogID", typeOf: reflect.TypeOf("")},
		{name: "Root", typeOf: reflect.TypeOf("")},
		{name: "Interrupts", typeOf: interruptType},
	}

	got := reflect.TypeOf(cli.Deps{})
	if got.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want %d", got.NumField(), len(want))
	}
	for index, expected := range want {
		field := got.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf {
			t.Errorf("Deps field %d = %s %s, want %s %s", index, field.Name, field.Type, expected.name, expected.typeOf)
		}
	}
}

// R-UKS2-1T6B R-VSOF-TJVB
func TestHelpSpellingsExitBeforeSessionWorkAndPlaceUsageOnStdout(t *testing.T) {
	for _, argument := range []string{"-h", "--help"} {
		t.Run(argument, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, []string{argument})
			if code != 0 {
				t.Errorf("Run(%q) code = %d, want 0", argument, code)
			}
			assertUsageText(t, stdout)
			if stderr != "" {
				t.Errorf("Run(%q) stderr = %q, want empty", argument, stderr)
			}
		})
	}
}

// R-UIC9-A9OX R-VTWC-7BM0
func TestUnknownFlagIsAUsageErrorOnStderr(t *testing.T) {
	stdout, stderr, code := runCLI(t, []string{"-unknown-phase-seven-flag"})
	if code != 2 {
		t.Errorf("Run(unknown flag) code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("Run(unknown flag) stdout = %q, want empty", stdout)
	}
	const diagnostic = "flag provided but not defined: -unknown-phase-seven-flag\n"
	if !strings.HasPrefix(stderr, diagnostic) {
		t.Errorf("Run(unknown flag) stderr = %q, want one leading diagnostic", stderr)
		return
	}
	assertUsageText(t, strings.TrimPrefix(stderr, diagnostic))
}

// R-UJK5-O1FM
func TestUsageErrorsReportTheirCauseExactlyOnce(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		cause string
	}{
		{name: "unknown flag", args: []string{"-unknown-phase-seven-flag"}, cause: "unknown-phase-seven-flag"},
		{name: "malformed config", args: []string{"-c", "missing-equals"}, cause: "malformed -c argument"},
		{name: "validation", args: []string{"-c", "provider=invalid-provider"}, cause: "invalid provider"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, test.args)
			if code != 2 {
				t.Fatalf("Run(%q) code = %d, want 2", test.args, code)
			}
			if count := strings.Count(stdout+stderr, test.cause); count != 1 {
				t.Errorf("cause %q occurs %d times across output, want 1; stdout=%q stderr=%q", test.cause, count, stdout, stderr)
			}
			if stdout != "" {
				t.Errorf("Run(%q) stdout = %q, want empty", test.args, stdout)
			}
			_, usage, found := strings.Cut(stderr, "\n")
			if !found {
				t.Fatalf("Run(%q) stderr has no diagnostic line: %q", test.args, stderr)
			}
			assertUsageText(t, usage)
		})
	}
}

// assertUsageText holds Run's placed text to the requirement's own words: the
// bytes MUST be options.Usage(). The fixed head, trailer, and composition of
// that text are pinned byte-for-byte in internal/options, so this assertion
// stays correct across an agentkit catalog release.
func assertUsageText(t *testing.T, output string) {
	t.Helper()
	if want := options.Usage(); output != want {
		t.Errorf("usage output = %q, want options.Usage() = %q", output, want)
	}
}

func runCLI(t *testing.T, args []string) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	deps := cli.Deps{
		Home: "/directory-that-does-not-exist/agent-repl-home",
		Getenv: func(string) string {
			t.Fatal("Run consulted the environment during an early exit")
			return ""
		},
		Now: func() time.Time {
			t.Fatal("Run consulted the clock during an early exit")
			return time.Time{}
		},
		LogID: "early-exit-test",
		Root:  "/directory-that-does-not-exist/agent-repl-root",
	}
	code := cli.Run(t.Context(), args, failOnRead{t: t}, &stdout, &stderr, deps)
	return stdout.String(), stderr.String(), code
}
