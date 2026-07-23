package wiki

import (
	"reflect"
	"strings"
	"testing"

	"wiki/internal/llm"
)

func TestNewConfigUsesEveryStageDefaultLunaCallSite(t *testing.T) {
	// R-A25B-A1V6
	cfg, err := NewConfig(fakeGetenv(map[string]string{}))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.LLM != nil {
		t.Fatal("NewConfig should leave the prompts client to the composition root")
	}
	assertProductionSite(t, cfg.CallSites.Extract, "extract", 2)
	assertProductionSite(t, cfg.CallSites.Compile, "compile", 0)
	assertProductionSite(t, cfg.CallSites.AskSubject, "ask-subject", 0)
	assertProductionSite(t, cfg.CallSites.AskSynthesis, "ask-synthesis", 0)
	if cfg.EmbedSite.Model != "text-embedding-3-small" || cfg.EmbedSite.Dims != 512 {
		t.Fatalf("EmbedSite = %#v, want default OpenAI small embeddings at 512 dims", cfg.EmbedSite)
	}
}

func TestNewConfigLayersPerCallSiteEnvironmentOverrides(t *testing.T) {
	// R-GK65-FYFZ
	cfg, err := NewConfig(fakeGetenv(map[string]string{
		"EXTRACT_MODEL":            "extract-model",
		"EXTRACT_TEMPERATURE":      "0.25",
		"COMPILE_MODEL":            "compile-model",
		"COMPILE_MAX_TOKENS":       "4096",
		"ASK_SUBJECT_MODEL":        "subject-model",
		"ASK_SUBJECT_REASONING":    "high",
		"ASK_SYNTHESIS_MODEL":      "synthesis-model",
		"ASK_SYNTHESIS_REASONING":  "disabled",
		"ASK_SYNTHESIS_MAX_TOKENS": "8192",
	}))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	assertResolvedSite(t, cfg.CallSites.Extract, "extract", "extract-model", 0.25, nil, 0, 2)
	assertResolvedSite(t, cfg.CallSites.Compile, "compile", "compile-model", nil, nil, 4096, 0)
	assertResolvedSite(t, cfg.CallSites.AskSubject, "ask-subject", "subject-model", nil, reasoningLevel("high"), 0, 0)
	assertResolvedSite(t, cfg.CallSites.AskSynthesis, "ask-synthesis", "synthesis-model", nil, llm.DisableReasoning(), 8192, 0)
}

func TestNewConfigBuildsDefaultEmbeddingSite(t *testing.T) {
	// R-Z932-H2RA
	cfg, err := NewConfig(fakeGetenv(map[string]string{}))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.EmbedSite.Model != "text-embedding-3-small" {
		t.Fatalf("EmbedSite.Model = %q, want OpenAI small embedding model", cfg.EmbedSite.Model)
	}
	if cfg.EmbedSite.Dims != 512 {
		t.Fatalf("EmbedSite.Dims = %d, want 512", cfg.EmbedSite.Dims)
	}
}

func TestNewConfigLayersEmbeddingEnvironmentOverrides(t *testing.T) {
	// R-Z932-H2RA
	// R-ZAAY-UUHZ
	cfg, err := NewConfig(fakeGetenv(map[string]string{
		"EMBED_MODEL": "text-embedding-3-large",
		"EMBED_DIMS":  "1024",
	}))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.EmbedSite.Model != "text-embedding-3-large" || cfg.EmbedSite.Dims != 1024 {
		t.Fatalf("EmbedSite = %#v, want env-selected model and dims", cfg.EmbedSite)
	}
}

func TestNewConfigRejectsMalformedCallSiteEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "temperature",
			env: map[string]string{
				"EXTRACT_TEMPERATURE": "warm",
			},
			wantErr: "EXTRACT_TEMPERATURE",
		},
		{
			name: "max tokens",
			env: map[string]string{
				"COMPILE_MAX_TOKENS": "0",
			},
			wantErr: "COMPILE_MAX_TOKENS",
		},
		{
			name: "reasoning",
			env: map[string]string{
				"ASK_SUBJECT_REASONING": "turbo",
			},
			wantErr: "ASK_SUBJECT_REASONING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// R-GLE1-TQ6O
			_, err := NewConfig(fakeGetenv(tt.env))
			if err == nil {
				t.Fatal("NewConfig returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewConfigRejectsMalformedEmbeddingEnvironment(t *testing.T) {
	// R-Z932-H2RA
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "non numeric dims",
			env: map[string]string{
				"EMBED_DIMS": "wide",
			},
			wantErr: "EMBED_DIMS",
		},
		{
			name: "zero dims",
			env: map[string]string{
				"EMBED_DIMS": "0",
			},
			wantErr: "EMBED_DIMS",
		},
		{
			name: "negative dims",
			env: map[string]string{
				"EMBED_DIMS": "-1",
			},
			wantErr: "EMBED_DIMS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConfig(fakeGetenv(tt.env))
			if err == nil {
				t.Fatal("NewConfig returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func assertResolvedSite(t *testing.T, got llm.CallSite, stage, model string, temp any, reasoning any, maxTokens, maxParseRetries int) {
	t.Helper()
	if got.Stage != stage {
		t.Fatalf("stage = %q, want %q", got.Stage, stage)
	}
	if got.Model != model {
		t.Fatalf("%s model = %q, want %q", stage, got.Model, model)
	}
	if temp == nil {
		if got.Temperature != nil {
			t.Fatalf("%s temperature = %#v, want nil", stage, got.Temperature)
		}
	} else {
		var wantTemp float64
		switch v := temp.(type) {
		case float64:
			wantTemp = v
		case int:
			wantTemp = float64(v)
		default:
			t.Fatalf("unsupported temperature expectation type %T", temp)
		}
		if got.Temperature == nil || *got.Temperature != wantTemp {
			t.Fatalf("%s temperature = %#v, want %v", stage, got.Temperature, wantTemp)
		}
	}
	if !reflect.DeepEqual(got.Reasoning, reasoning) {
		t.Fatalf("%s reasoning = %#v, want %#v", stage, got.Reasoning, reasoning)
	}
	if got.MaxTokens != maxTokens {
		t.Fatalf("%s MaxTokens = %d, want %d", stage, got.MaxTokens, maxTokens)
	}
	if got.MaxParseRetries != maxParseRetries {
		t.Fatalf("%s MaxParseRetries = %d, want %d", stage, got.MaxParseRetries, maxParseRetries)
	}
}

func assertProductionSite(t *testing.T, got llm.CallSite, stage string, maxParseRetries int) {
	t.Helper()
	if got.Stage != stage || got.System == "" {
		t.Fatalf("%s site = %#v, want stage and embedded system prompt", stage, got)
	}
	if got.Config.Provider != "openai" || got.Config.Model != "gpt-5.6-luna" || got.Config.Effort != "low" || got.Config.MaxTokens != 16384 {
		t.Fatalf("%s config = %#v, want openai Luna low/16384", stage, got.Config)
	}
	if got.Config.Temperature != nil || got.Config.Thinking != nil || got.Temperature != nil || got.Reasoning != nil {
		t.Fatalf("%s site = %#v, want no temperature or thinking pins", stage, got)
	}
	if got.MaxParseRetries != maxParseRetries {
		t.Fatalf("%s MaxParseRetries = %d, want %d", stage, got.MaxParseRetries, maxParseRetries)
	}
}

func fakeGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
