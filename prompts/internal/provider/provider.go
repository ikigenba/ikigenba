// Package provider constructs AgentKit providers from pinned prompt config.
package provider

import (
	"fmt"
	"strings"

	"prompts/internal/prompt"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/anthropic"
	"github.com/ikigenba/agentkit/google"
	"github.com/ikigenba/agentkit/openai"
	"github.com/ikigenba/agentkit/openrouter"
	"github.com/ikigenba/agentkit/zai"
)

var keyNames = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"google":     "GEMINI_API_KEY",
	"zai":        "ZAI_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// Build constructs the configured completion provider.
func Build(cfg prompt.Config, getenv func(string) string) (agentkit.Provider, error) {
	key, err := apiKey(cfg.Provider, getenv)
	if err != nil {
		return nil, err
	}

	switch cfg.Provider {
	case "anthropic":
		var opts []anthropic.Option
		if cfg.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(cfg.BaseURL))
		}
		return anthropic.New(anthropic.APIKey(key), opts...), nil
	case "openai":
		var opts []openai.Option
		if cfg.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
		}
		return openai.New(openai.APIKey(key), opts...), nil
	case "google":
		var opts []google.Option
		if cfg.BaseURL != "" {
			opts = append(opts, google.WithBaseURL(cfg.BaseURL))
		}
		return google.New(google.APIKey(key), opts...), nil
	case "zai":
		var opts []zai.Option
		if cfg.BaseURL != "" {
			opts = append(opts, zai.WithBaseURL(cfg.BaseURL))
		}
		return zai.New(zai.APIKey(key), opts...), nil
	case "openrouter":
		var opts []openrouter.Option
		if cfg.BaseURL != "" {
			opts = append(opts, openrouter.WithBaseURL(cfg.BaseURL))
		}
		return openrouter.New(openrouter.APIKey(key), opts...), nil
	default:
		panic("unreachable")
	}
}

// BuildEmbedder constructs an embedding provider for a supported provider.
func BuildEmbedder(providerName string, getenv func(string) string) (agentkit.EmbeddingProvider, error) {
	if providerName != "openai" && providerName != "google" {
		return nil, fmt.Errorf("provider %q does not support embeddings", providerName)
	}
	key, err := apiKey(providerName, getenv)
	if err != nil {
		return nil, err
	}

	switch providerName {
	case "openai":
		return openai.NewEmbedder(openai.APIKey(key)), nil
	case "google":
		return google.NewEmbedder(google.APIKey(key)), nil
	default:
		panic("unreachable")
	}
}

func apiKey(providerName string, getenv func(string) string) (string, error) {
	keyName := keyNames[providerName]
	if keyName == "" {
		return "", fmt.Errorf("unsupported provider %q", providerName)
	}
	key := strings.TrimSpace(getenv(keyName))
	if key == "" {
		return "", fmt.Errorf("%s is not set", keyName)
	}
	return key, nil
}
