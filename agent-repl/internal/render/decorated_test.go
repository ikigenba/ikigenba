package render_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/render"
	"github.com/ikigenba/ikigenba/agentkit"
)

func TestDecoratedLifecycle(t *testing.T) {
	// R-WEMM-PF7T, R-WJI8-8I6L
	var stdout, stderr bytes.Buffer
	d := render.NewDecorated(&stdout, &stderr)
	d.Prompt()
	d.Begin()
	d.End()
	if got, want := stdout.String(), "you › \n\n"; got != want {
		t.Fatalf("lifecycle output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestOneLine(t *testing.T) {
	// R-WFUJ-36YI
	if got, want := render.OneLine("a\r\nb\nc\rd"), "a b c\rd"; got != want {
		t.Fatalf("OneLine = %q, want %q", got, want)
	}
}

func TestDecoratedMessageDone(t *testing.T) {
	// R-WKQ4-M9XA
	var stdout bytes.Buffer
	d := render.NewDecorated(&stdout, &bytes.Buffer{})
	d.Event(agentkit.MessageDone{Message: agentkit.Message{Blocks: []agentkit.Block{
		agentkit.Text{Text: "first\nkept"},
		agentkit.ToolUse{ID: "ignored", Name: "tool"},
		agentkit.Text{Text: "second"},
	}}})
	if got, want := stdout.String(), "assistant › first\nkept\nsecond\n\n"; got != want {
		t.Fatalf("message output = %q, want %q", got, want)
	}
	stdout.Reset()
	d.Event(agentkit.MessageDone{Message: agentkit.Message{Blocks: []agentkit.Block{
		agentkit.ToolUse{ID: "only-tool", Name: "tool"},
	}}})
	if stdout.Len() != 0 {
		t.Fatalf("message without text output = %q, want empty", stdout.String())
	}
}

func TestDecoratedToolCall(t *testing.T) {
	// R-WLY1-01NZ
	var stdout bytes.Buffer
	d := render.NewDecorated(&stdout, &bytes.Buffer{})
	d.Event(agentkit.ToolCall{Use: agentkit.ToolUse{ID: "valid", Name: "Read", Input: []byte("{ \"path\" : \"a\" }")}})
	d.Event(agentkit.ToolCall{Use: agentkit.ToolUse{ID: "invalid", Name: "Broken", Input: []byte("bad\r\njson\n")}})
	want := "tool › Read {\"path\":\"a\"}\n\ntool › Broken bad json \n\n"
	if got := stdout.String(); got != want {
		t.Fatalf("tool output = %q, want %q", got, want)
	}
}

func TestDecoratedToolReturn(t *testing.T) {
	// R-WN5X-DTEO
	var stdout bytes.Buffer
	d := render.NewDecorated(&stdout, &bytes.Buffer{})
	d.Event(agentkit.ToolCall{Use: agentkit.ToolUse{ID: "call-1", Name: "Read", Input: []byte("{}")}})
	stdout.Reset()
	d.Event(agentkit.ToolReturn{Result: agentkit.ToolResult{ToolUseID: "call-1", Content: "one\r\ntwo"}})
	d.Event(agentkit.ToolReturn{Result: agentkit.ToolResult{ToolUseID: "missing", Content: "bad\nnews", IsError: true}})
	want := "result › Read one two\n\nresult › missing error: bad news\n\n"
	if got := stdout.String(); got != want {
		t.Fatalf("result output = %q, want %q", got, want)
	}
}

func TestDecoratedIgnoresOutputDone(t *testing.T) {
	// R-WODT-RL5D
	var stdout bytes.Buffer
	d := render.NewDecorated(&stdout, &bytes.Buffer{})
	d.Event(agentkit.OutputDone{Value: []byte(`{"done":true}`)})
	if stdout.Len() != 0 {
		t.Fatalf("output event wrote %q, want empty", stdout.String())
	}
}

func TestDecoratedError(t *testing.T) {
	// R-CNZG-JOTG
	var stdout, stderr bytes.Buffer
	d := render.NewDecorated(&stdout, &stderr)
	d.Error(errors.New("provider\r\nfailed\nagain"))
	if got, want := stderr.String(), "error › provider failed again\n\n"; got != want {
		t.Fatalf("error output = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestDecoratedSummary(t *testing.T) {
	// R-WS1I-WWDG
	var stdout bytes.Buffer
	d := render.NewDecorated(&stdout, &bytes.Buffer{})
	d.Summary(agentkit.Usage{
		InputTokens:        10,
		CachedTokens:       20,
		CacheWrite5mTokens: 30,
		CacheWrite1hTokens: 40,
		OutputTokens:       50,
		ReasoningTokens:    60,
	}, agentkit.Cost(4_115_000))
	want := "summary\n· tokens  in=10 cache(r=20 w=70) out=50 reasoning=60 total=210\n· cost     $0.004115 session\n"
	if got := stdout.String(); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
