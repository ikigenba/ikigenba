package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOptionsSupportsAnalysisStep(t *testing.T) {
	opts, err := parseOptions([]string{"analysis"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.step != "analysis" {
		t.Fatalf("step = %q, want analysis", opts.step)
	}
}

type recordedCommand struct {
	dir  string
	name string
	args []string
	env  []string
}

type scriptedExecutor struct {
	calls   []recordedCommand
	err     error
	waitErr error
	onStart func() error
	onWait  func()
	output  bytes.Buffer
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
			content := []byte(`{"mean_composite":0.75,"run_composites":[0.74,0.75,0.76],"epsilon":0.02}`)
			for j := range args {
				if args[j] == "-split" && j+1 < len(args) && args[j+1] == "holdout" {
					content = []byte(`{"mean_composite":0.70}`)
				}
			}
			if err := writeFile(path, content); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *scriptedExecutor) Start(dir string, env []string, name string, args ...string) (commandProcess, error) {
	e.calls = append(e.calls, recordedCommand{dir: dir, name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)})
	if e.err != nil {
		return nil, e.err
	}
	if e.onStart != nil {
		if err := e.onStart(); err != nil {
			return nil, err
		}
	}
	return scriptedProcess{wait: func() error {
		if e.onWait != nil {
			e.onWait()
		}
		return e.waitErr
	}}, nil
}

func (e *scriptedExecutor) Output() io.Writer { return &e.output }

type scriptedProcess struct{ wait func() error }

func (p scriptedProcess) Wait() error            { return p.wait() }
func (p scriptedProcess) Signal(os.Signal) error { return nil }

func TestStepDispatchNamesSupportedAndRejectedSteps(t *testing.T) {
	// R-EYQR-OABI
	root := newAutotuneTree(t)
	executor := &scriptedExecutor{}
	if err := run(configuredArgs(), root, executor); err != nil {
		t.Fatalf("supported extract step failed: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("extract invoked %d commands, want build, baseline, and ralph", len(executor.calls))
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
			args := configuredArgs()
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
			assertFile(t, filepath.Join(root, "tmp/autotune/extract/best/scorecard.json"), `{"mean_composite":0.75,"run_composites":[0.74,0.75,0.76],"epsilon":0.02}`)
			resolvedConfig := mustRead(t, filepath.Join(root, "tmp/autotune/extract/config.json"))
			var resolved map[string]json.RawMessage
			if err := json.Unmarshal(resolvedConfig, &resolved); err != nil {
				t.Fatal(err)
			}
			if compactJSON(resolved["eval"]) != `{"auth":"key","model":"claude-sonnet-4-6","provider":"anthropic"}` {
				t.Fatalf("resolved eval config = %s", resolved["eval"])
			}

			want := []recordedCommand{
				{dir: root, name: "go", args: []string{"build", "-o", "tmp/autotune/extract/bin/eval-extract", "./cmd/eval-extract"}},
				{dir: root, name: "tmp/autotune/extract/bin/eval-extract", args: []string{"run", "-prompt", "autotune/extract/prompt.txt", "-gold", "eval/extract/gold", "-config", "tmp/autotune/extract/config.json", "-out", "tmp/autotune/extract/baseline.json", "-split", "dev", "-repeat", "3"}},
				{dir: root, name: "ralph", args: []string{"eval/extract/improve.md"}},
			}
			if !reflect.DeepEqual(commandsWithoutEnv(executor.calls), want) {
				t.Fatalf("commands:\n got: %#v\nwant: %#v", executor.calls, want)
			}
		})
	}
}

