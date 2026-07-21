package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type recordedCommand struct {
	dir  string
	name string
	args []string
}

type scriptedExecutor struct {
	calls []recordedCommand
	err   error
}

func (e *scriptedExecutor) Run(dir, name string, args ...string) error {
	e.calls = append(e.calls, recordedCommand{dir: dir, name: name, args: append([]string(nil), args...)})
	if e.err != nil {
		return e.err
	}
	for i := range args {
		if args[i] == "-out" && i+1 < len(args) {
			path := args[i+1]
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if err := writeFile(path, []byte(`{"composite":0.75}`)); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestStepDispatchNamesSupportedAndRejectedSteps(t *testing.T) {
	// R-EYQR-OABI
	root := newAutotuneTree(t)
	executor := &scriptedExecutor{}
	if err := run([]string{"extract"}, root, executor); err != nil {
		t.Fatalf("supported extract step failed: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("extract invoked %d commands, want build and baseline", len(executor.calls))
	}

	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "compile", args: []string{"compile"}, want: []string{"compile", "extract"}},
		{name: "unknown", args: []string{"frobnicate"}, want: []string{"frobnicate", "extract"}},
		{name: "missing", args: nil, want: []string{"missing step", "extract"}},
		{name: "resume from", args: []string{"extract", "--resume", "--from", "candidate.txt"}, want: []string{"--resume", "--from"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args, root, &scriptedExecutor{})
			if err == nil {
				t.Fatal("run succeeded, want dispatch error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

func TestFreshRunResetsSeedsBuildsAndMeasuresBaseline(t *testing.T) {
	// R-EZYO-2227
	for _, tc := range []struct {
		name       string
		from       bool
		promptText string
	}{
		{name: "committed prompt", promptText: "committed prompt\n"},
		{name: "from override", from: true, promptText: "candidate prompt\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newAutotuneTree(t)
			mustWrite(t, filepath.Join(root, "eval/extract/prompt.txt"), []byte("committed prompt\n"))
			stale := filepath.Join(root, "tmp/autotune/extract/stale")
			mustWrite(t, stale, []byte("old"))
			args := []string{"extract"}
			if tc.from {
				mustWrite(t, filepath.Join(root, "candidate.txt"), []byte(tc.promptText))
				args = append(args, "--from", "candidate.txt")
			}
			executor := &scriptedExecutor{}
			if err := run(args, root, executor); err != nil {
				t.Fatalf("run: %v", err)
			}
			if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale workspace content still exists: %v", err)
			}
			assertFile(t, filepath.Join(root, "autotune/extract/prompt.txt"), tc.promptText)
			assertFile(t, filepath.Join(root, "tmp/autotune/extract/best/prompt.txt"), tc.promptText)
			assertFile(t, filepath.Join(root, "tmp/autotune/extract/best/scorecard.json"), `{"composite":0.75}`)
			resolvedConfig := mustRead(t, filepath.Join(root, "tmp/autotune/extract/config.json"))
			committedConfig := mustRead(t, filepath.Join(root, "eval/extract/config.json"))
			if string(resolvedConfig) != string(committedConfig) {
				t.Fatal("unoverridden resolved config is not byte-identical to committed pins")
			}

			want := []recordedCommand{
				{dir: root, name: "go", args: []string{"build", "-o", "tmp/autotune/extract/bin/eval-extract", "./cmd/eval-extract"}},
				{dir: root, name: "tmp/autotune/extract/bin/eval-extract", args: []string{"run", "-prompt", "autotune/extract/prompt.txt", "-gold", "eval/extract/gold", "-config", "tmp/autotune/extract/config.json", "-out", "tmp/autotune/extract/baseline.json", "-split", "dev", "-repeat", "3"}},
			}
			if !reflect.DeepEqual(executor.calls, want) {
				t.Fatalf("commands:\n got: %#v\nwant: %#v", executor.calls, want)
			}
		})
	}
}

func TestConfigResolutionChangesOnlyAcceptedEvalPins(t *testing.T) {
	// R-F2EG-TLJL
	root := newAutotuneTree(t)
	base := mustRead(t, filepath.Join(root, "eval/extract/config.json"))
	if err := run([]string{"extract", "-c", "model=X", "-c", "temperature=0.5"}, root, &scriptedExecutor{}); err != nil {
		t.Fatalf("run with overrides: %v", err)
	}
	resolved := mustRead(t, filepath.Join(root, "tmp/autotune/extract/config.json"))
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(base, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(resolved, &after); err != nil {
		t.Fatal(err)
	}
	for _, immutable := range []string{"embedding", "weights"} {
		if string(before[immutable]) != string(after[immutable]) {
			t.Errorf("%s block changed bytewise:\nbefore %s\nafter  %s", immutable, before[immutable], after[immutable])
		}
	}
	var evalBlock map[string]any
	if err := json.Unmarshal(after["eval"], &evalBlock); err != nil {
		t.Fatal(err)
	}
	if evalBlock["model"] != "X" || evalBlock["temperature"] != 0.5 {
		t.Fatalf("overrides not resolved: %#v", evalBlock)
	}
	if evalBlock["provider"] != "anthropic" || evalBlock["max_tokens"] != float64(16384) || evalBlock["max_parse_retries"] != float64(2) {
		t.Fatalf("unrelated eval pins changed: %#v", evalBlock)
	}

	for _, rejected := range []string{"embedding.model=nope", "weights.claim=1", "unknown=nope"} {
		t.Run(rejected, func(t *testing.T) {
			rejectRoot := newAutotuneTree(t)
			err := run([]string{"extract", "-c", rejected}, rejectRoot, &scriptedExecutor{})
			key, _, _ := strings.Cut(rejected, "=")
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("error %v does not name rejected key %q", err, key)
			}
			if _, statErr := os.Stat(filepath.Join(rejectRoot, "tmp/autotune/extract")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("rejected override wrote workspace: %v", statErr)
			}
		})
	}
}

func TestResumeRequiresMatchingEvalStampWithoutMutation(t *testing.T) {
	// R-F16K-FTSW
	root := newAutotuneTree(t)
	base := mustRead(t, filepath.Join(root, "eval/extract/config.json"))
	stamp, _, err := resolveConfig(base, []string{"model=X"})
	if err != nil {
		t.Fatal(err)
	}
	stampPath := filepath.Join(root, "tmp/autotune/extract/config.json")
	mustWrite(t, stampPath, stamp)
	mustWrite(t, filepath.Join(root, "tmp/autotune/extract/sentinel"), []byte("keep"))

	executor := &scriptedExecutor{}
	if err := run([]string{"extract", "--resume", "-c", "model=X"}, root, executor); err != nil {
		t.Fatalf("matching resume: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("resume invoked commands: %#v", executor.calls)
	}
	assertFile(t, stampPath, string(stamp))
	assertFile(t, filepath.Join(root, "tmp/autotune/extract/sentinel"), "keep")

	err = run([]string{"extract", "--resume", "-c", "model=Y"}, root, &scriptedExecutor{})
	if err == nil || !strings.Contains(err.Error(), "X") || !strings.Contains(err.Error(), "Y") {
		t.Fatalf("mismatch error does not name both configs: %v", err)
	}
	assertFile(t, stampPath, string(stamp))
	assertFile(t, filepath.Join(root, "tmp/autotune/extract/sentinel"), "keep")
}

func newAutotuneTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "eval/extract/config.json"), []byte(`{
  "eval": {
    "provider": "anthropic",
    "model": "claude-sonnet-4-6",
    "temperature": 0,
    "thinking": false,
    "max_tokens": 16384,
    "max_parse_retries": 2
  },
  "embedding": {
    "provider": "openai",
    "model": "text-embedding-3-small",
    "dimensions": 1536,
    "threshold": 0.80,
    "margin": 0.03
  },
  "weights": {
    "subject": 0.35,
    "claim": 0.50,
    "field": 0.15
  }
}
`))
	mustWrite(t, filepath.Join(root, "eval/extract/prompt.txt"), []byte("committed prompt\n"))
	return root
}

func mustWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := writeFile(path, content); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	if got := string(mustRead(t, path)); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
