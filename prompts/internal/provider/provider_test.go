package provider

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"prompts/internal/prompt"
)

func TestBuildConstructsEveryConfiguredProvider(t *testing.T) {
	// R-K5I9-YGS9
	tests := []struct {
		provider string
		envKey   string
		name     string
	}{
		{provider: "anthropic", envKey: "ANTHROPIC_API_KEY", name: "anthropic"},
		{provider: "openai", envKey: "OPENAI_API_KEY", name: "openai.apikey"},
		{provider: "google", envKey: "GEMINI_API_KEY", name: "google"},
		{provider: "zai", envKey: "ZAI_API_KEY", name: "zai"},
		{provider: "openrouter", envKey: "OPENROUTER_API_KEY", name: "openrouter"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			var requested string
			built, err := Build(prompt.Config{
				Provider: tt.provider,
				BaseURL:  "https://provider.example.test/v1",
			}, func(key string) string {
				requested = key
				return " test-key "
			})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if requested != tt.envKey {
				t.Fatalf("requested env key = %q, want %q", requested, tt.envKey)
			}
			if got := built.Name(); got != tt.name {
				t.Fatalf("provider name = %q, want %q", got, tt.name)
			}
		})
	}
}

func TestNewBuilderSelectsSubscriptionAndKeyCredentials(t *testing.T) {
	// R-T319-YQNF
	path := filepath.Join(t.TempDir(), "auth.json")
	writeSubscriptionTokens(t, path)
	build := NewBuilder(NewSubAuth(path))

	sub, err := build(prompt.Config{Provider: "openai", Auth: "sub"}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("subscription build: %v", err)
	}
	if got := sub.Name(); got != "openai.subscription" {
		t.Fatalf("subscription provider name = %q", got)
	}
	for _, auth := range []string{"", "key"} {
		key, err := build(prompt.Config{Provider: "openai", Auth: auth}, func(string) string { return "sk-test" })
		if err != nil {
			t.Fatalf("key build (%q): %v", auth, err)
		}
		if got := key.Name(); got != "openai.apikey" {
			t.Fatalf("key provider name (%q) = %q", auth, got)
		}
	}
}

func TestNewBuilderCachesLoadedSubscriptionStore(t *testing.T) {
	// R-T496-CIE4
	path := filepath.Join(t.TempDir(), "auth.json")
	writeSubscriptionTokens(t, path)
	build := NewBuilder(NewSubAuth(path))
	cfg := prompt.Config{Provider: "openai", Auth: "sub"}
	if _, err := build(cfg, func(string) string { return "" }); err != nil {
		t.Fatalf("first build: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove credential: %v", err)
	}
	got, err := build(cfg, func(string) string { return "" })
	if err != nil || got.Name() != "openai.subscription" {
		t.Fatalf("cached build = %v, %v", got, err)
	}
}

func TestNewBuilderErrorsNameCredentialPath(t *testing.T) {
	// R-T6OZ-41VI
	for _, tc := range []struct {
		name    string
		content *string
	}{
		{name: "missing"},
		{name: "invalid", content: stringPointer("{")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name+"-auth.json")
			if tc.content != nil {
				if err := os.WriteFile(path, []byte(*tc.content), 0o600); err != nil {
					t.Fatalf("write invalid credential: %v", err)
				}
			}
			_, err := NewBuilder(NewSubAuth(path))(prompt.Config{Provider: "openai", Auth: "sub"}, func(string) string { return "" })
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v, want configured path %q", err, path)
			}
		})
	}
}

func TestResolveAuthPathUsesOverrideOrDatabaseDirectory(t *testing.T) {
	// R-T7WV-HTM7
	override := "/srv/secrets/openai.json"
	if got := ResolveAuthPath(func(key string) string {
		if key == "PROMPTS_OPENAI_AUTH_PATH" {
			return override
		}
		return ""
	}); got != override {
		t.Fatalf("override path = %q", got)
	}
	dbPath := filepath.Join("var", "lib", "prompts", "prompts.db")
	want := filepath.Join(filepath.Dir(dbPath), "auth.json")
	if got := ResolveAuthPath(func(key string) string {
		if key == "PROMPTS_DB_PATH" {
			return dbPath
		}
		return ""
	}); got != want {
		t.Fatalf("default path = %q, want %q", got, want)
	}
}

func writeSubscriptionTokens(t *testing.T, path string) {
	t.Helper()
	claims, err := json.Marshal(map[string]any{
		"exp":                         time.Now().Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "acct-test"},
	})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	token := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	raw, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		t.Fatalf("marshal token response: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write token response: %v", err)
	}
}

func stringPointer(value string) *string { return &value }

func TestBuildRejectsMissingKeyAndUnknownProvider(t *testing.T) {
	if _, err := Build(prompt.Config{Provider: "openai"}, func(string) string { return "  " }); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("missing-key error = %v", err)
	}
	if _, err := Build(prompt.Config{Provider: "unknown"}, func(string) string { return "key" }); err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unknown-provider error = %v", err)
	}
}

func TestBuildEmbedderSupportsOpenAIAndGoogleOnly(t *testing.T) {
	for _, providerName := range []string{"openai", "google"} {
		embedder, err := BuildEmbedder(providerName, func(string) string { return "key" })
		if err != nil {
			t.Fatalf("BuildEmbedder(%q): %v", providerName, err)
		}
		if embedder == nil || embedder.Name() == "" {
			t.Fatalf("BuildEmbedder(%q) returned invalid embedder", providerName)
		}
	}
	if _, err := BuildEmbedder("anthropic", func(string) string { return "key" }); err == nil || !strings.Contains(err.Error(), "does not support embeddings") {
		t.Fatalf("unsupported embedder error = %v", err)
	}
}