func TestFreeFormConfigResolutionUsesOnlyRequestedEvalSettings(t *testing.T) {
	// R-XGGI-JK9U
	root := newAutotuneTree(t)
	base := mustRead(t, filepath.Join(root, "eval/extract/config.json"))
	if err := run(configuredArgs("-c", "model=X", "-c", "temperature=0.5"), root, &scriptedExecutor{}); err != nil {
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
	if !reflect.DeepEqual(evalBlock, map[string]any{"auth": "key", "provider": "anthropic", "model": "X", "temperature": 0.5}) {
		t.Fatalf("eval block inherited or omitted settings: %#v", evalBlock)
	}

	for _, rejected := range []string{"embedding.model=nope", "weights.claim=1", "unknown=nope"} {
		t.Run(rejected, func(t *testing.T) {
			rejectRoot := newAutotuneTree(t)
			err := run(configuredArgs("-c", rejected), rejectRoot, &scriptedExecutor{})
			key, _, _ := strings.Cut(rejected, "=")
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("error %v does not name rejected key %q", err, key)
			}
			if _, statErr := os.Stat(filepath.Join(rejectRoot, "tmp/autotune/extract")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("rejected override wrote workspace: %v", statErr)
			}
		})
	}
	for _, missing := range []struct {
		name string
		args []string
	}{
		{name: "provider", args: []string{"extract", "-c", "model=X"}},
		{name: "model", args: []string{"extract", "-c", "provider=openai"}},
	} {
		t.Run("missing "+missing.name, func(t *testing.T) {
			missingRoot := newAutotuneTree(t)
			err := run(missing.args, missingRoot, &scriptedExecutor{})
			if err == nil || !strings.Contains(err.Error(), missing.name) {
				t.Fatalf("error %v does not name missing key %q", err, missing.name)
			}
			if _, statErr := os.Stat(filepath.Join(missingRoot, "tmp/autotune/extract")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("missing key wrote workspace: %v", statErr)
			}
		})
	}
}

func TestResumeRequiresMatchingEvalStampWithoutMutation(t *testing.T) {
	// R-F16K-FTSW
	root := newAutotuneTree(t)
	base := mustRead(t, filepath.Join(root, "eval/extract/config.json"))
	stamp, _, err := resolveConfig(base, []string{"provider=anthropic", "model=X"})
	if err != nil {
		t.Fatal(err)
	}
	stampPath := filepath.Join(root, "tmp/autotune/extract/config.json")
	mustWrite(t, stampPath, stamp)
	mustWrite(t, filepath.Join(root, "tmp/autotune/extract/sentinel"), []byte("keep"))

	mustWrite(t, filepath.Join(root, "tmp/autotune/extract/start-prompt.txt"), []byte("committed prompt\n"))
	mustWrite(t, filepath.Join(root, "tmp/autotune/extract/best/prompt.txt"), []byte("committed prompt\n"))
	mustWrite(t, filepath.Join(root, "autotune/extract/prompt.txt"), []byte("committed prompt\n"))
	executor := &scriptedExecutor{}
	if err := run(configuredArgs("--resume", "-c", "model=X"), root, executor); err != nil {
		t.Fatalf("matching resume: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].name != "ralph" {
		t.Fatalf("resume commands: %#v", executor.calls)
	}
	assertFile(t, stampPath, string(stamp))
	assertFile(t, filepath.Join(root, "tmp/autotune/extract/sentinel"), "keep")

	err = run(configuredArgs("--resume", "-c", "model=Y"), root, &scriptedExecutor{})
	if err == nil || !strings.Contains(err.Error(), "X") || !strings.Contains(err.Error(), "Y") {
		t.Fatalf("mismatch error does not name both configs: %v", err)
	}
	assertFile(t, stampPath, string(stamp))
	assertFile(t, filepath.Join(root, "tmp/autotune/extract/sentinel"), "keep")
}

func TestWrappedExecPreservesPassthroughExactly(t *testing.T) {
	// R-F3MD-7DAA
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "passthrough", args: configuredArgs("--", "--harness", "agentkit", "-c", "model=Z", "--max-time", "2h"), want: []string{"--harness", "agentkit", "-c", "model=Z", "--max-time", "2h", "eval/extract/improve.md"}},
		{name: "defaults", args: configuredArgs(), want: []string{"eval/extract/improve.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newAutotuneTree(t)
			executor := &scriptedExecutor{}
			if err := run(tc.args, root, executor); err != nil {
				t.Fatal(err)
			}
			got := executor.calls[2]
			if got.name != "ralph" || !reflect.DeepEqual(got.args, tc.want) {
				t.Fatalf("ralph call = %#v, want args %#v", got, tc.want)
			}
		})
	}
}

