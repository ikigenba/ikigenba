package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"wiki/internal/eval"
)

const supportedSteps = "extract, analysis"

var supportedStep = map[string]bool{"extract": true, "analysis": true}

type commandExecutor interface {
	Run(dir, name string, args ...string) error
	Start(dir string, env []string, name string, args ...string) (commandProcess, error)
	Output() io.Writer
}

type commandProcess interface {
	Wait() error
	Signal(os.Signal) error
}

type osExecutor struct {
	stdout io.Writer
	stderr io.Writer
}

func (e osExecutor) Run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = e.stdout
	cmd.Stderr = e.stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (e osExecutor) Start(dir string, env []string, name string, args ...string) (commandProcess, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = e.stdout
	cmd.Stderr = e.stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return osProcess{cmd: cmd}, nil
}

func (e osExecutor) Output() io.Writer { return e.stdout }

type osProcess struct{ cmd *exec.Cmd }

func (p osProcess) Wait() error                   { return p.cmd.Wait() }
func (p osProcess) Signal(signal os.Signal) error { return p.cmd.Process.Signal(signal) }

type options struct {
	step      string
	overrides []string
	resume    bool
	from      string
	passthru  []string
}

func main() {
	if err := run(os.Args[1:], ".", osExecutor{stdout: os.Stdout, stderr: os.Stderr}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, root string, executor commandExecutor) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	basePath := filepath.Join(root, "eval", opts.step, "config.json")
	base, err := os.ReadFile(basePath)
	if err != nil {
		return fmt.Errorf("read committed config: %w", err)
	}
	resolved, requestedEval, err := resolveConfig(base, opts.overrides)
	if err != nil {
		return err
	}
	var call eval.EvalCall
	if err := json.Unmarshal(requestedEval, &call); err != nil {
		return fmt.Errorf("decode resolved eval config: %w", err)
	}
	childEnv, err := scorerEnvironment(call, os.Getenv, os.Environ())
	if err != nil {
		return err
	}

	workspaceRel := filepath.Join("tmp", "autotune", opts.step)
	workspace := filepath.Join(root, workspaceRel)
	stampPath := filepath.Join(workspace, "config.json")
	promptSource := filepath.Join(root, "eval", opts.step, "prompt.txt")
	if opts.from != "" {
		promptSource = opts.from
		if !filepath.IsAbs(promptSource) {
			promptSource = filepath.Join(root, promptSource)
		}
	}
	workingPromptRel := filepath.Join("autotune", opts.step, "prompt.txt")
	workingPrompt := filepath.Join(root, workingPromptRel)
	runnerName := "eval-" + opts.step
	runnerRel := filepath.Join(workspaceRel, "bin", runnerName)
	baselineRel := filepath.Join(workspaceRel, "baseline.json")
	configRel := filepath.Join(workspaceRel, "config.json")
	startPath := filepath.Join(workspace, "start-prompt.txt")
	if opts.resume {
		if err := checkResumeStamp(stampPath, requestedEval); err != nil {
			return err
		}
	} else {
		prompt, err := os.ReadFile(promptSource)
		if err != nil {
			return fmt.Errorf("read starting prompt: %w", err)
		}
		if err := os.RemoveAll(workspace); err != nil {
			return fmt.Errorf("reset workspace: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(workspace, "bin"), 0o755); err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		if err := writeFile(stampPath, resolved); err != nil {
			return fmt.Errorf("write resolved config: %w", err)
		}
		if err := writeFile(workingPrompt, prompt); err != nil {
			return fmt.Errorf("seed working prompt: %w", err)
		}
		if err := writeFile(startPath, prompt); err != nil {
			return fmt.Errorf("stamp starting prompt: %w", err)
		}
		if err := executor.Run(root, "go", "build", "-o", runnerRel, "./cmd/"+runnerName); err != nil {
			return fmt.Errorf("build %s runner: %w", runnerName, err)
		}
		if err := executor.Run(root, runnerRel,
			"run", "-prompt", workingPromptRel,
			"-gold", filepath.Join("eval", opts.step, "gold"),
			"-config", configRel, "-out", baselineRel,
			"-split", "dev", "-repeat", "3",
		); err != nil {
			return fmt.Errorf("measure dev baseline: %w", err)
		}
		baseline, err := os.ReadFile(filepath.Join(root, baselineRel))
		if err != nil {
			return fmt.Errorf("read baseline result: %w", err)
		}
		best := filepath.Join(workspace, "best")
		if err := writeFile(filepath.Join(best, "prompt.txt"), prompt); err != nil {
			return fmt.Errorf("install best prompt: %w", err)
		}
		if err := writeFile(filepath.Join(best, "scorecard.json"), baseline); err != nil {
			return fmt.Errorf("install best baseline: %w", err)
		}
	}

	committedPrompt, err := os.ReadFile(filepath.Join(root, "eval", opts.step, "prompt.txt"))
	if err != nil {
		return fmt.Errorf("read committed prompt for diff: %w", err)
	}
	before, err := os.ReadFile(workingPrompt)
	if err != nil {
		return fmt.Errorf("read working prompt: %w", err)
	}
	loopArgs := append(append([]string(nil), opts.passthru...), filepath.Join("eval", opts.step, "improve.md"))
	process, err := executor.Start(root, childEnv, "ralph", loopArgs...)
	if err != nil {
		return fmt.Errorf("start ralph: %w", err)
	}
	watchPrompt(process, workingPrompt, filepath.Join("eval", opts.step, "prompt.txt"), workingPromptRel, committedPrompt, before, executor.Output())
	return finalize(root, opts.step, executor)
}

var providerKeyNames = map[string]string{
	"anthropic":  providerKeyName("ANTHROPIC"),
	"google":     providerKeyName("GEMINI"),
	"openai":     providerKeyName("OPENAI"),
	"openrouter": providerKeyName("OPENROUTER"),
	"zai":        providerKeyName("ZAI"),
}

func scorerEnvironment(call eval.EvalCall, getenv func(string) string, environ []string) ([]string, error) {
	embeddingCanonical := providerKeyName("OPENAI")
	embeddingKey, err := availableKey(embeddingCanonical, getenv)
	if err != nil {
		return nil, fmt.Errorf("embedding key: %w", err)
	}
	aliases := map[string]string{"EVAL_" + embeddingCanonical: embeddingKey}
	if call.Auth == "" || call.Auth == "key" {
		canonical, ok := providerKeyNames[call.Provider]
		if !ok {
			return nil, fmt.Errorf("unsupported chat provider %q", call.Provider)
		}
		chatKey, err := availableKey(canonical, getenv)
		if err != nil {
			return nil, fmt.Errorf("chat key for provider %s: %w", call.Provider, err)
		}
		aliases["EVAL_"+canonical] = chatKey
	}

	result := append([]string(nil), environ...)
	for name, value := range aliases {
		prefix := name + "="
		filtered := result[:0]
		for _, entry := range result {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		result = append(filtered, name+"="+value)
	}
	return result, nil
}

func providerKeyName(provider string) string {
	return provider + "_API_" + "KEY"
}

func availableKey(canonical string, getenv func(string) string) (string, error) {
	alias := "EVAL_" + canonical
	if value := strings.TrimSpace(getenv(alias)); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(getenv(canonical)); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("neither %s nor %s is set", alias, canonical)
}

func watchPrompt(process commandProcess, path, oldName, newName string, committed, previous []byte, out io.Writer) {
	done := make(chan struct{})
	go func() { _ = process.Wait(); close(done) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if current, err := os.ReadFile(path); err == nil && !bytes.Equal(current, previous) {
			fmt.Fprint(out, unifiedDiff(oldName, newName, committed, current))
			previous = current
		}
		select {
		case sig := <-signals:
			_ = process.Signal(sig)
		case <-ticker.C:
		case <-done:
			if current, err := os.ReadFile(path); err == nil && !bytes.Equal(current, previous) {
				fmt.Fprint(out, unifiedDiff(oldName, newName, committed, current))
			}
			return
		}
	}
}

func unifiedDiff(oldName, newName string, old, new []byte) string {
	if bytes.Equal(old, new) {
		return ""
	}
	var result strings.Builder
	fmt.Fprintf(&result, "--- a/%s\n+++ b/%s\n@@ -1 +1 @@\n", oldName, newName)
	for _, line := range strings.Split(strings.TrimSuffix(string(old), "\n"), "\n") {
		fmt.Fprintf(&result, "-%s\n", line)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(new), "\n"), "\n") {
		fmt.Fprintf(&result, "+%s\n", line)
	}
	return result.String()
}

func finalize(root, step string, executor commandExecutor) error {
	workspaceRel := filepath.Join("tmp", "autotune", step)
	workspace := filepath.Join(root, workspaceRel)
	start, err := os.ReadFile(filepath.Join(workspace, "start-prompt.txt"))
	if err != nil {
		return fmt.Errorf("read starting prompt stamp: %w", err)
	}
	best, err := os.ReadFile(filepath.Join(workspace, "best", "prompt.txt"))
	if err != nil {
		return fmt.Errorf("read best prompt: %w", err)
	}
	attempts := countAttempts(filepath.Join(workspace, "history.md"))
	working := filepath.Join(root, "autotune", step, "prompt.txt")
	if bytes.Equal(best, start) {
		if err := writeFile(working, start); err != nil {
			return fmt.Errorf("restore starting prompt: %w", err)
		}
		fmt.Fprintf(executor.Output(), "no improvement after %d attempts; evidence: %s\n", attempts, workspaceRel)
		return nil
	}
	holdoutRel := filepath.Join(workspaceRel, "holdout-scorecard.json")
	holdoutPath := filepath.Join(root, holdoutRel)
	if _, err := os.Stat(holdoutPath); errors.Is(err, os.ErrNotExist) {
		runnerRel := filepath.Join(workspaceRel, "bin", "eval-"+step)
		if err := executor.Run(root, runnerRel, "run",
			"-prompt", filepath.Join(workspaceRel, "best", "prompt.txt"),
			"-gold", filepath.Join("eval", step, "gold"),
			"-config", filepath.Join(workspaceRel, "config.json"),
			"-out", holdoutRel, "-split", "holdout",
		); err != nil {
			return fmt.Errorf("measure holdout: %w", err)
		}
		if err := writeSummary(workspace, attempts); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("check holdout scorecard: %w", err)
	}
	committed, err := os.ReadFile(filepath.Join(root, "eval", step, "prompt.txt"))
	if err != nil {
		return err
	}
	fmt.Fprint(executor.Output(), unifiedDiff(filepath.Join("eval", step, "prompt.txt"), filepath.Join("autotune", step, "prompt.txt"), committed, best))
	return nil
}

func countAttempts(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func writeSummary(workspace string, attempts int) error {
	read := func(name string) (eval.Scorecard, error) {
		var score eval.Scorecard
		content, err := os.ReadFile(filepath.Join(workspace, name))
		if err == nil {
			err = json.Unmarshal(content, &score)
		}
		return score, err
	}
	baseline, err := read("baseline.json")
	if err != nil {
		return fmt.Errorf("read baseline scorecard: %w", err)
	}
	best, err := read(filepath.Join("best", "scorecard.json"))
	if err != nil {
		return fmt.Errorf("read best scorecard: %w", err)
	}
	holdout, err := read("holdout-scorecard.json")
	if err != nil {
		return fmt.Errorf("read holdout scorecard: %w", err)
	}
	verdict := "generalized"
	if holdout.MeanComposite <= baseline.MeanComposite {
		verdict = "OVERFIT: the dev win did not hold up on holdout"
	}
	summary := fmt.Sprintf("# Autotune summary\n\nDev best: %.6f\nBaseline: %.6f\nEpsilon: %.6f\nAttempts: %d\nHoldout composite: %.6f\nVerdict: %s\n", best.MeanComposite, baseline.MeanComposite, baseline.Epsilon, attempts, holdout.MeanComposite, verdict)
	if err := writeFile(filepath.Join(workspace, "summary.md"), []byte(summary)); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return opts, fmt.Errorf("missing step; supported steps: %s", supportedSteps)
	}
	opts.step = args[0]
	if !supportedStep[opts.step] {
		return opts, fmt.Errorf("unsupported step %q; supported steps: %s", opts.step, supportedSteps)
	}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--":
			opts.passthru = append([]string(nil), args[i+1:]...)
			i = len(args)
		case "-c":
			if i+1 >= len(args) {
				return opts, errors.New("-c requires key=value")
			}
			i++
			opts.overrides = append(opts.overrides, args[i])
		case "--resume":
			opts.resume = true
		case "--from":
			if i+1 >= len(args) {
				return opts, errors.New("--from requires a file")
			}
			i++
			opts.from = args[i]
		default:
			return opts, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if opts.resume && opts.from != "" {
		return opts, errors.New("--resume cannot be combined with --from")
	}
	return opts, nil
}

func resolveConfig(base []byte, overrides []string) ([]byte, json.RawMessage, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(base, &document); err != nil {
		return nil, nil, fmt.Errorf("decode committed config: %w", err)
	}
	if _, ok := document["eval"]; !ok {
		return nil, nil, errors.New("committed config has no eval block")
	}
	rawEval := document["eval"]
	evalBlock := map[string]any{}

	for _, override := range overrides {
		key, value, found := strings.Cut(override, "=")
		if !found || key == "" {
			return nil, nil, fmt.Errorf("invalid override %q; want key=value", override)
		}
		switch key {
		case "provider", "model", "auth", "auth_file":
			evalBlock[key] = value
		case "temperature":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid value for %s: %q", key, value)
			}
			evalBlock[key] = n
		case "thinking":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid value for %s: %q", key, value)
			}
			evalBlock[key] = b
		case "max_tokens", "max_parse_retries":
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid value for %s: %q", key, value)
			}
			evalBlock[key] = n
		default:
			return nil, nil, fmt.Errorf("unsupported config key %q; overrides apply only to eval", key)
		}
	}
	for _, required := range []string{"provider", "model"} {
		if value, ok := evalBlock[required].(string); !ok || value == "" {
			return nil, nil, fmt.Errorf("missing required config key %q", required)
		}
	}
	if _, ok := evalBlock["auth"]; !ok {
		evalBlock["auth"] = "key"
	}

	resolvedEval, err := json.MarshalIndent(evalBlock, "  ", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("encode resolved eval block: %w", err)
	}
	start := bytes.Index(base, rawEval)
	if start < 0 {
		return nil, nil, errors.New("locate eval block in committed config")
	}
	resolved := make([]byte, 0, len(base)-len(rawEval)+len(resolvedEval))
	resolved = append(resolved, base[:start]...)
	resolved = append(resolved, resolvedEval...)
	resolved = append(resolved, base[start+len(rawEval):]...)
	return resolved, resolvedEval, nil
}

func checkResumeStamp(path string, requestedEval json.RawMessage) error {
	stamp, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read workspace config stamp: %w", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(stamp, &document); err != nil {
		return fmt.Errorf("decode workspace config stamp: %w", err)
	}
	workspaceEval, ok := document["eval"]
	if !ok {
		return errors.New("workspace config stamp has no eval block")
	}
	if !jsonEqual(workspaceEval, requestedEval) {
		return fmt.Errorf("workspace was tuned under %s, you asked for %s", compactJSON(workspaceEval), compactJSON(requestedEval))
	}
	return nil
}

func jsonEqual(a, b []byte) bool {
	var av, bv any
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}

func compactJSON(value []byte) string {
	var dst bytes.Buffer
	if json.Compact(&dst, value) != nil {
		return string(value)
	}
	return dst.String()
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
