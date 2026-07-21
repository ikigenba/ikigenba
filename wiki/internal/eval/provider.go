package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/anthropic"
	"github.com/ikigenba/agentkit/google"
	"github.com/ikigenba/agentkit/openai"
	"github.com/ikigenba/agentkit/openai/subscription"
	"github.com/ikigenba/agentkit/openrouter"
	"github.com/ikigenba/agentkit/zai"
)

func BuildConfiguredChatProvider(cfg EvalCall, getenv func(string) string) (agentkit.Provider, error) {
	switch cfg.Auth {
	case "", "key":
		return BuildChatProvider(cfg.Provider, getenv)
	case "sub":
		if cfg.Provider != "openai" {
			return nil, fmt.Errorf("subscription auth is unsupported for provider %q", cfg.Provider)
		}
		path, err := ExpandHome(cfg.AuthFile)
		if err != nil {
			return nil, fmt.Errorf("load subscription auth file %q: %w", cfg.AuthFile, err)
		}
		store, err := subscription.Load(path)
		if err != nil {
			return nil, fmt.Errorf("load subscription auth file %q: %w", cfg.AuthFile, err)
		}
		return openai.New(openai.Subscription(store)), nil
	default:
		return nil, fmt.Errorf("unsupported auth %q", cfg.Auth)
	}
}

func BuildChatProvider(name string, getenv func(string) string) (agentkit.Provider, error) {
	constructors := map[string]struct {
		key string
		new func(string) agentkit.Provider
	}{
		"anthropic":  {"ANTHROPIC_API_KEY", func(key string) agentkit.Provider { return anthropic.New(anthropic.APIKey(key)) }},
		"google":     {"GEMINI_API_KEY", func(key string) agentkit.Provider { return google.New(google.APIKey(key)) }},
		"openai":     {"OPENAI_API_KEY", func(key string) agentkit.Provider { return openai.New(openai.APIKey(key)) }},
		"openrouter": {"OPENROUTER_API_KEY", func(key string) agentkit.Provider { return openrouter.New(openrouter.APIKey(key)) }},
		"zai":        {"ZAI_API_KEY", func(key string) agentkit.Provider { return zai.New(zai.APIKey(key)) }},
	}
	constructor, ok := constructors[name]
	if !ok {
		return nil, fmt.Errorf("unsupported chat provider %q", name)
	}
	key, err := RequiredKey(constructor.key, getenv)
	if err != nil {
		return nil, err
	}
	return constructor.new(key), nil
}

func BuildEmbeddingProvider(name string, getenv func(string) string) (agentkit.EmbeddingProvider, error) {
	if name != "openai" {
		return nil, fmt.Errorf("unsupported embedding provider %q", name)
	}
	key, err := RequiredKey("OPENAI_API_KEY", getenv)
	if err != nil {
		return nil, err
	}
	return openai.NewEmbedder(openai.APIKey(key)), nil
}

func RequiredKey(name string, getenv func(string) string) (string, error) {
	alias := "EVAL_" + name
	if value := strings.TrimSpace(getenv(alias)); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(getenv(name)); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("neither %s nor %s is set", alias, name)
}

func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

// ChatJSON runs one chat request and retries responses rejected by parse.
func ChatJSON[T any](ctx context.Context, provider agentkit.Provider, cfg EvalCall, prompt string, parse func(string) (T, error)) (T, error) {
	maxParseRetries := 2
	if cfg.MaxParseRetries != nil {
		maxParseRetries = *cfg.MaxParseRetries
	}
	original := prompt
	for attempt := 0; attempt <= maxParseRetries; attempt++ {
		var gen agentkit.GenSettings
		gen.Temperature = cfg.Temperature
		if cfg.MaxTokens != nil {
			gen.MaxTokens = *cfg.MaxTokens
		}
		if cfg.Thinking != nil && !*cfg.Thinking {
			gen.Reasoning = agentkit.DisableReasoning()
		}
		conv := &agentkit.Conversation{Provider: provider, Model: cfg.Model, Gen: gen}
		stream := conv.Send(ctx, prompt)
		var response string
		for event := range stream.Events() {
			if done, ok := event.(agentkit.MessageDone); ok {
				response = MessageText(done.Message)
			}
		}
		streamErr := stream.Err()
		_ = conv.Close()
		if streamErr != nil {
			var zero T
			return zero, fmt.Errorf("chat call: %w", streamErr)
		}
		result, err := parse(response)
		if err == nil {
			return result, nil
		}
		if attempt == maxParseRetries {
			var zero T
			return zero, fmt.Errorf("response validation exhausted retries: %w", err)
		}
		prompt = original + "\n\nYour previous response was invalid (" + err.Error() + "). Return corrected JSON only."
	}
	panic("unreachable")
}

func MessageText(message agentkit.Message) string {
	var b strings.Builder
	for _, block := range message.Blocks {
		if text, ok := block.(agentkit.TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