func TestRalphReceivesScorerAliasesWithoutDriverAddedCanonicalKeys(t *testing.T) {
	// R-1KUR-XO00
	t.Run("key auth injects embedding and provider-specific chat aliases", func(t *testing.T) {
		root := newAutotuneTree(t)
		unsetEnv(t, "OPENAI_API_KEY")
		unsetEnv(t, "ANTHROPIC_API_KEY")
		t.Setenv("EVAL_OPENAI_API_KEY", "embedding-alias-key")
		t.Setenv("EVAL_ANTHROPIC_API_KEY", "chat-alias-key")
		executor := &scriptedExecutor{}
		if err := run(configuredArgs(), root, executor); err != nil {
			t.Fatal(err)
		}
		env := environmentMap(executor.calls[2].env)
		if env["EVAL_OPENAI_API_KEY"] != "embedding-alias-key" {
			t.Errorf("embedding alias = %q", env["EVAL_OPENAI_API_KEY"])
		}
		if env["EVAL_ANTHROPIC_API_KEY"] != "chat-alias-key" {
			t.Errorf("anthropic chat alias = %q", env["EVAL_ANTHROPIC_API_KEY"])
		}
		for _, canonical := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
			if _, ok := env[canonical]; ok {
				t.Errorf("driver added canonical key %s", canonical)
			}
		}
	})

	t.Run("subscription auth injects only the embedding alias", func(t *testing.T) {
		root := newAutotuneTree(t)
		unsetEnv(t, "OPENAI_API_KEY")
		unsetEnv(t, "ANTHROPIC_API_KEY")
		unsetEnv(t, "EVAL_ANTHROPIC_API_KEY")
		t.Setenv("EVAL_OPENAI_API_KEY", "embedding-alias-key")
		executor := &scriptedExecutor{}
		if err := run(configuredArgs("-c", "provider=openai", "-c", "auth=sub"), root, executor); err != nil {
			t.Fatal(err)
		}
		env := environmentMap(executor.calls[2].env)
		if env["EVAL_OPENAI_API_KEY"] != "embedding-alias-key" {
			t.Errorf("embedding alias = %q", env["EVAL_OPENAI_API_KEY"])
		}
		for _, name := range []string{"EVAL_ANTHROPIC_API_KEY", "EVAL_GEMINI_API_KEY", "EVAL_OPENROUTER_API_KEY", "EVAL_ZAI_API_KEY"} {
			if _, ok := env[name]; ok {
				t.Errorf("subscription run injected chat alias %s", name)
			}
		}
	})

	t.Run("missing embedding key fails before any child", func(t *testing.T) {
		root := newAutotuneTree(t)
		unsetEnv(t, "OPENAI_API_KEY")
		unsetEnv(t, "EVAL_OPENAI_API_KEY")
		executor := &scriptedExecutor{}
		err := run(configuredArgs(), root, executor)
		if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") || !strings.Contains(err.Error(), "EVAL_OPENAI_API_KEY") {
			t.Fatalf("missing embedding error = %v", err)
		}
		if len(executor.calls) != 0 {
			t.Fatalf("commands ran before embedding key validation: %#v", executor.calls)
		}
	})
}

func TestDiffIsPrintedOnlyWhenPromptChangesDuringRun(t *testing.T) {
	// R-F4U9-L50Z
	root := newAutotuneTree(t)
	executor := &scriptedExecutor{}
	executor.onWait = func() {
		mustWrite(t, filepath.Join(root, "autotune/extract/prompt.txt"), []byte("improved prompt\n"))
		time.Sleep(20 * time.Millisecond)
		if !strings.Contains(executor.output.String(), "+improved prompt") {
			t.Error("diff was not printed before the child ended")
		}
	}
	if err := run(configuredArgs(), root, executor); err != nil {
		t.Fatal(err)
	}
	output := executor.output.String()
	for _, want := range []string{"--- a/eval/extract/prompt.txt", "+++ b/autotune/extract/prompt.txt", "-committed prompt", "+improved prompt"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}

	unchanged := &scriptedExecutor{}
	if err := run(configuredArgs(), newAutotuneTree(t), unchanged); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unchanged.output.String(), "--- a/") {
		t.Fatalf("unchanged run printed a diff:\n%s", unchanged.output.String())
	}
}

