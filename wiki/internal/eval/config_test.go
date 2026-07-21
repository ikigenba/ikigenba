package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigPreservesPinsAndRejectsInvalidFields(t *testing.T) {
	// R-KJKV-RYMZ
	cfg, err := LoadConfig(filepath.Join("..", "..", "eval", "extract", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Eval.Model != "claude-sonnet-4-6" || cfg.Eval.Temperature != 0 || cfg.Eval.Thinking || cfg.Eval.MaxTokens != 16384 || cfg.Eval.MaxParseRetries != 2 {
		t.Fatalf("unexpected eval pin: %+v", cfg.Eval)
	}
	if cfg.Eval.Auth != "key" || cfg.Eval.AuthFile != "~/.agentrepl/auth.json" {
		t.Fatalf("unexpected auth defaults: %+v", cfg.Eval)
	}
	cfg.Eval.Auth = "sub"
	cfg.Eval.AuthFile = "/tmp/test-auth.json"
	explicitPath := filepath.Join(t.TempDir(), "explicit-auth.json")
	explicitData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(explicitPath, explicitData, 0o600); err != nil {
		t.Fatal(err)
	}
	explicit, err := LoadConfig(explicitPath)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Eval.Auth != "sub" || explicit.Eval.AuthFile != "/tmp/test-auth.json" {
		t.Fatalf("explicit auth settings were not loaded: %+v", explicit.Eval)
	}
	if cfg.Embedding.Model != "text-embedding-3-small" || cfg.Embedding.Dimensions != 1536 || cfg.Embedding.Threshold != 0.80 || cfg.Embedding.Margin != 0.03 {
		t.Fatalf("unexpected embedding pin: %+v", cfg.Embedding)
	}
	if sum := cfg.Weights.Subject + cfg.Weights.Claim + cfg.Weights.Field; sum != 1 {
		t.Fatalf("weights sum = %v", sum)
	}

	for name, body := range map[string]string{
		"missing max_tokens": `{"eval":{"provider":"a","model":"m","temperature":0,"thinking":false,"max_parse_retries":2},"embedding":{"provider":"o","model":"e","dimensions":3,"threshold":0.8,"margin":0.03},"weights":{"subject":0.35,"claim":0.5,"field":0.15}}`,
		"bad weights":        `{"eval":{"provider":"a","model":"m","temperature":0,"thinking":false,"max_tokens":1,"max_parse_retries":2},"embedding":{"provider":"o","model":"e","dimensions":3,"threshold":0.8,"margin":0.03},"weights":{"subject":0.4,"claim":0.5,"field":0.15}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			if err == nil || (!strings.Contains(err.Error(), "max_tokens") && !strings.Contains(err.Error(), "weights")) {
				t.Fatalf("expected named field error, got %v", err)
			}
		})
	}
}
