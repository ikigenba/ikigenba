package render_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/render"
)

type futureAgentTrace interface {
	Event(string, agentkit.Event)
	Error(string, error)
	Record(agentkit.LogRecord) string
}

var _ futureAgentTrace = (*render.Trace)(nil)

// R-JR6N-EU2E
func TestTraceExportsOpaqueRequiredSurface(t *testing.T) {
	assertFunctionSignatures(render.NewTrace, render.OneLine)
	var summary interface {
		Summary(agentkit.Usage, agentkit.Cost, string)
	} = render.NewTrace(io.Discard, io.Discard)
	_ = summary

	typ := reflect.TypeOf(render.Trace{})
	for i := range typ.NumField() {
		if typ.Field(i).IsExported() {
			t.Fatalf("Trace field %q is exported", typ.Field(i).Name)
		}
	}

	methods := reflect.TypeOf((*render.Trace)(nil))
	want := map[string]string{
		"Event":   "func(*render.Trace, string, agentkit.Event)",
		"Error":   "func(*render.Trace, string, error)",
		"Record":  "func(*render.Trace, agentkit.LogRecord) string",
		"Summary": "func(*render.Trace, agentkit.Usage, agentkit.Cost, string)",
	}
	if methods.NumMethod() != len(want) {
		t.Fatalf("exported method count = %d, want %d", methods.NumMethod(), len(want))
	}
	for name, signature := range want {
		method, ok := methods.MethodByName(name)
		if !ok || method.Type.String() != signature {
			t.Errorf("%s signature = %v, want %s", name, method.Type, signature)
		}
	}
}

func assertFunctionSignatures(
	_ func(io.Writer, io.Writer) *render.Trace,
	_ func(string) string,
) {
}

// R-JSEJ-SLT3
func TestOneLineCollapsesOnlyLineTerminators(t *testing.T) {
	tests := map[string]string{
		"windows\r\nline":          "windows line",
		"unix\nline":               "unix line",
		"classic\rmac":             "classic\rmac",
		"\n\nempty\r\n\r\nlines\n": "  empty  lines ",
		"unchanged":                "unchanged",
	}
	for input, want := range tests {
		if got := render.OneLine(input); got != want {
			t.Errorf("OneLine(%q) = %q, want %q", input, got, want)
		}
	}
}

// R-JTMG-6DJS
func TestMessageEventPreservesTextAndIgnoresOtherBlocks(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	trace := render.NewTrace(stdout, stderr)
	trace.Event("root/child", agentkit.MessageDone{Message: agentkit.Message{
		Role: agentkit.RoleAssistant,
		Blocks: []agentkit.Block{
			agentkit.Reasoning{Text: "hidden"},
			agentkit.Text{Text: "first\ncontinuation"},
			agentkit.ToolUse{ID: "ignored", Name: "ignored"},
			agentkit.Text{Text: "second"},
		},
	}})
	trace.Event("root", agentkit.MessageDone{Message: agentkit.Message{
		Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.Reasoning{Text: "only hidden"}},
	}})

	want := "root/child assistant › first\ncontinuation\nsecond\n\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

