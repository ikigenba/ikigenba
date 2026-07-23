package wiki

import (
	"fmt"
	"strconv"
	"strings"

	"wiki/internal/compile"
	"wiki/internal/extract"
	"wiki/internal/llm"
)

type reasoningLevel string

func (level reasoningLevel) Level() (string, bool) { return string(level), level != "" }

const defaultMaxTokens = 16384
const defaultEmbedModel = "text-embedding-3-small"
const defaultEmbedDims = 512

// CallSites carries wiki's per-stage generation settings.
type CallSites struct {
	Extract      llm.CallSite
	Compile      llm.CallSite
	AskSubject   llm.CallSite
	AskSynthesis llm.CallSite
}

// EmbedSite carries wiki's embedding model settings.
type EmbedSite struct {
	Model string
	Dims  int
}

// Config is wiki's service-side runtime configuration.
type Config struct {
	CallSites         CallSites
	EmbedSite         EmbedSite
	LLM               *llm.Client
	WorkerConcurrency int
	SearchDefault     int
	SearchCap         int
}

// NewConfig reads service configuration. Chat inference is supplied by prompts.
func NewConfig(getenv func(string) string) (Config, error) {
	callSites, err := resolveCallSites(getenv)
	if err != nil {
		return Config{}, err
	}
	embedSite, err := resolveEmbedSite(getenv)
	if err != nil {
		return Config{}, err
	}
	return Config{
		CallSites:         callSites,
		EmbedSite:         embedSite,
		WorkerConcurrency: WorkerConcurrency,
		SearchDefault:     SearchDefault,
		SearchCap:         SearchCap,
	}, nil
}

func resolveEmbedSite(getenv func(string) string) (EmbedSite, error) {
	site := EmbedSite{
		Model: defaultEmbedModel,
		Dims:  defaultEmbedDims,
	}
	if model := strings.TrimSpace(getenv("EMBED_MODEL")); model != "" {
		site.Model = model
	}
	if raw := strings.TrimSpace(getenv("EMBED_DIMS")); raw != "" {
		dims, err := strconv.Atoi(raw)
		if err != nil {
			return EmbedSite{}, fmt.Errorf("EMBED_DIMS: %w", err)
		}
		if dims <= 0 {
			return EmbedSite{}, fmt.Errorf("EMBED_DIMS: must be greater than zero")
		}
		site.Dims = dims
	}
	return site, nil
}

func resolveCallSites(getenv func(string) string) (CallSites, error) {
	extractSite, err := resolveCallSite(getenv, "EXTRACT", extract.DefaultCallSite())
	if err != nil {
		return CallSites{}, err
	}
	compileSite, err := resolveCallSite(getenv, "COMPILE", compile.DefaultCallSite())
	if err != nil {
		return CallSites{}, err
	}
	askSubject, err := resolveCallSite(getenv, "ASK_SUBJECT", defaultAskSubjectCallSite())
	if err != nil {
		return CallSites{}, err
	}
	askSynthesis, err := resolveCallSite(getenv, "ASK_SYNTHESIS", defaultAskSynthesisCallSite())
	if err != nil {
		return CallSites{}, err
	}
	return CallSites{
		Extract:      extractSite,
		Compile:      compileSite,
		AskSubject:   askSubject,
		AskSynthesis: askSynthesis,
	}, nil
}

// resolveCallSite layers <PREFIX>_MODEL / _REASONING / _TEMPERATURE / _MAX_TOKENS onto base.
func resolveCallSite(getenv func(string) string, prefix string, base llm.CallSite) (llm.CallSite, error) {
	site := base
	if model := strings.TrimSpace(getenv(prefix + "_MODEL")); model != "" {
		site.Config.Model = model
		site.Model = model
	}
	if raw := strings.TrimSpace(getenv(prefix + "_REASONING")); raw != "" {
		effort, thinking, err := parseReasoning(raw)
		if err != nil {
			return llm.CallSite{}, fmt.Errorf("%s_REASONING: %w", prefix, err)
		}
		site.Config.Effort = effort
		site.Config.Thinking = thinking
		if thinking != nil {
			site.Reasoning = llm.DisableReasoning()
		} else {
			site.Reasoning = reasoningLevel(effort)
		}
	}
	if raw := strings.TrimSpace(getenv(prefix + "_TEMPERATURE")); raw != "" {
		temp, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return llm.CallSite{}, fmt.Errorf("%s_TEMPERATURE: %w", prefix, err)
		}
		site.Config.Temperature = &temp
		site.Temperature = &temp
	}
	if raw := strings.TrimSpace(getenv(prefix + "_MAX_TOKENS")); raw != "" {
		maxTokens, err := strconv.Atoi(raw)
		if err != nil {
			return llm.CallSite{}, fmt.Errorf("%s_MAX_TOKENS: %w", prefix, err)
		}
		if maxTokens <= 0 {
			return llm.CallSite{}, fmt.Errorf("%s_MAX_TOKENS: must be greater than zero", prefix)
		}
		site.Config.MaxTokens = maxTokens
		site.MaxTokens = maxTokens
	}
	return site, nil
}

func parseReasoning(raw string) (string, *bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "disabled", "off":
		value := false
		return "", &value, nil
	case "minimal", "low", "medium", "high", "xhigh", "max", "none":
		return strings.ToLower(strings.TrimSpace(raw)), nil, nil
	default:
		return "", nil, fmt.Errorf("must be disabled, off, or a native reasoning level")
	}
}

func defaultAskSubjectCallSite() llm.CallSite {
	return llm.CallSite{Stage: "ask-subject", Config: llm.Config{Model: ModelID, Effort: "low", MaxTokens: defaultMaxTokens}, Model: ModelID, Reasoning: reasoningLevel("low"), MaxTokens: defaultMaxTokens}
}

func defaultAskSynthesisCallSite() llm.CallSite {
	return llm.CallSite{Stage: "ask-synthesis", Config: llm.Config{Model: ModelID, Effort: "low", MaxTokens: defaultMaxTokens}, Model: ModelID, Reasoning: reasoningLevel("low"), MaxTokens: defaultMaxTokens}
}
