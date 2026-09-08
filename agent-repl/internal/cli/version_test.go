package cli

import (
	"bytes"
	"io"
	"regexp"
	"testing"
)

// expectedInitialVersion is release data fixed by the phase-7 brief. Keeping
// it independent of the production variable makes output regressions visible.
const expectedInitialVersion = "v0.12.0"

// expectedSuccessfulExitCode is a test-owned anchor for the CLI contract. It
// must remain independent of the production exitSuccess constant so a change
// to that constant makes the version-path assertion fail.
const expectedSuccessfulExitCode = 0

type versionFailOnRead struct {
	t *testing.T
}

func (reader versionFailOnRead) Read([]byte) (int, error) {
	reader.t.Fatal("Run read stdin during a version early exit")
	return 0, io.ErrUnexpectedEOF
}

// R-VV48-L3CP R-UKS2-1T6B
func TestVersionSpellingsExitEarlyWithIdenticalBareVersion(t *testing.T) {
	var baseline string
	for _, argument := range []string{"-V", "--version"} {
		t.Run(argument, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(t.Context(), []string{argument}, versionFailOnRead{t: t}, &stdout, &stderr, Deps{
				Home:  "/directory-that-does-not-exist/agent-repl-home",
				LogID: "version-early-exit-test",
				Root:  "/directory-that-does-not-exist/agent-repl-root",
			})
			if code != expectedSuccessfulExitCode {
				t.Errorf("Run(%q) code = %d, want %d", argument, code, expectedSuccessfulExitCode)
			}
			if got, want := stdout.String(), expectedInitialVersion+"\n"; got != want {
				t.Errorf("Run(%q) stdout = %q, want %q", argument, got, want)
			}
			if stderr.Len() != 0 {
				t.Errorf("Run(%q) stderr = %q, want empty", argument, stderr.String())
			}
			if baseline == "" {
				baseline = stdout.String()
			} else if stdout.String() != baseline {
				t.Errorf("Run(%q) stdout = %q, want same output as -V: %q", argument, stdout.String(), baseline)
			}
		})
	}
}

// R-VWC4-YV3E
func TestSourceVersionIsCanonicalSemanticVersion(t *testing.T) {
	if version != expectedInitialVersion {
		t.Errorf("version = %q, want initial release %q", version, expectedInitialVersion)
	}
	pattern := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	if !pattern.MatchString(version) {
		t.Fatalf("version = %q, want v-prefixed semantic version", version)
	}
}
