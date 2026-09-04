//go:build integration

package agentkit

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestOAuthRefreshOpenAI(t *testing.T) {
	path := os.Getenv("AGENTKIT_OPENAI_OAUTH_FILE")
	if path == "" {
		t.Skip("AGENTKIT_OPENAI_OAUTH_FILE is unset")
	}

	beforeData, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read OpenAI OAuth file: %v", err)
	}
	var before struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(beforeData, &before); err != nil {
		t.Fatalf("decode OpenAI OAuth file before refresh: %v", err)
	}

	offering, err := Lookup("gpt-5.6-sol", HostOpenAI, WireResponses)
	if err != nil {
		t.Fatalf("look up OpenAI responses offering: %v", err)
	}
	source, err := offering.TokenSource(FileTokenStore(path))
	if err != nil {
		t.Fatalf("construct OpenAI token source: %v", err)
	}
	if _, err := source.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh OpenAI OAuth token: %v", err)
	}

	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAI OAuth file after refresh: %v", err)
	}
	var after struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(afterData, &after); err != nil {
		t.Fatalf("decode OpenAI OAuth file after refresh: %v", err)
	}
	if after.AccessToken == before.AccessToken {
		t.Fatal("OpenAI access_token did not change after refresh")
	}
}
