package cli

import (
	"bytes"
	"context"
	"regexp"
	"testing"
)

// R-IG9T-7Q01
func TestRunVersionWritesExactVersion(t *testing.T) {
	type result struct {
		exitCode int
		stdout   string
	}

	results := make(map[string]result)
	for _, flag := range []string{"-V", "--version"} {
		var stdout, stderr bytes.Buffer
		exitCode := Run(context.Background(), []string{flag}, &stdout, &stderr, Deps{})

		if exitCode != 0 {
			t.Errorf("Run(%q) exit code = %d, want 0", flag, exitCode)
		}
		// The release value may change independently; this assertion instead proves
		// that Run emits the package's source-carried product fact with exactly one newline.
		// llm-lint:ignore expected-value-from-code-under-test
		if got, want := stdout.String(), version+"\n"; got != want {
			t.Errorf("Run(%q) stdout = %q, want exactly %q", flag, got, want)
		}
		if got := stderr.String(); got != "" {
			t.Errorf("Run(%q) stderr = %q, want empty", flag, got)
		}

		results[flag] = result{exitCode: exitCode, stdout: stdout.String()}
	}

	if results["-V"] != results["--version"] {
		t.Errorf("version spellings differ: -V = %+v, --version = %+v", results["-V"], results["--version"])
	}
}

// R-VBTO-NEYR
func TestVersionHasSemanticVersionShape(t *testing.T) {
	pattern := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	if !pattern.MatchString(version) {
		t.Errorf("version = %q, want match for %s", version, pattern)
	}
}
