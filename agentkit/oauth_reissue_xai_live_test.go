//go:build live

package agentkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type xAIReissueTokenStore struct {
	path string
}

func (s xAIReissueTokenStore) Read(_ context.Context) ([]byte, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	var accessToken string
	if err := json.Unmarshal(stored["access_token"], &accessToken); err != nil {
		return nil, err
	}
	segments := strings.Split(accessToken, ".")
	if len(segments) != 3 || len(segments[2]) < 4 {
		return nil, fmt.Errorf("access_token is not a three-segment JWT with a four-character signature suffix")
	}
	suffix := "AAAA"
	if strings.HasSuffix(segments[2], suffix) {
		suffix = "BBBB"
	}
	segments[2] = segments[2][:len(segments[2])-4] + suffix
	stored["access_token"], err = json.Marshal(strings.Join(segments, "."))
	if err != nil {
		return nil, err
	}
	return json.Marshal(stored)
}

func (s xAIReissueTokenStore) Write(_ context.Context, data []byte) error {
	return os.WriteFile(s.path, data, 0o600)
}

func readXAIAccessToken(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read xAI OAuth file: %v", err)
	}
	var stored struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("decode xAI OAuth file: %v", err)
	}
	return stored.AccessToken
}

func buildXAIReissueConversation(t *testing.T, path string) *Conversation {
	t.Helper()
	model, offering := firstXAIReissueOffering(t)
	rotator := OAuthRotator(xAIReissueTokenStore{path: path})
	auth, err := offering.Authenticator(rotator)
	if err != nil {
		t.Fatalf("build xAI authenticator: %v", err)
	}
	endpoint, err := NewEndpoint(auth)
	if err != nil {
		t.Fatalf("build xAI endpoint: %v", err)
	}
	conversation, err := New(offering.WireFormat, endpoint, model, Config{})
	if err != nil {
		t.Fatalf("build xAI conversation: %v", err)
	}
	return conversation
}

func firstXAIReissueOffering(t *testing.T) (string, Offering) {
	t.Helper()
	var selectedModel string
	var selectedOffering Offering
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.Host != HostXAI || offering.ID != OfferingXAIResponses || offering.WireName != WireResponses {
				continue
			}
			for _, endpoint := range offering.Endpoints {
				if endpoint.AuthMode == AuthModeOAuth && (selectedModel == "" || entry.Model < selectedModel) {
					selectedModel, selectedOffering = entry.Model, offering
				}
			}
		}
	}
	if selectedModel == "" {
		t.Fatal("catalog has no xAI responses offering with OAuth")
	}
	return selectedModel, selectedOffering
}

func assertXAIReissueTextTurn(t *testing.T, conversation *Conversation) {
	t.Helper()
	stream := conversation.Send(context.Background(), Text{Text: "Reply with the single word: pong"})
	hasText := false
	for event := range stream.Events() {
		completed, ok := event.(MessageDone)
		if !ok {
			continue
		}
		for _, block := range completed.Message.Blocks {
			text, ok := block.(Text)
			hasText = hasText || ok && text.Text != ""
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("xAI text stream: %v", err)
	}
	if !hasText {
		t.Fatal("xAI text turn had no MessageDone with a non-empty Text block")
	}
}

func TestLiveOAuthReissueXAI(t *testing.T) {
	path := os.Getenv("AGENTKIT_XAI_OAUTH_FILE")
	if path == "" {
		t.Fatal("AGENTKIT_XAI_OAUTH_FILE is unset")
	}

	beforeToken := readXAIAccessToken(t, path)
	conversation := buildXAIReissueConversation(t, path)
	assertXAIReissueTextTurn(t, conversation)
	if readXAIAccessToken(t, path) == beforeToken {
		t.Fatal("xAI access_token did not change after rejected-credential reissue")
	}
}
