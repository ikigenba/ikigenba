//go:build integration

package agentkit

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestOAuthRefreshXAI(t *testing.T) {
	path := os.Getenv("AGENTKIT_XAI_OAUTH_FILE")
	if path == "" {
		t.Skip("AGENTKIT_XAI_OAUTH_FILE is unset")
	}

	beforeData, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read xAI OAuth file: %v", err)
	}
	var before struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(beforeData, &before); err != nil {
		t.Fatalf("decode xAI OAuth file before refresh: %v", err)
	}

	offering, err := Lookup("grok-4.5", HostXAI, WireResponses)
	if err != nil {
		t.Fatalf("look up xAI responses offering: %v", err)
	}
	source, err := offering.TokenSource(FileTokenStore(path))
	if err != nil {
		t.Fatalf("construct xAI token source: %v", err)
	}
	if _, err := source.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh xAI OAuth token: %v", err)
	}

	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read xAI OAuth file after refresh: %v", err)
	}
	var after struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(afterData, &after); err != nil {
		t.Fatalf("decode xAI OAuth file after refresh: %v", err)
	}
	if after.AccessToken == before.AccessToken {
		t.Fatal("xAI access_token did not change after refresh")
	}
}
