//go:build live

package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

type liveMatrixCell struct {
	model    string
	offering Offering
	authMode AuthMode
}

func liveMatrixRepresentativeCells() []liveMatrixCell {
	type pair struct {
		offering OfferingID
		authMode AuthMode
	}
	representatives := make(map[pair]liveMatrixCell)
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			for _, endpoint := range offering.Endpoints {
				key := pair{offering.ID, endpoint.AuthMode}
				cell, found := representatives[key]
				if !found || entry.Model < cell.model {
					representatives[key] = liveMatrixCell{entry.Model, offering, endpoint.AuthMode}
				}
			}
		}
	}
	cells := make([]liveMatrixCell, 0, len(representatives))
	for _, cell := range representatives {
		cells = append(cells, cell)
	}
	slices.SortFunc(cells, func(a, b liveMatrixCell) int {
		if order := strings.Compare(string(a.offering.ID), string(b.offering.ID)); order != 0 {
			return order
		}
		return strings.Compare(string(a.authMode), string(b.authMode))
	})
	return cells
}

// R-LOM6-6DAN
// R-BFL0-X1CL
// R-CJRE-3H0I
// R-M212-DUGA
func TestLiveMatrix(t *testing.T) {
	for _, cell := range liveMatrixRepresentativeCells() {
		t.Run(fmt.Sprintf("%s/%s/1", cell.offering.ID, cell.authMode), func(t *testing.T) {
			runLiveMatrixCell(t, cell)
		})
	}
}

func liveMatrixRotator(t *testing.T, cell liveMatrixCell) Rotator {
	t.Helper()
	var variable string
	switch cell.authMode {
	case AuthModeAPIKey:
		variable = map[Host]string{
			HostAnthropic:  "ANTHROPIC_API_KEY",
			HostOpenAI:     "OPENAI_API_KEY",
			HostGemini:     "GEMINI_API_KEY",
			HostXAI:        "XAI_API_KEY",
			HostOpenRouter: "OPENROUTER_API_KEY",
		}[cell.offering.Host]
	case AuthModeOAuth:
		variable = map[Host]string{
			HostOpenAI: "AGENTKIT_OPENAI_OAUTH_FILE",
			HostXAI:    "AGENTKIT_XAI_OAUTH_FILE",
		}[cell.offering.Host]
	}
	if variable == "" {
		t.Fatalf("no credential variable for %s/%s", cell.offering.Host, cell.authMode)
	}
	credential := os.Getenv(variable)
	if credential == "" {
		t.Fatalf("%s is unset", variable)
	}
	if cell.authMode == AuthModeOAuth {
		return OAuthRotator(FileTokenStore(credential))
	}
	return APIKeyRotator(credential)
}

func runLiveMatrixCell(t *testing.T, cell liveMatrixCell) {
	t.Helper()
	auth, err := cell.offering.Authenticator(liveMatrixRotator(t, cell))
	if err != nil {
		t.Fatalf("authenticator: %v", err)
	}
	endpoint, err := NewEndpoint(auth)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	type secretWordInput struct {
		Topic string `json:"topic" jsonschema:"required"`
	}
	tool, err := NewTool("secret_word", "Find the secret word for a topic", func(context.Context, secretWordInput) (string, error) {
		return "mango", nil
	}, func(secretWordInput) Access { return BlocksNone() })
	if err != nil {
		t.Fatalf("secret_word tool: %v", err)
	}
	var log bytes.Buffer
	conversation, err := New(cell.offering.WireFormat, endpoint, cell.offering.WireModel, Config{
		Tools: []Tool{tool},
		Log:   NewLog(&log, time.Now, ""),
	})
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	stream := conversation.Send(context.Background(), Text{Text: `What is the secret word for the topic "fruit"? Use the secret_word tool to find out, then tell me.`})
	stage := 0
	var callID string
	for event := range stream.Events() {
		switch event := event.(type) {
		case ToolCall:
			if stage == 0 && event.Use.Name == "secret_word" {
				callID = event.Use.ID
				stage = 1
			}
		case ToolReturn:
			if stage == 1 && event.Result.ToolUseID == callID {
				stage = 2
			}
		case MessageDone:
			if stage == 2 {
				for _, block := range event.Message.Blocks {
					if text, ok := block.(Text); ok && strings.Contains(strings.ToLower(text.Text), "mango") {
						stage = 3
					}
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if stage != 3 {
		t.Fatal("turn lacked a secret_word ToolCall, its ToolReturn, and a subsequent MessageDone containing mango")
	}
	decoder := json.NewDecoder(&log)
	usageRecords := 0
	for {
		var record LogRecord
		err := decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode log: %v", err)
		}
		if record.Type != RecordUsage {
			continue
		}
		usageRecords++
		if record.Usage == nil || record.Usage.OutputTokens <= 0 || record.Usage.InputTokens+record.Usage.CachedTokens <= 0 {
			t.Errorf("usage record %d lacks positive output and prompt tokens", usageRecords)
		}
	}
	if usageRecords < 2 {
		t.Fatalf("usage records = %d, want at least two", usageRecords)
	}
}
