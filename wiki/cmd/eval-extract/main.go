// Command eval-extract runs production-faithful extraction evaluations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ikigenba/agentkit"

	"wiki/internal/eval"
	"wiki/internal/extract"
)

type dependencies struct {
	chat     func(eval.EvalCall, func(string) string) (agentkit.Provider, error)
	embed    func(string, func(string) string) (agentkit.EmbeddingProvider, error)
	getenv   func(string) string
	cacheDir string
}

func main() {
	os.Exit(execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr, productionDependencies()))
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: eval-extract run|compare")
		return 2
	}
	var err error
	switch args[0] {
	case "run":
		err = run(ctx, args[1:], stderr, deps)
	case "compare":
		var accepted bool
		accepted, err = compare(args[1:], stdout)
		if err == nil && !accepted {
			return 1
		}
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

type optionalString struct {
	value string
	set   bool
}

func (v *optionalString) String() string         { return v.value }
func (v *optionalString) Set(value string) error { v.value, v.set = value, true; return nil }

type optionalNumber[T int | float64] struct {
	value T
	set   bool
}

func (v *optionalNumber[T]) String() string { return fmt.Sprint(v.value) }
func (v *optionalNumber[T]) Set(value string) error {
	_, err := fmt.Sscan(value, &v.value)
	v.set = err == nil
	return err
}

func run(ctx context.Context, args []string, stderr io.Writer, deps dependencies) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	promptPath := fs.String("prompt", "", "prompt instruction file")
	goldPath := fs.String("gold", "", "gold corpus directory")
	configPath := fs.String("config", "", "evaluation config")
	outPath := fs.String("out", "", "scorecard output")
	split := fs.String("split", "dev", "dev or holdout")
	repeat := fs.Int("repeat", 1, "number of complete runs")
	var model, provider optionalString
	var temperature optionalNumber[float64]
	var maxTokens optionalNumber[int]
	fs.Var(&model, "model", "model override")
	fs.Var(&provider, "provider", "provider override")
	fs.Var(&temperature, "temperature", "temperature override")
	fs.Var(&maxTokens, "max-tokens", "max tokens override")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse run flags: %w", err)
	}
	if *promptPath == "" || *goldPath == "" || *configPath == "" || *outPath == "" {
		return errors.New("run requires -prompt, -gold, -config, and -out")
	}
	if *repeat < 1 {
		return errors.New("repeat must be at least 1")
	}
	instructions, err := os.ReadFile(*promptPath)
	if err != nil {
		return fmt.Errorf("read prompt file: %w", err)
	}
	cfg, err := eval.LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if model.set {
		cfg.Eval.Model = model.value
	}
	if provider.set {
		cfg.Eval.Provider = provider.value
	}
	if temperature.set {
		cfg.Eval.Temperature = &temperature.value
	}
	if maxTokens.set {
		cfg.Eval.MaxTokens = &maxTokens.value
	}
	dev, holdout, err := eval.LoadGold(*goldPath)
	if err != nil {
		return fmt.Errorf("load gold: %w", err)
	}
	var cases []eval.GoldCase
	switch *split {
	case "dev":
		cases = dev
	case "holdout":
		cases = holdout
	default:
		return fmt.Errorf("split must be dev or holdout, got %q", *split)
	}
	chatProvider, err := deps.chat(cfg.Eval, deps.getenv)
	if err != nil {
		return fmt.Errorf("create chat provider: %w", err)
	}
	embedProvider, err := deps.embed(cfg.Embedding.Provider, deps.getenv)
	if err != nil {
		return fmt.Errorf("create embedding provider: %w", err)
	}
	embedder := &agentkit.Embedder{Provider: embedProvider, Model: cfg.Embedding.Model, Dimensions: cfg.Embedding.Dimensions}
	cacheDir := deps.cacheDir
	if cacheDir == "" {
		cacheDir, err = os.MkdirTemp("", "eval-extract-cache-*")
		if err != nil {
			return fmt.Errorf("create temporary embedding cache: %w", err)
		}
		defer os.RemoveAll(cacheDir)
	}
	embed := eval.NewDiskCache(cacheDir, cfg.Embedding.Model, func(ctx context.Context, texts []string) ([][]float32, error) {
		result, err := embedder.Embed(ctx, texts, agentkit.InputDocument)
		if err != nil {
			return nil, err
		}
		return result.Vectors, nil
	})

	var first eval.Scorecard
	composites := make([]float64, 0, *repeat)
	for n := 0; n < *repeat; n++ {
		scores := make([]eval.CaseScore, 0, len(cases))
		for i, gold := range cases {
			got, err := extractCase(ctx, chatProvider, cfg.Eval, string(instructions), gold)
			if err != nil {
				return fmt.Errorf("case %s: %w", gold.Name, err)
			}
			score, err := eval.ScoreCase(ctx, gold, got, embed, cfg)
			if err != nil {
				return fmt.Errorf("case %s: %w", gold.Name, err)
			}
			scores = append(scores, score)
			fmt.Fprintf(stderr, "case %d/%d repeat %d/%d %s composite=%.6f\n", i+1, len(cases), n+1, *repeat, gold.Name, score.Composite)
		}
		card := eval.Aggregate(scores, cfg)
		if n == 0 {
			first = card
		}
		composites = append(composites, card.MeanComposite)
	}
	if *repeat > 1 {
		first.RunComposites = composites
		first.Epsilon = eval.Epsilon(composites)
	}
	data, err := first.MarshalDeterministic()
	if err != nil {
		return fmt.Errorf("marshal scorecard: %w", err)
	}
	if err := atomicWrite(*outPath, data); err != nil {
		return fmt.Errorf("write scorecard: %w", err)
	}
	return nil
}

