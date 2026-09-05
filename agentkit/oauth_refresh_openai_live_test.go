//go:build live

package agentkit

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestLiveOAuthRefreshOpenAI(t *testing.T) {
	path := os.Getenv("AGENTKIT_OPENAI_OAUTH_FILE")
	if path == "" {
		t.Fatal("AGENTKIT_OPENAI_OAUTH_FILE is unset")
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAI OAuth file: %v", err)
	}
	var beforeToken struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(before, &beforeToken); err != nil {
		t.Fatalf("decode OpenAI OAuth file before rotation: %v", err)
	}

	offering, err := Lookup("gpt-5.4-mini", HostOpenAI, WireResponses)
	if err != nil {
		t.Fatalf("look up OpenAI responses offering: %v", err)
	}
	spec, ok := offering.endpointForAuthMode(AuthModeOAuth)
	if !ok {
		t.Fatal("OpenAI responses offering has no oauth endpoint spec")
	}

	rotator := OAuthRotator(FileTokenStore(path))
	if _, err := rotator.Rotate(context.Background(), spec.Rotation); err != nil {
		t.Fatalf("rotate OpenAI OAuth token: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAI OAuth file after rotation: %v", err)
	}
	var afterToken struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(after, &afterToken); err != nil {
		t.Fatalf("decode OpenAI OAuth file after rotation: %v", err)
	}
	if afterToken.AccessToken == beforeToken.AccessToken {
		t.Fatal("OpenAI access_token did not change after rotation")
	}
}
