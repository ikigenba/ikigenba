//go:build live

package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// liveMatrixCell is one row of the live matrix (D23): an offering id and
// auth mode, the host and wire that resolve it, and the fixed cheap model
// the cell runs on.
type liveMatrixCell struct {
	offering OfferingID
	authMode AuthMode
	host     Host
	wire     WireName
	model    string
}

// liveMatrixCells is the exact cell table D23 specifies.
var liveMatrixCells = []liveMatrixCell{
	{OfferingAnthropicMessages, AuthModeAPIKey, HostAnthropic, WireMessages, "claude-haiku-4-5"},              // anthropic-messages/api_key
	{OfferingAnthropicMessages, AuthModeAPIKey, HostAnthropic, WireMessages, "claude-opus-5"},                 // anthropic-messages/api_key
	{OfferingOpenAIResponses, AuthModeAPIKey, HostOpenAI, WireResponses, "gpt-5.4-nano"},                      // openai-responses/api_key
	{OfferingOpenAIResponses, AuthModeOAuth, HostOpenAI, WireResponses, "gpt-5.4-mini"},                       // openai-responses/oauth
	{OfferingOpenAIChat, AuthModeAPIKey, HostOpenAI, WireChat, "gpt-5.4-nano"},                                // openai-chat/api_key
	{OfferingGeminiGenerateContent, AuthModeAPIKey, HostGemini, WireGenerateContent, "gemini-3.1-flash-lite"}, // gemini-generate-content/api_key
	{OfferingXAIResponses, AuthModeAPIKey, HostXAI, WireResponses, "grok-4.3"},                                // xai-responses/api_key
	{OfferingXAIResponses, AuthModeOAuth, HostXAI, WireResponses, "grok-4.3"},                                 // xai-responses/oauth
	{OfferingXAIChat, AuthModeAPIKey, HostXAI, WireChat, "grok-4.3"},                                          // xai-chat/api_key
	{OfferingXAIChat, AuthModeOAuth, HostXAI, WireChat, "grok-4.3"},                                           // xai-chat/oauth
	{OfferingOpenRouterChat, AuthModeAPIKey, HostOpenRouter, WireChat, "gpt-5.4-nano"},                        // openrouter-chat/api_key
	{OfferingOpenRouterResponses, AuthModeAPIKey, HostOpenRouter, WireResponses, "gpt-5.4-nano"},              // openrouter-responses/api_key
}

func TestLiveMatrix(t *testing.T) {
	for _, cell := range liveMatrixCells {
		t.Run(string(cell.offering)+"/"+string(cell.authMode)+"/"+cell.model, func(t *testing.T) {
			credential := requireLiveMatrixCredential(t, cell)

			offering, err := Lookup(cell.model, cell.host, cell.wire)
			if err != nil {
				t.Fatalf("look up %s/%s on %s: %v", cell.offering, cell.authMode, cell.model, err)
			}
			var rotator Rotator
			switch cell.authMode {
			case AuthModeAPIKey:
				rotator = APIKeyRotator(credential)
			case AuthModeOAuth:
				rotator = OAuthRotator(FileTokenStore(credential))
			default:
				t.Fatalf("unsupported auth mode %q", cell.authMode)
			}
			auth, err := offering.Authenticator(rotator)
			if err != nil {
				t.Fatalf("build authenticator: %v", err)
			}
			endpoint, err := NewEndpoint(auth)
			if err != nil {
				t.Fatalf("build endpoint: %v", err)
			}

			assertLiveMatrixTextTurn(t, offering, endpoint, cell.model)
			assertLiveMatrixToolTurn(t, offering, endpoint, cell.model)
			assertLiveMatrixSystemSequence(t, offering, endpoint, cell)
		})
	}
}

func assertLiveMatrixTextTurn(t *testing.T, offering Offering, endpoint Endpoint, model string) {
	t.Helper()
	var log bytes.Buffer
	conversation, err := New(offering.WireFormat, endpoint, model, Config{Log: NewLog(&log, time.Now, "")})
	if err != nil {
		t.Fatalf("build text conversation: %v", err)
	}
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
		t.Fatalf("text stream: %v", err)
	}
	if !hasText {
		t.Fatal("text turn had no MessageDone with a non-empty Text block")
	}

	hasUsage := false
	for _, line := range bytes.Split(log.Bytes(), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record LogRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode text log record: %v", err)
		}
		if record.Type == RecordUsage && record.Usage != nil && record.Usage.InputTokens > 0 && record.Usage.OutputTokens > 0 {
			hasUsage = true
		}
	}
	if !hasUsage {
		t.Fatal("text turn had no usage record with positive input and output tokens")
	}
}

