package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

const supportedStep = "extract"

type commandExecutor interface {
	Run(dir, name string, args ...string) error
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

	workspaceRel := filepath.Join("tmp", "autotune", opts.step)
	workspace := filepath.Join(root, workspaceRel)
	stampPath := filepath.Join(workspace, "config.json")
	if opts.resume {
		return checkResumeStamp(stampPath, requestedEval)
	}

	promptSource := filepath.Join(root, "eval", opts.step, "prompt.txt")
	if opts.from != "" {
		promptSource = opts.from
		if !filepath.IsAbs(promptSource) {
			promptSource = filepath.Join(root, promptSource)
		}
	}
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

	workingPromptRel := filepath.Join("autotune", opts.step, "prompt.txt")
	workingPrompt := filepath.Join(root, workingPromptRel)
	if err := writeFile(workingPrompt, prompt); err != nil {
		return fmt.Errorf("seed working prompt: %w", err)
	}

	runnerRel := filepath.Join(workspaceRel, "bin", "eval-extract")
	if err := executor.Run(root, "go", "build", "-o", runnerRel, "./cmd/eval-extract"); err != nil {
		return fmt.Errorf("build eval-extract runner: %w", err)
	}
	baselineRel := filepath.Join(workspaceRel, "baseline.json")
	configRel := filepath.Join(workspaceRel, "config.json")
	if err := executor.Run(root, runnerRel,
		"run",
		"-prompt", workingPromptRel,
		"-gold", filepath.Join("eval", opts.step, "gold"),
		"-config", configRel,
		"-out", baselineRel,
		"-split", "dev",
		"-repeat", "3",
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
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return opts, fmt.Errorf("missing step; supported steps: %s", supportedStep)
	}
	opts.step = args[0]
	if opts.step != supportedStep {
		return opts, fmt.Errorf("unsupported step %q; supported steps: %s", opts.step, supportedStep)
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
	rawEval, ok := document["eval"]
	if !ok {
		return nil, nil, errors.New("committed config has no eval block")
	}
	if len(overrides) == 0 {
		return append([]byte(nil), base...), append(json.RawMessage(nil), rawEval...), nil
	}
	var evalBlock map[string]any
	if err := json.Unmarshal(rawEval, &evalBlock); err != nil {
		return nil, nil, fmt.Errorf("decode committed eval block: %w", err)
	}

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
		case "max_tokens":
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid value for %s: %q", key, value)
			}
			evalBlock[key] = n
		default:
			return nil, nil, fmt.Errorf("unsupported config key %q; overrides apply only to eval", key)
		}
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