func TestFinalizerScoresWinnerOnceForEveryChildExit(t *testing.T) {
	// R-F625-YWRO
	for _, tc := range []struct {
		name    string
		waitErr error
	}{
		{name: "success"},
		{name: "non-zero", waitErr: errors.New("exit status 9")},
		{name: "signaled", waitErr: errors.New("signal: interrupt")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newAutotuneTree(t)
			executor := &scriptedExecutor{waitErr: tc.waitErr}
			executor.onWait = func() {
				mustWrite(t, filepath.Join(root, "tmp/autotune/extract/best/prompt.txt"), []byte("winner\n"))
				mustWrite(t, filepath.Join(root, "autotune/extract/prompt.txt"), []byte("winner\n"))
				mustWrite(t, filepath.Join(root, "tmp/autotune/extract/best/scorecard.json"), []byte(`{"mean_composite":0.90}`))
				mustWrite(t, filepath.Join(root, "tmp/autotune/extract/history.md"), []byte("001 accept\n002 reject\n"))
			}
			if err := run(configuredArgs(), root, executor); err != nil {
				t.Fatalf("finalize after child exit: %v", err)
			}
			if got := holdoutCalls(executor.calls); got != 1 {
				t.Fatalf("holdout calls = %d, want 1; %#v", got, executor.calls)
			}
			summary := string(mustRead(t, filepath.Join(root, "tmp/autotune/extract/summary.md")))
			for _, want := range []string{"Dev best: 0.900000", "Baseline: 0.750000", "Epsilon: 0.020000", "Attempts: 2", "Holdout composite: 0.700000", "OVERFIT"} {
				if !strings.Contains(summary, want) {
					t.Errorf("summary missing %q:\n%s", want, summary)
				}
			}
			if !strings.Contains(executor.output.String(), "+winner") {
				t.Fatalf("final diff missing:\n%s", executor.output.String())
			}

			resume := &scriptedExecutor{}
			if err := run(configuredArgs("--resume"), root, resume); err != nil {
				t.Fatalf("resume finalization: %v", err)
			}
			if got := holdoutCalls(resume.calls); got != 0 {
				t.Fatalf("resume holdout calls = %d, want 0", got)
			}
		})
	}
}

func TestFinalizerWithoutWinnerReportsEvidenceAndRestoresPrompt(t *testing.T) {
	// R-F7A2-COID
	root := newAutotuneTree(t)
	executor := &scriptedExecutor{}
	executor.onWait = func() {
		mustWrite(t, filepath.Join(root, "autotune/extract/prompt.txt"), []byte("rejected candidate\n"))
		mustWrite(t, filepath.Join(root, "tmp/autotune/extract/history.md"), []byte("001 reject\n002 reject\n003 reject\n"))
	}
	if err := run(configuredArgs(), root, executor); err != nil {
		t.Fatal(err)
	}
	if holdoutCalls(executor.calls) != 0 {
		t.Fatalf("no-winner finalizer ran holdout: %#v", executor.calls)
	}
	if output := executor.output.String(); !strings.Contains(output, "no improvement after 3 attempts") || !strings.Contains(output, "tmp/autotune/extract") {
		t.Fatalf("no-improvement evidence missing: %s", output)
	}
	assertFile(t, filepath.Join(root, "autotune/extract/prompt.txt"), "committed prompt\n")
}

func holdoutCalls(calls []recordedCommand) int {
	count := 0
	for _, call := range calls {
		if slicesContainPair(call.args, "-split", "holdout") {
			count++
		}
	}
	return count
}

func slicesContainPair(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}

func newAutotuneTree(t *testing.T) string {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "embedding-test-key")
	t.Setenv("ANTHROPIC_API_KEY", "chat-test-key")
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

func configuredArgs(suffix ...string) []string {
	args := []string{"extract", "-c", "provider=anthropic", "-c", "model=claude-sonnet-4-6"}
	return append(args, suffix...)
}

func commandsWithoutEnv(commands []recordedCommand) []recordedCommand {
	result := append([]recordedCommand(nil), commands...)
	for i := range result {
		result[i].env = nil
	}
	return result
}

func environmentMap(entries []string) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			result[name] = value
		}
	}
	return result
}

func unsetEnv(t *testing.T, name string) {
	t.Helper()
	previous, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
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
