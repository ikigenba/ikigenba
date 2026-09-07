//go:build live

package agentkit

import (
	"context"
	"testing"
)

func TestLiveSerialToolCalls(t *testing.T) {
	for _, cell := range liveMatrixCells {
		if cell.offering == OfferingGeminiGenerateContent {
			continue
		}
		t.Run(string(cell.offering)+"/"+string(cell.authMode)+"/"+cell.model, func(t *testing.T) {
			runLiveSerialToolCallsCell(t, cell)
		})
	}
}

type liveSerialToolInput struct {
	Text string `json:"text" jsonschema:"required"`
}

func runLiveSerialToolCallsCell(t *testing.T, cell liveMatrixCell) {
	t.Helper()
	offering, endpoint := liveSerialToolEndpoint(t, cell)
	tool := func(name string) Tool {
		return MustTool(name, "return the supplied text", func(_ context.Context, input liveSerialToolInput) (string, error) {
			return input.Text, nil
		}, func(_ liveSerialToolInput) Access { return BlocksNone() })
	}
	conversation, err := New(offering.WireFormat, endpoint, cell.model, Config{
		Settings: Settings{SerialToolCalls: true},
		Tools:    []Tool{tool("first_tool"), tool("second_tool")},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := conversation.Send(context.Background(), Text{Text: "Call first_tool with text first and second_tool with text second, then answer done."})
	assertLiveSerialMessageCounts(t, stream)
}

func liveSerialToolEndpoint(t *testing.T, cell liveMatrixCell) (Offering, Endpoint) {
	t.Helper()
	credential := requireLiveMatrixCredential(t, cell)
	offering, err := Lookup(cell.model, cell.host, cell.wire)
	if err != nil {
		t.Fatal(err)
	}
	var rotator Rotator = APIKeyRotator(credential)
	if cell.authMode == AuthModeOAuth {
		rotator = OAuthRotator(FileTokenStore(credential))
	}
	auth, err := offering.Authenticator(rotator)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := NewEndpoint(auth)
	if err != nil {
		t.Fatal(err)
	}
	return offering, endpoint
}

func assertLiveSerialMessageCounts(t *testing.T, stream *Stream) {
	t.Helper()
	toolTurnRan := false
	for event := range stream.Events() {
		message, ok := event.(MessageDone)
		if !ok || message.Message.Role != RoleAssistant {
			continue
		}
		uses := 0
		for _, block := range message.Message.Blocks {
			if _, ok := block.(ToolUse); ok {
				uses++
				toolTurnRan = true
			}
		}
		if uses > 1 {
			t.Fatalf("assistant MessageDone has %d ToolUse blocks", uses)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("serial tool call stream: %v", err)
	}
	if !toolTurnRan {
		t.Fatal("serial tool call stream completed without a tool turn")
	}
}