// R-JUUC-K5AH
func TestToolCallCompactsValidJSONAndCollapsesInvalidInput(t *testing.T) {
	stdout := new(bytes.Buffer)
	trace := render.NewTrace(stdout, io.Discard)
	trace.Event("a", agentkit.ToolCall{Use: agentkit.ToolUse{
		ID: "1", Name: "search", Input: json.RawMessage(` { "query" : "go", "n" : 2 } `),
	}})
	trace.Event("b", agentkit.ToolCall{Use: agentkit.ToolUse{
		ID: "2", Name: "broken", Input: json.RawMessage("{bad\njson}"),
	}})

	want := "a tool › search {\"query\":\"go\",\"n\":2}\n\n" +
		"b tool › broken {bad json}\n\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// R-JW28-XX16
func TestToolReturnUsesAddressScopedCallNameAndOutputIsSilent(t *testing.T) {
	stdout := new(bytes.Buffer)
	trace := render.NewTrace(stdout, io.Discard)
	trace.Event("parent", agentkit.ToolCall{Use: agentkit.ToolUse{ID: "same", Name: "delegate", Input: json.RawMessage(`{}`)}})
	trace.Event("child", agentkit.ToolCall{Use: agentkit.ToolUse{ID: "same", Name: "lookup", Input: json.RawMessage(`{}`)}})
	trace.Event("parent", agentkit.ToolReturn{Result: agentkit.ToolResult{ToolUseID: "same", Content: "ok\nnow"}})
	trace.Event("child", agentkit.ToolReturn{Result: agentkit.ToolResult{ToolUseID: "same", Content: "no\r\nmatch", IsError: true}})
	trace.Event("other", agentkit.ToolReturn{Result: agentkit.ToolResult{ToolUseID: "missing", Content: "raw"}})
	trace.Event("parent", agentkit.OutputDone{Value: json.RawMessage(`{"silent":true}`)})

	want := "parent tool › delegate {}\n\n" +
		"child tool › lookup {}\n\n" +
		"parent result › delegate ok now\n\n" +
		"child result › lookup error: no match\n\n" +
		"other result › missing raw\n\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// R-JXA5-BORV
func TestTerminalErrorWritesOnlyCollapsedStderr(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	render.NewTrace(stdout, stderr).Error("root/worker", errors.New("failed\r\ntry\nagain"))
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if want := "root/worker error › failed try again\n\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

// R-JYI1-PGIK
func TestRecordFormatsOnlySearchableTranscriptEntries(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	trace := render.NewTrace(stdout, stderr)
	tests := []struct {
		name string
		rec  agentkit.LogRecord
		want string
	}{
		{"user", messageRecord(agentkit.RoleUser), "user › first\nsecond"},
		{"assistant", messageRecord(agentkit.RoleAssistant), "assistant › first\nsecond"},
		{"tool role", messageRecord(agentkit.RoleTool), ""},
		{"system role", messageRecord(agentkit.RoleSystem), ""},
		{"tool use", agentkit.LogRecord{Type: agentkit.RecordToolUse, ToolUse: &agentkit.ToolUse{Name: "find", Input: json.RawMessage(` { "x" : 1 } `)}}, "tool › find {\"x\":1}"},
		{"tool result", agentkit.LogRecord{Type: agentkit.RecordToolResult, ToolResult: &agentkit.ToolResult{Content: "one\ntwo"}}, "result › one two"},
		{"tool error", agentkit.LogRecord{Type: agentkit.RecordToolResult, ToolResult: &agentkit.ToolResult{Content: "bad\r\nnews", IsError: true}}, "result › error: bad news"},
	}
	for _, typ := range []agentkit.RecordType{
		agentkit.RecordTurnStart, agentkit.RecordOutput, agentkit.RecordUsage,
		agentkit.RecordLimit, agentkit.RecordError, agentkit.RecordRetry,
		agentkit.RecordTurnEnd, agentkit.RecordSummary,
	} {
		tests = append(tests, struct {
			name string
			rec  agentkit.LogRecord
			want string
		}{string(typ), agentkit.LogRecord{Type: typ}, ""})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trace.Record(test.rec); got != test.want {
				t.Errorf("Record() = %q, want %q", got, test.want)
			}
		})
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("Record wrote stdout %q or stderr %q", stdout.String(), stderr.String())
	}
}

func messageRecord(role agentkit.Role) agentkit.LogRecord {
	return agentkit.LogRecord{Type: agentkit.RecordMessage, Message: &agentkit.Message{
		Role: role,
		Blocks: []agentkit.Block{
			agentkit.Text{Text: "first"},
			agentkit.Reasoning{Text: "hidden"},
			agentkit.Text{Text: "second"},
		},
	}}
}

// R-JZPY-3899
func TestSummaryIncludesEveryUsageBucketAndSixDecimalCost(t *testing.T) {
	stdout := new(bytes.Buffer)
	trace := render.NewTrace(stdout, io.Discard)
	trace.Summary(agentkit.Usage{
		InputTokens: 1, CachedTokens: 2, CacheWrite5mTokens: 3,
		CacheWrite1hTokens: 4, OutputTokens: 5, ReasoningTokens: 6,
	}, agentkit.Cost(31_200_000), "session-id")

	want := "summary\n" +
		"· tokens   in=1 cache(r=2 w=7) out=5 reasoning=6 total=21\n" +
		"· cost     $0.031200 pass\n" +
		"· session  session-id\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

type interleavingWriter struct {
	active  atomic.Int32
	overlap atomic.Bool
	data    bytes.Buffer
}

func (w *interleavingWriter) Write(p []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	defer w.active.Add(-1)
	for _, b := range p {
		_ = w.data.WriteByte(b)
		runtime.Gosched()
	}
	return len(p), nil
}

// R-K0XU-GZZY
func TestConcurrentEventsKeepCompleteOutputUnitsAtomic(t *testing.T) {
	const count = 100
	w := new(interleavingWriter)
	trace := render.NewTrace(w, io.Discard)
	var start sync.WaitGroup
	start.Add(1)
	var calls sync.WaitGroup
	for i := range count {
		calls.Add(1)
		go func() {
			defer calls.Done()
			start.Wait()
			trace.Event(fmt.Sprintf("agent-%03d", i), agentkit.MessageDone{Message: agentkit.Message{
				Role:   agentkit.RoleAssistant,
				Blocks: []agentkit.Block{agentkit.Text{Text: fmt.Sprintf("body-%03d", i)}},
			}})
		}()
	}
	start.Done()
	calls.Wait()
	if w.overlap.Load() {
		t.Fatal("writer observed overlapping Write calls")
	}

	got := strings.Split(strings.TrimSuffix(w.data.String(), "\n\n"), "\n\n")
	want := make([]string, count)
	for i := range count {
		want[i] = fmt.Sprintf("agent-%03d assistant › body-%03d", i, i)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("concurrent units differ:\n got %q\nwant %q", got, want)
	}
}
