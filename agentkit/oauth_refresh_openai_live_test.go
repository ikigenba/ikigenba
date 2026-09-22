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

	_, _, spec := firstOpenAIOAuthResponsesOffering(t)

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

func firstOpenAIOAuthResponsesOffering(t *testing.T) (string, Offering, EndpointSpec) {
	t.Helper()
	var selectedModel string
	var selectedOffering Offering
	var selectedEndpoint EndpointSpec
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.Host != HostOpenAI || offering.ID != OfferingOpenAIResponses || offering.WireName != WireResponses {
				continue
			}
			for _, endpoint := range offering.Endpoints {
				if endpoint.AuthMode == AuthModeOAuth && (selectedModel == "" || entry.Model < selectedModel) {
					selectedModel, selectedOffering, selectedEndpoint = entry.Model, offering, endpoint
				}
			}
		}
	}
	if selectedModel == "" {
		t.Fatal("catalog has no OpenAI responses offering with OAuth")
	}
	return selectedModel, selectedOffering, selectedEndpoint
}
