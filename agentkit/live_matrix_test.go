//go:build live

package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// liveMatrixCell is one representative row of the live matrix (D23).
type liveMatrixCell struct {
	offering OfferingID
	authMode AuthMode
	host     Host
	wire     WireName
	model    string
}

var liveMatrixCells = liveMatrixRepresentativeCells()

func liveMatrixRepresentativeCells() []liveMatrixCell {
	type pair struct {
		offering OfferingID
		authMode AuthMode
	}
	representatives := make(map[pair]liveMatrixCell)
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			for _, endpoint := range offering.Endpoints {
				key := pair{offering: offering.ID, authMode: endpoint.AuthMode}
				cell, found := representatives[key]
				if !found || entry.Model < cell.model {
					representatives[key] = liveMatrixCell{
						offering: offering.ID,
						authMode: endpoint.AuthMode,
						host:     offering.Host,
						wire:     offering.WireName,
						model:    entry.Model,
					}
				}
			}
		}
	}
	cells := make([]liveMatrixCell, 0, len(representatives))
	for _, cell := range representatives {
		cells = append(cells, cell)
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].offering != cells[j].offering {
			return cells[i].offering < cells[j].offering
		}
		return cells[i].authMode < cells[j].authMode
	})
	return cells
}

func TestLiveMatrix(t *testing.T) {
	for _, cell := range liveMatrixCells {
		t.Run(fmt.Sprintf("%s/%s/1", cell.offering, cell.authMode), func(t *testing.T) {
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
	assertLiveMatrixStreamOK(t, "text", stream)
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
	}, func(struct {
		Text string `json:"text"`
	}) Access {
		return BlocksAll()
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
	assertLiveMatrixStreamOK(t, "tool", stream)
	if sequence != 3 {
		t.Fatalf("tool event sequence reached step %d, want ToolCall(echo), ToolReturn, MessageDone", sequence)
	}
}

func assertLiveMatrixSystemSequence(t *testing.T, offering Offering, endpoint Endpoint, cell liveMatrixCell) {
	t.Helper()
	const firstToken = "zqfirst7k9"
	const secondToken = "zqsecond4m2"

	var log bytes.Buffer
	conversation, err := New(offering.WireFormat, endpoint, cell.model, Config{Log: NewLog(&log, time.Now, "")})
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
	assertLiveMatrixStreamOK(t, "first system", first)
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
	assertLiveMatrixStreamOK(t, "second system", second)
	if !secondHasToken {
		t.Fatalf("second system turn had no MessageDone Text containing %q; texts: %q", secondToken, secondTexts)
	}
}

func assertLiveMatrixStreamOK(t *testing.T, label string, stream *Stream) {
	t.Helper()
	err := stream.Err()
	if err == nil {
		return
	}
	var providerError *Error
	if errors.As(err, &providerError) && (providerError.Category == CategoryTransport || providerError.Category == CategoryTimeout) {
		t.Fatalf("%s stream was truncated by transport: %v", label, err)
	}
	t.Fatalf("%s stream returned a response error: %v", label, err)
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
