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
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	Temperature     int    `json:"temperature"`
	Thinking        bool   `json:"thinking"`
	MaxTokens       int    `json:"max_tokens"`
	MaxParseRetries int    `json:"max_parse_retries"`
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

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var raw struct {
		Eval *struct {
			Provider        *string `json:"provider"`
			Model           *string `json:"model"`
			Temperature     *int    `json:"temperature"`
			Thinking        *bool   `json:"thinking"`
			MaxTokens       *int    `json:"max_tokens"`
			MaxParseRetries *int    `json:"max_parse_retries"`
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
	if err := requireConfigFields(raw.Eval.Provider, raw.Eval.Model, raw.Eval.Temperature, raw.Eval.Thinking, raw.Eval.MaxTokens, raw.Eval.MaxParseRetries, raw.Embedding.Provider, raw.Embedding.Model, raw.Embedding.Dimensions, raw.Embedding.Threshold, raw.Embedding.Margin, raw.Weights.Subject, raw.Weights.Claim, raw.Weights.Field); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Eval:      EvalCall{*raw.Eval.Provider, *raw.Eval.Model, *raw.Eval.Temperature, *raw.Eval.Thinking, *raw.Eval.MaxTokens, *raw.Eval.MaxParseRetries},
		Embedding: Embedding{*raw.Embedding.Provider, *raw.Embedding.Model, *raw.Embedding.Dimensions, *raw.Embedding.Threshold, *raw.Embedding.Margin},
		Weights:   Weights{*raw.Weights.Subject, *raw.Weights.Claim, *raw.Weights.Field},
	}
	if math.Abs(cfg.Weights.Subject+cfg.Weights.Claim+cfg.Weights.Field-1) > 1e-9 {
		return Config{}, fmt.Errorf("field weights must sum to 1")
	}
	return cfg, nil
}

func requireConfigFields(fields ...any) error {
	names := []string{"eval.provider", "eval.model", "eval.temperature", "eval.thinking", "eval.max_tokens", "eval.max_parse_retries", "embedding.provider", "embedding.model", "embedding.dimensions", "embedding.threshold", "embedding.margin", "weights.subject", "weights.claim", "weights.field"}
	for i, field := range fields {
		if field == nil || (reflect.ValueOf(field).Kind() == reflect.Pointer && reflect.ValueOf(field).IsNil()) {
			return fmt.Errorf("missing required field %s", names[i])
		}
	}
	return nil
}
