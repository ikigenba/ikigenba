package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/model"
)

func TestOpenRequiresTheResolvedAPIKeyEnvironmentVariable(t *testing.T) {
	// R-IBYV-CGX8
	cfg := openAIChatConfig(t)
	const envVar = "OPENAI_API_KEY"
	var requested []string
	cfg.Getenv = func(name string) string {
		requested = append(requested, name)
		return "fixed-test-key"
	}
	if _, err := model.Open(cfg); err != nil {
		t.Fatalf("Open with API key: %v", err)
	}
	if len(requested) != 1 || requested[0] != envVar {
		t.Fatalf("Getenv calls = %v, want exactly [%s]", requested, envVar)
	}

	requested = nil
	cfg.Getenv = func(name string) string {
		requested = append(requested, name)
		return ""
	}
	_, err := model.Open(cfg)
	if err == nil || !strings.Contains(err.Error(), envVar) {
		t.Fatalf("Open with empty API key error = %v, want variable %q", err, envVar)
	}
	if len(requested) != 1 || requested[0] != envVar {
		t.Fatalf("empty-key Getenv calls = %v, want exactly [%s]", requested, envVar)
	}
}

func TestOpenValidatesOAuthTokenFileImmediately(t *testing.T) {
	// R-IBYV-CGX8
	base, _ := oauthConfig(t)
	base.Auth = string(agentkit.AuthModeOAuth)

	tests := []struct {
		name  string
		setup func(*testing.T, string)
		ok    bool
	}{
		{name: "missing"},
		{name: "unreadable", setup: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed", setup: writeOAuthFixture("not-json")},
		{name: "no token", setup: writeOAuthFixture(`{"access_token":""}`)},
		{name: "valid", setup: writeOAuthFixture(`{"access_token":"test-oauth-token"}`), ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.AuthFile = filepath.Join(t.TempDir(), "oauth.json")
			if test.setup != nil {
				test.setup(t, cfg.AuthFile)
			}
			_, err := model.Open(cfg)
			if test.ok {
				if err != nil {
					t.Fatalf("Open valid OAuth file: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), cfg.AuthFile) {
				t.Fatalf("Open error = %v, want OAuth file %q", err, cfg.AuthFile)
			}
		})
	}
}

func writeOAuthFixture(contents string) func(*testing.T, string) {
	return func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
