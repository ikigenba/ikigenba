package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
)

type Config struct {
	Eval      EvalCall  `json:"eval"`
	Embedding Embedding `json:"embedding"`
	Weights   Weights   `json:"weights"`
}

type EvalCall struct {
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	Temperature     *float64 `json:"temperature,omitempty"`
	Thinking        *bool    `json:"thinking,omitempty"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	MaxParseRetries *int     `json:"max_parse_retries,omitempty"`
	Auth            string   `json:"auth"`
	AuthFile        string   `json:"auth_file"`
}

type Embedding struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	Dimensions int     `json:"dimensions"`
	Threshold  float64 `json:"threshold"`
	Margin     float64 `json:"margin"`
}

type Weights struct {
	Subject float64 `json:"subject"`
	Claim   float64 `json:"claim"`
	Field   float64 `json:"field"`
}

type AnalysisConfig struct {
	Eval      EvalCall        `json:"eval"`
	Embedding Embedding       `json:"embedding"`
	Weights   AnalysisWeights `json:"weights"`
}

type AnalysisWeights struct {
	SubQueries float64 `json:"sub_queries"`
	Keywords   float64 `json:"keywords"`
	Aliases    float64 `json:"aliases"`
}

func LoadAnalysisConfig(path string) (AnalysisConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AnalysisConfig{}, fmt.Errorf("read analysis config: %w", err)
	}
	var raw struct {
		Eval *struct {
			Provider        *string  `json:"provider"`
			Model           *string  `json:"model"`
			Temperature     *float64 `json:"temperature"`
			Thinking        *bool    `json:"thinking"`
			MaxTokens       *int     `json:"max_tokens"`
			MaxParseRetries *int     `json:"max_parse_retries"`
			Auth            string   `json:"auth"`
			AuthFile        string   `json:"auth_file"`
		} `json:"eval"`
		Embedding *struct {
			Provider   *string  `json:"provider"`
			Model      *string  `json:"model"`
			Dimensions *int     `json:"dimensions"`
			Threshold  *float64 `json:"threshold"`
			Margin     *float64 `json:"margin"`
		} `json:"embedding"`
		Weights *struct {
			SubQueries *float64 `json:"sub_queries"`
			Keywords   *float64 `json:"keywords"`
			Aliases    *float64 `json:"aliases"`
		} `json:"weights"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return AnalysisConfig{}, fmt.Errorf("parse analysis config: %w", err)
	}
	if raw.Eval == nil {
		return AnalysisConfig{}, fmt.Errorf("missing required field eval")
	}
	if raw.Embedding == nil {
		return AnalysisConfig{}, fmt.Errorf("missing required field embedding")
	}
	if raw.Weights == nil {
		return AnalysisConfig{}, fmt.Errorf("missing required field weights")
	}
	fields := []struct {
		name  string
		value any
	}{
		{"eval.provider", raw.Eval.Provider}, {"eval.model", raw.Eval.Model},
		{"embedding.provider", raw.Embedding.Provider}, {"embedding.model", raw.Embedding.Model},
		{"embedding.dimensions", raw.Embedding.Dimensions}, {"embedding.threshold", raw.Embedding.Threshold},
		{"embedding.margin", raw.Embedding.Margin}, {"weights.sub_queries", raw.Weights.SubQueries},
		{"weights.keywords", raw.Weights.Keywords}, {"weights.aliases", raw.Weights.Aliases},
	}
	for _, field := range fields {
		if field.value == nil || (reflect.ValueOf(field.value).Kind() == reflect.Pointer && reflect.ValueOf(field.value).IsNil()) {
			return AnalysisConfig{}, fmt.Errorf("missing required field %s", field.name)
		}
	}
	auth := raw.Eval.Auth
	if auth == "" {
		auth = "key"
	}
	authFile := raw.Eval.AuthFile
	if authFile == "" {
		authFile = "~/.agentrepl/auth.json"
	}
	cfg := AnalysisConfig{
		Eval:      EvalCall{Provider: *raw.Eval.Provider, Model: *raw.Eval.Model, Temperature: raw.Eval.Temperature, Thinking: raw.Eval.Thinking, MaxTokens: raw.Eval.MaxTokens, MaxParseRetries: raw.Eval.MaxParseRetries, Auth: auth, AuthFile: authFile},
		Embedding: Embedding{Provider: *raw.Embedding.Provider, Model: *raw.Embedding.Model, Dimensions: *raw.Embedding.Dimensions, Threshold: *raw.Embedding.Threshold, Margin: *raw.Embedding.Margin},
		Weights:   AnalysisWeights{SubQueries: *raw.Weights.SubQueries, Keywords: *raw.Weights.Keywords, Aliases: *raw.Weights.Aliases},
	}
	if math.Abs(cfg.Weights.SubQueries+cfg.Weights.Keywords+cfg.Weights.Aliases-1) > 1e-9 {
		return AnalysisConfig{}, fmt.Errorf("analysis weights must sum to 1")
	}
	return cfg, nil
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var raw struct {
		Eval *struct {
			Provider        *string  `json:"provider"`
			Model           *string  `json:"model"`
			Temperature     *float64 `json:"temperature"`
			Thinking        *bool    `json:"thinking"`
			MaxTokens       *int     `json:"max_tokens"`
			MaxParseRetries *int     `json:"max_parse_retries"`
			Auth            string   `json:"auth"`
			AuthFile        string   `json:"auth_file"`
		} `json:"eval"`
		Embedding *struct {
			Provider   *string  `json:"provider"`
			Model      *string  `json:"model"`
			Dimensions *int     `json:"dimensions"`
			Threshold  *float64 `json:"threshold"`
			Margin     *float64 `json:"margin"`
		} `json:"embedding"`
		Weights *struct {
			Subject *float64 `json:"subject"`
			Claim   *float64 `json:"claim"`
			Field   *float64 `json:"field"`
		} `json:"weights"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if raw.Eval == nil {
		return Config{}, fmt.Errorf("missing required field eval")
	}
	if raw.Embedding == nil {
		return Config{}, fmt.Errorf("missing required field embedding")
	}
	if raw.Weights == nil {
		return Config{}, fmt.Errorf("missing required field weights")
	}
	if err := requireConfigFields(raw.Eval.Provider, raw.Eval.Model, raw.Embedding.Provider, raw.Embedding.Model, raw.Embedding.Dimensions, raw.Embedding.Threshold, raw.Embedding.Margin, raw.Weights.Subject, raw.Weights.Claim, raw.Weights.Field); err != nil {
		return Config{}, err
	}
	auth := raw.Eval.Auth
	if auth == "" {
		auth = "key"
	}
	authFile := raw.Eval.AuthFile
	if authFile == "" {
		authFile = "~/.agentrepl/auth.json"
	}
	cfg := Config{
		Eval:      EvalCall{Provider: *raw.Eval.Provider, Model: *raw.Eval.Model, Temperature: raw.Eval.Temperature, Thinking: raw.Eval.Thinking, MaxTokens: raw.Eval.MaxTokens, MaxParseRetries: raw.Eval.MaxParseRetries, Auth: auth, AuthFile: authFile},
		Embedding: Embedding{*raw.Embedding.Provider, *raw.Embedding.Model, *raw.Embedding.Dimensions, *raw.Embedding.Threshold, *raw.Embedding.Margin},
		Weights:   Weights{*raw.Weights.Subject, *raw.Weights.Claim, *raw.Weights.Field},
	}
	if math.Abs(cfg.Weights.Subject+cfg.Weights.Claim+cfg.Weights.Field-1) > 1e-9 {
		return Config{}, fmt.Errorf("field weights must sum to 1")
	}
	return cfg, nil
}

func requireConfigFields(fields ...any) error {
	names := []string{"eval.provider", "eval.model", "embedding.provider", "embedding.model", "embedding.dimensions", "embedding.threshold", "embedding.margin", "weights.subject", "weights.claim", "weights.field"}
	for i, field := range fields {
		if field == nil || (reflect.ValueOf(field).Kind() == reflect.Pointer && reflect.ValueOf(field).IsNil()) {
			return fmt.Errorf("missing required field %s", names[i])
		}
	}
	return nil
}
