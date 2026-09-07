package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type orderedLogTool struct {
	name string
	run  func(context.Context) string
}

func (t orderedLogTool) Name() string                  { return t.name }
func (t orderedLogTool) Description() string           { return "event log ordering fixture" }
func (t orderedLogTool) Schema() json.RawMessage       { return json.RawMessage(`{"type":"object"}`) }
func (t orderedLogTool) Access(json.RawMessage) Access { return BlocksNone() }
func (t orderedLogTool) isTool()                       {}
func (t orderedLogTool) Call(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.run(ctx), nil
}

// R-DN5C-9BTK
func TestLogWritesOrderedToolMessageAfterCompletionOrderedResults(t *testing.T) {
	conversation, output := newOrderedLogConversation()
	records := runOrderedLogConversation(t, conversation, output)
	assertOrderedToolMessageRecords(t, records)
}

func newOrderedLogConversation() (*Conversation, *bytes.Buffer) {
	calls := []ToolUse{
		{ID: "slow", Name: "slow", Input: json.RawMessage(`{}`)},
		{ID: "fast", Name: "fast", Input: json.RawMessage(`{}`)},
	}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{calls[0], calls[1]}}}},
		{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}}},
	}}
	fastDone := make(chan struct{})
	tools := []Tool{
		orderedLogTool{name: "slow", run: func(context.Context) string {
			select {
			case <-fastDone:
				return "slow"
			case <-time.After(time.Second):
				return "timed out waiting for fast tool"
			}
		}},
		orderedLogTool{name: "fast", run: func(context.Context) string {
			close(fastDone)
			return "fast"
		}},
	}
	transportCalls := 0
	output := &bytes.Buffer{}
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Tools: tools,
		Log:   NewLog(output, func() time.Time { return time.Time{} }, ""),
	})
	return conversation, output
}

func runOrderedLogConversation(t *testing.T, conversation *Conversation, output *bytes.Buffer) []LogRecord {
	t.Helper()
	stream := conversation.Send(context.Background(), Text{Text: "run both"})
	drainStream(stream)
	if stream.Err() != nil {
		t.Fatalf("Send error = %v", stream.Err())
	}
	return decodeLogRecords(t, output.Bytes())
}

func assertOrderedToolMessageRecords(t *testing.T, records []LogRecord) {
	t.Helper()
	var resultIndexes []int
	var resultIDs []string
	var toolMessages []int
	for index, record := range records {
		switch {
		case record.Type == RecordToolResult:
			resultIndexes = append(resultIndexes, index)
			resultIDs = append(resultIDs, record.ToolResult.ToolUseID)
		case record.Type == RecordMessage && record.Message != nil && record.Message.Role == RoleTool:
			toolMessages = append(toolMessages, index)
		}
	}
	if !reflect.DeepEqual(resultIDs, []string{"fast", "slow"}) {
		t.Fatalf("tool_result records = %v, want completion order [fast slow]", resultIDs)
	}
	if len(toolMessages) != 1 || len(resultIndexes) != 2 || toolMessages[0] != resultIndexes[1]+1 {
		t.Fatalf("tool-result indexes = %v, RoleTool message indexes = %v; want one message immediately after all results", resultIndexes, toolMessages)
	}
	message := *records[toolMessages[0]].Message
	want := Message{Role: RoleTool, Blocks: []Block{
		ToolResult{ToolUseID: "slow", Content: "slow"},
		ToolResult{ToolUseID: "fast", Content: "fast"},
	}}
	if !reflect.DeepEqual(message, want) {
		t.Fatalf("logged RoleTool message = %#v, want request order %#v", message, want)
	}
}