func assertLiveMatrixToolTurn(t *testing.T, offering Offering, endpoint Endpoint, model string) {
	t.Helper()
	var log bytes.Buffer
	echo := MustTool("echo", "echo the argument back", func(_ context.Context, in struct {
		Text string `json:"text"`
	}) (string, error) {
		return in.Text, nil
	})
	conversation, err := New(offering.WireFormat, endpoint, model, Config{Tools: []Tool{echo}, Log: NewLog(&log, time.Now, "")})
	if err != nil {
		t.Fatalf("build tool conversation: %v", err)
	}
	stream := conversation.Send(context.Background(), Text{Text: `Call the echo tool with {"text":"pong"}, then answer with the single word: done`})
	sequence := 0
	for event := range stream.Events() {
		switch event := event.(type) {
		case ToolCall:
			if sequence == 0 && event.Use.Name == "echo" {
				sequence = 1
			}
		case ToolReturn:
			if sequence == 1 {
				sequence = 2
			}
		case MessageDone:
			if sequence == 2 {
				sequence = 3
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("tool stream: %v", err)
	}
	if sequence != 3 {
		t.Fatalf("tool event sequence reached step %d, want ToolCall(echo), ToolReturn, MessageDone", sequence)
	}
}

func assertLiveMatrixSystemSequence(t *testing.T, offering Offering, endpoint Endpoint, cell liveMatrixCell) {
	t.Helper()
	const firstToken = "zqfirst7k9"
	const secondToken = "zqsecond4m2"

	var log bytes.Buffer
	config := Config{Log: NewLog(&log, time.Now, "")}
	if cell.model == "claude-opus-5" {
		config.Settings = Settings{Options: Options{"effort": "low"}}
	}
	conversation, err := New(offering.WireFormat, endpoint, cell.model, config)
	if err != nil {
		t.Fatalf("build system conversation: %v", err)
	}
	if err := conversation.AddSystem("Append " + firstToken + " to every reply."); err != nil {
		t.Fatalf("add leading system message: %v", err)
	}
	first := conversation.Send(context.Background(), Text{Text: "Say hello."})
	firstHasToken := false
	for event := range first.Events() {
		completed, ok := event.(MessageDone)
		if !ok {
			continue
		}
		for _, block := range completed.Message.Blocks {
			text, ok := block.(Text)
			firstHasToken = firstHasToken || ok && strings.Contains(text.Text, firstToken)
		}
	}
	if err := first.Err(); err != nil {
		t.Fatalf("first system stream: %v", err)
	}
	if !firstHasToken {
		t.Fatalf("first system turn had no MessageDone Text containing %q", firstToken)
	}

	if err := conversation.AddSystem("Append " + secondToken + " to every reply."); err != nil {
		t.Fatalf("add interleaved system message: %v", err)
	}
	second := conversation.Send(context.Background(), Text{Text: "Say goodbye."})
	secondHasToken := false
	var secondTexts []string
	for event := range second.Events() {
		completed, ok := event.(MessageDone)
		if !ok {
			continue
		}
		for _, block := range completed.Message.Blocks {
			text, ok := block.(Text)
			secondHasToken = secondHasToken || ok && strings.Contains(text.Text, secondToken)
			if ok {
				secondTexts = append(secondTexts, text.Text)
			}
		}
	}
	streamErr := second.Err()
	isHaikuRejection := cell.offering == OfferingAnthropicMessages &&
		cell.authMode == AuthModeAPIKey && cell.model == "claude-haiku-4-5"
	if isHaikuRejection {
		if streamErr == nil {
			t.Fatal("interleaved system stream succeeded, want vendor 400")
		}
		var vendorError *Error
		if !errors.As(streamErr, &vendorError) {
			t.Fatalf("interleaved system stream error = %T, want *Error", streamErr)
		}
		if vendorError.Status != 400 {
			t.Fatalf("interleaved system stream status = %d, want 400", vendorError.Status)
		}
		return
	}
	if streamErr != nil {
		t.Fatalf("second system stream: %v", streamErr)
	}
	if !secondHasToken {
		t.Fatalf("second system turn had no MessageDone Text containing %q; texts: %q", secondToken, secondTexts)
	}
}

// requireLiveMatrixCredential fails the subtest, never skips it, when the
// credential the cell's host and auth mode need is absent or unreadable.
func requireLiveMatrixCredential(t *testing.T, cell liveMatrixCell) string {
	t.Helper()
	if cell.authMode == AuthModeOAuth {
		variable := liveMatrixOAuthFileVariable(cell.host)
		path := os.Getenv(variable)
		if path == "" {
			t.Fatalf("%s is unset", variable)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s names an unreadable file: %v", variable, err)
		}
		return path
	}
	variable := liveMatrixAPIKeyVariable(cell.host)
	key := os.Getenv(variable)
	if key == "" {
		t.Fatalf("%s is unset", variable)
	}
	return key
}

// liveMatrixAPIKeyVariable is the vendor's conventional API key environment
// variable for host.
func liveMatrixAPIKeyVariable(host Host) string {
	switch host {
	case HostAnthropic:
		return "ANTHROPIC_API_KEY"
	case HostOpenAI:
		return "OPENAI_API_KEY"
	case HostGemini:
		return "GEMINI_API_KEY"
	case HostXAI:
		return "XAI_API_KEY"
	case HostOpenRouter:
		return "OPENROUTER_API_KEY"
	default:
		return ""
	}
}

// liveMatrixOAuthFileVariable is the environment variable naming the OAuth
// token file for host.
func liveMatrixOAuthFileVariable(host Host) string {
	switch host {
	case HostOpenAI:
		return "AGENTKIT_OPENAI_OAUTH_FILE"
	case HostXAI:
		return "AGENTKIT_XAI_OAUTH_FILE"
	default:
		return ""
	}
}