func extractCase(ctx context.Context, provider agentkit.Provider, cfg eval.EvalCall, instructions string, gold eval.GoldCase) ([]extract.ExtractedSubject, error) {
	prompt := instructions + "\n\n" + extract.Render(gold.Header, gold.Document)
	return eval.ChatJSON(ctx, provider, cfg, prompt, func(response string) ([]extract.ExtractedSubject, error) {
		var envelope struct {
			Subjects []extract.ExtractedSubject `json:"subjects"`
		}
		err := json.Unmarshal([]byte(response), &envelope)
		if err == nil {
			err = extract.Validate(envelope.Subjects)
		}
		return envelope.Subjects, err
	})
}

func compare(args []string, stdout io.Writer) (bool, error) {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	candidatePath := fs.String("candidate", "", "candidate scorecard")
	baselinePath := fs.String("baseline", "", "baseline scorecard")
	if err := fs.Parse(args); err != nil {
		return false, fmt.Errorf("parse compare flags: %w", err)
	}
	if *candidatePath == "" || *baselinePath == "" {
		return false, errors.New("compare requires -candidate and -baseline")
	}
	var candidate, baseline eval.Scorecard
	if err := readJSON(*candidatePath, &candidate); err != nil {
		return false, fmt.Errorf("read candidate: %w", err)
	}
	if err := readJSON(*baselinePath, &baseline); err != nil {
		return false, fmt.Errorf("read baseline: %w", err)
	}
	accepted := eval.Accept(candidate.MeanComposite, baseline.MeanComposite, baseline.Epsilon)
	if accepted {
		fmt.Fprintln(stdout, "accept")
	} else {
		fmt.Fprintln(stdout, "reject")
	}
	return accepted, nil
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return err
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".scorecard-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err == nil {
		err = temp.Chmod(0o644)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func messageText(message agentkit.Message) string {
	return eval.MessageText(message)
}

func productionDependencies() dependencies {
	return dependencies{chat: buildConfiguredChatProvider, embed: buildEmbeddingProvider, getenv: os.Getenv, cacheDir: filepath.Join("tmp", "eval-extract", "embeddings")}
}

func buildConfiguredChatProvider(cfg eval.EvalCall, getenv func(string) string) (agentkit.Provider, error) {
	return eval.BuildConfiguredChatProvider(cfg, getenv)
}

func expandHome(path string) (string, error) {
	return eval.ExpandHome(path)
}

func buildChatProvider(name string, getenv func(string) string) (agentkit.Provider, error) {
	return eval.BuildChatProvider(name, getenv)
}

func buildEmbeddingProvider(name string, getenv func(string) string) (agentkit.EmbeddingProvider, error) {
	return eval.BuildEmbeddingProvider(name, getenv)
}

func requiredKey(name string, getenv func(string) string) (string, error) {
	return eval.RequiredKey(name, getenv)
}
