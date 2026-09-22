//go:build live

package agentkit

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestLiveOAuthRefreshXAI(t *testing.T) {
	path := os.Getenv("AGENTKIT_XAI_OAUTH_FILE")
	if path == "" {
		t.Fatal("AGENTKIT_XAI_OAUTH_FILE is unset")
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read xAI OAuth file: %v", err)
	}
	var beforeToken struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(before, &beforeToken); err != nil {
		t.Fatalf("decode xAI OAuth file before rotation: %v", err)
	}

	_, _, spec := firstXAIOAuthResponsesOffering(t)

	rotator := OAuthRotator(FileTokenStore(path))
	if _, err := rotator.Rotate(context.Background(), spec.Rotation); err != nil {
		t.Fatalf("rotate xAI OAuth token: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read xAI OAuth file after rotation: %v", err)
	}
	var afterToken struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(after, &afterToken); err != nil {
		t.Fatalf("decode xAI OAuth file after rotation: %v", err)
	}
	if afterToken.AccessToken == beforeToken.AccessToken {
		t.Fatal("xAI access_token did not change after rotation")
	}
}

func firstXAIOAuthResponsesOffering(t *testing.T) (string, Offering, EndpointSpec) {
	t.Helper()
	var selectedModel string
	var selectedOffering Offering
	var selectedEndpoint EndpointSpec
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.Host != HostXAI || offering.ID != OfferingXAIResponses || offering.WireName != WireResponses {
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
		t.Fatal("catalog has no xAI responses offering with OAuth")
	}
	return selectedModel, selectedOffering, selectedEndpoint
}
