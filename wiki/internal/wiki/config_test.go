package wiki

import (
	"reflect"
	"strings"
	"testing"

	"wiki/internal/llm"
)

func TestNewConfigBuildsDefaultPerCallSiteModels(t *testing.T) {
	// R-GIY9-26PA
	cfg, err := NewConfig(fakeGetenv(map[string]string{}))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.LLM != nil {
		t.Fatal("NewConfig should leave the prompts client to the composition root")
	}
	assertResolvedSite(t, cfg.CallSites.Extract, "extract", ModelID, 0, llm.DisableReasoning(), 16384, 2)
	assertResolvedSite(t, cfg.CallSites.Compile, "compile", ModelID, 0, llm.DisableReasoning(), 16384, 2)
	assertResolvedSite(t, cfg.CallSites.AskSubject, "ask-subject", ModelID, nil, reasoningLevel("low"), 16384, 0)
	assertResolvedSite(t, cfg.CallSites.AskSynthesis, "ask-synthesis", ModelID, nil, reasoningLevel("low"), 16384, 0)
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

	assertResolvedSite(t, cfg.CallSites.Extract, "extract", "extract-model", 0.25, llm.DisableReasoning(), 16384, 2)
	assertResolvedSite(t, cfg.CallSites.Compile, "compile", "compile-model", 0, llm.DisableReasoning(), 4096, 2)
	assertResolvedSite(t, cfg.CallSites.AskSubject, "ask-subject", "subject-model", nil, reasoningLevel("high"), 16384, 0)
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

func fakeGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
