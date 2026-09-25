package grok

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

const chatBase = "home/dev/.grok/"
const chatSessions = chatBase + "sessions/"

func chatFile(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func newGrokDecoder(root bool, own string) *grokDecoder {
	return &grokDecoder{rootAgent: root, ownID: own, locating: "/" + strings.TrimSuffix(chatSessions, "/"), spawns: map[string]string{}, prompts: map[string]bool{}, children: map[string]*grokChild{}}
}

func decodeGrok(d *grokDecoder, record string) ([]chat.Entry, chat.Usage) {
	return d.Decode(fstest.MapFS{}, []byte(record))
}

// R-L0UP-RB3J
var _ func(fs.FS, string, string, string) (*chat.Transcript, []chat.Entry, error) = Chat

// R-PN5X-UWUN R-PPLQ-MGC1 R-PQTN-082Q R-LXS0-344A R-LU4A-XSW7
func TestChatRootTranscript(t *testing.T) {
	root := fstest.MapFS{
		chatBase + "active_sessions.json":     chatFile("[]"),
		chatSessions + "x/root/summary.json":  chatFile(`{"session_kind":"headless"}`),
		chatSessions + "x/root/updates.jsonl": chatFile(`{"timestamp":7,"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"hello"}}}}` + "\n"),
	}
	tr, entries, err := Chat(root, "/home/dev", "root", "root")
	if err != nil || tr == nil || tr.Path() != "/"+chatSessions+"x/root/updates.jsonl" || len(entries) != 1 || entries[0].Text != "hello" {
		t.Fatalf("root chat: tr=%v entries=%+v err=%v", tr, entries, err)
	}
	if tr.Recorded() != (chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}) {
		t.Fatalf("recorded: %+v", tr.Recorded())
	}
	delete(root, chatSessions+"x/root/updates.jsonl")
	tr, entries, err = Chat(root, "/home/dev", "root", "root")
	if err != nil || tr == nil || tr.Path() != "/"+chatSessions+"x/root/updates.jsonl" || len(entries) != 0 {
		t.Fatalf("missing transcript file: tr=%v entries=%+v err=%v", tr, entries, err)
	}
	indexOnly := fstest.MapFS{
		chatBase + "active_sessions.json":     chatFile(`[{"session_id":"root","pid":42,"opened_at":"1970-01-01T00:16:42Z"}]`),
		"proc/stat":                           chatFile("btime 1000\n"),
		"proc/42/stat":                        chatFile("42 (grok) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 150 0 0"),
		chatSessions + "x/other/summary.json": chatFile("{}"),
	}
	tr, entries, err = Chat(indexOnly, "/home/dev", "root", "root")
	if err != nil || tr == nil || tr.Path() != "" || len(entries) != 0 {
		t.Fatalf("index-only root: tr=%v entries=%+v err=%v", tr, entries, err)
	}
}

// R-PODU-8OLC R-LSWE-K15I
func TestChatSubagentLookup(t *testing.T) {
	root := fstest.MapFS{
		chatBase + "active_sessions.json":             chatFile("[]"),
		chatSessions + "x/root/summary.json":          chatFile("{}"),
		chatSessions + "x/root/subagents/a/meta.json": chatFile(`{"child_session_id":"child"}`),
		chatSessions + "y/child/updates.jsonl":        chatFile(`{"params":{"update":{"sessionUpdate":"user_message_chunk","content":{"text":"task"}}}}` + "\n"),
	}
	tr, entries, err := Chat(root, "/home/dev", "root", "a")
	if err != nil || tr == nil || tr.Path() != "/"+chatSessions+"y/child/updates.jsonl" || len(entries) != 1 || entries[0].Kind != chat.KindAgent {
		t.Fatalf("subagent chat: tr=%v entries=%+v err=%v", tr, entries, err)
	}
	for _, id := range []string{"", "other", "aa", "child"} {
		got, e, err := Chat(root, "/home/dev", "root", id)
		if got != nil || e != nil || !exactChatError(err, chat.ErrAgentNotFound) {
			t.Errorf("agent %q: %v %+v %v", id, got, e, err)
		}
	}
	root[chatSessions+"x/root/subagents/a/meta.json"] = chatFile(`{"child_session_id":"../bad"}`)
	tr, entries, err = Chat(root, "/home/dev", "root", "a")
	if err != nil || tr == nil || tr.Path() != "" || len(entries) != 0 {
		t.Fatalf("invalid child session: %v %+v %v", tr, entries, err)
	}
}

// R-LROI-69ET R-LQGL-SHO4 R-PO1U-SH9N
func TestChatErrors(t *testing.T) {
	for _, root := range []fstest.MapFS{
		{},
		{chatSessions + "x/other/summary.json": chatFile("{}")},
		{chatBase + "active_sessions.json": chatFile("[]"), chatSessions + "x/root/summary.json": chatFile(`{"session_kind":"subagent"}`)},
	} {
		tr, entries, err := Chat(root, "/home/dev", "root", "root")
		if tr != nil || entries != nil || !exactChatError(err, tree.ErrNotFound) {
			t.Fatalf("not found: %v %+v %v", tr, entries, err)
		}
	}
	root := fstest.MapFS{chatBase + "active_sessions.json": chatFile("[]"), chatSessions + "x/root/summary.json": chatFile("{}")}
	tr, entries, err := Chat(root, "/home/dev", "root", "missing")
	if tr != nil || entries != nil || !exactChatError(err, chat.ErrAgentNotFound) {
		t.Fatalf("agent not found: %v %+v %v", tr, entries, err)
	}
	blocked := &chatTraceFS{files: root, failOpen: chatSessions + "x/root/updates.jsonl"}
	tr, entries, err = Chat(blocked, "/home/dev", "root", "root")
	if tr != nil || entries != nil {
		t.Fatalf("read failure must be atomic: %v %+v", tr, entries)
	}
	var readErr *session.ReadError
	var pathErr *fs.PathError
	if !errors.As(err, &readErr) || readErr.Path != "/"+chatSessions+"x/root/updates.jsonl" || !errors.Is(readErr.Err, fs.ErrPermission) || errors.As(readErr.Err, &pathErr) || strings.HasPrefix(readErr.Path, "//") {
		t.Fatalf("transcript read error: %v", err)
	}
}

func exactChatError(got, want error) bool {
	return got != nil && reflect.TypeOf(got) == reflect.TypeOf(want) && reflect.ValueOf(got).Pointer() == reflect.ValueOf(want).Pointer()
}

// R-PT9F-RRK4 R-PUHC-5JAT R-PVP8-JB1I R-GV52-MVE5
func TestGrokMessagesAndCopiedRecords(t *testing.T) {
	d := newGrokDecoder(true, "s")
	entries, usage := decodeGrok(d, `{"timestamp":12,"params":{"_meta":{"eventId":"s-1"},"update":{"sessionUpdate":"user_message_chunk","content":{"text":"one"}}}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindUser || entries[0].Text != "one" || !entries[0].HasTime || entries[0].Time.Compare(time.Unix(12, 0)) != 0 || usage != (chat.Usage{}) {
		t.Fatalf("user: %+v %+v", entries, usage)
	}
	for _, tc := range []struct {
		kind string
		want chat.Kind
	}{{"agent_message_chunk", chat.KindAssistant}, {"agent_thought_chunk", chat.KindReasoning}} {
		entries, usage = decodeGrok(d, `{"timestamp":1.2,"params":{"update":{"sessionUpdate":"`+tc.kind+`","content":{"text":"two"}}}}`)
		if len(entries) != 1 || entries[0].Kind != tc.want || entries[0].HasTime || entries[0].Text != "two" || usage != (chat.Usage{}) {
			t.Fatalf("%s: %+v %+v", tc.kind, entries, usage)
		}
	}
	for _, record := range []string{
		`{"params":{"_meta":{"eventId":"other-1"},"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"copy"}}}}`,
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","_meta":{"hideFromScrollback":true},"content":{"text":"hidden"}}}}`,
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","content":{"text":2}}}}`,
		`{"params":{"update":{"sessionUpdate":"hook_execution"}}}`,
	} {
		entries, usage = decodeGrok(d, record)
		if len(entries) != 0 || usage != (chat.Usage{}) {
			t.Fatalf("ignored: %+v %+v", entries, usage)
		}
	}
	entries, _ = decodeGrok(newGrokDecoder(false, "child"), `{"params":{"update":{"sessionUpdate":"user_message_chunk","content":{"text":"task"}}}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindAgent {
		t.Fatalf("subagent prompt: %+v", entries)
	}
	// Copied prompt and spawn records cannot change whether a later finish is direct.
	decodeGrok(d, `{"params":{"_meta":{"eventId":"old-1","promptId":"copied-prompt"},"update":{"sessionUpdate":"subagent_spawned","subagent_id":"copied-agent","parent_prompt_id":"copied-prompt"}}}`)
	decodeGrok(d, `{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"copied-agent","parent_prompt_id":"copied-prompt"}}}`)
	entries, usage = decodeGrok(d, `{"params":{"update":{"sessionUpdate":"subagent_finished","subagent_id":"copied-agent","output":"ignored"}}}`)
	if len(entries) != 0 || usage != (chat.Usage{}) {
		t.Fatalf("copied history changed decoder state: %+v %+v", entries, usage)
	}
}

// R-PWX4-X2S7 R-PY51-AUIW
func TestGrokTools(t *testing.T) {
	d := newGrokDecoder(true, "s")
	entries, _ := decodeGrok(d, `{"params":{"update":{"sessionUpdate":"tool_call","title":"fallback","_meta":{"x.ai/tool":{"name":"Bash"}},"rawInput": { "cmd": "ls" }}}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindTool || entries[0].Tool != "Bash" || entries[0].Text != `{ "cmd": "ls" }` {
		t.Fatalf("tool: %+v", entries)
	}
	entries, _ = decodeGrok(d, `{"params":{"update":{"sessionUpdate":"tool_call","title":"fallback"}}}`)
	if len(entries) != 1 || entries[0].Tool != "fallback" || entries[0].Text != "" {
		t.Fatalf("fallback tool: %+v", entries)
	}
	for _, tc := range []struct {
		status, output string
		want           chat.Kind
	}{
		{"failed", `null`, chat.KindResultError},
		{"completed", `{"type":"Bash","exit_code":1e400}`, chat.KindResultError},
		{"completed", `{"type":"Bash","exit_code":0}`, chat.KindResultOK},
		{"completed", `{"type":"Other","exit_code":7}`, chat.KindResultOK},
	} {
		entries, _ = decodeGrok(d, `{"params":{"update":{"sessionUpdate":"tool_call_update","status":"`+tc.status+`","rawOutput":`+tc.output+`}}}`)
		if len(entries) != 1 || entries[0].Kind != tc.want || entries[0].Text != "" || entries[0].Tool != "" {
			t.Fatalf("result: %+v", entries)
		}
	}
	entries, _ = decodeGrok(d, `{"params":{"update":{"sessionUpdate":"tool_call_update","status":"running"}}}`)
	if len(entries) != 0 {
		t.Fatalf("nonterminal result: %+v", entries)
	}
}

// R-PZCX-OM9L R-GQ9H-3SFD
func TestGrokDirectFinishedAgent(t *testing.T) {
	d := newGrokDecoder(true, "s")
	spawn := `{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"a","parent_prompt_id":"p"}}}`
	finish := `{"params":{"update":{"sessionUpdate":"subagent_finished","subagent_id":"a","output":"done"}}}`
	decodeGrok(d, spawn)
	if entries, _ := decodeGrok(d, finish); len(entries) != 0 {
		t.Fatalf("premature direct child: %+v", entries)
	}
	decodeGrok(d, `{"params":{"_meta":{"promptId":"p"},"update":{"sessionUpdate":"plan"}}}`)
	entries, _ := decodeGrok(d, finish)
	if len(entries) != 1 || entries[0].Kind != chat.KindAgent || entries[0].Text != "done" {
		t.Fatalf("direct finish: %+v", entries)
	}
	decodeGrok(d, `{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"a","parent_prompt_id":"missing"}}}`)
	if entries, _ := decodeGrok(d, finish); len(entries) != 0 {
		t.Fatalf("last spawn wins: %+v", entries)
	}
}

// R-Q48J-7P8D
func TestGrokTurnUsage(t *testing.T) {
	d := newGrokDecoder(true, "s")
	_, got := decodeGrok(d, `{"params":{"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":100,"cachedReadTokens":20,"cacheCreationTokens":10,"outputTokens":30,"reasoningTokens":5,"modelCalls":2}}}}`)
	want := chat.Usage{In: 70, CacheWrite: 10, CacheRead: 20, Out: 30, Reasoning: 5, Calls: 2}
	if got != want {
		t.Fatalf("usage: %+v", got)
	}
	_, got = decodeGrok(d, `{"params":{"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":1.2,"outputTokens":"2","modelCalls":3}}}}`)
	if got != (chat.Usage{Calls: 3}) {
		t.Fatalf("invalid numeric fields: %+v", got)
	}
}

// R-DLG6-8HL8 R-DMO2-M9BX R-PP9R-690C R-GTCN-9NFH R-EODI-QJ1P
func TestGrokChildUsageIncremental(t *testing.T) {
	childPath := chatSessions + "x/child/updates.jsonl"
	root := fstest.MapFS{childPath: chatFile(
		`{"params":{"_meta":{"eventId":"child-1"},"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":30,"outputTokens":3,"modelCalls":1}}}}` + "\n" +
			`{"params":{"_meta":{"eventId":"other-1"},"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":900}}}}` + "\n")}
	d := newGrokDecoder(true, "root")
	d.Decode(root, []byte(`{"params":{"_meta":{"promptId":"p"}}}`))
	d.Decode(root, []byte(`{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"a","parent_prompt_id":"p"}}}`))
	finish := []byte(`{"params":{"update":{"sessionUpdate":"subagent_finished","subagent_id":"a","child_session_id":"child"}}}`)
	turn := []byte(`{"params":{"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":100,"outputTokens":10,"modelCalls":2}}}}`)
	d.Decode(root, finish)
	d.Decode(root, finish)
	_, got := d.Decode(root, turn)
	if got != (chat.Usage{In: 70, Out: 7, Calls: 1}) {
		t.Fatalf("first turn: %+v", got)
	}
	root[childPath] = chatFile(string(root[childPath].Data) + `{"params":{"_meta":{"eventId":"child-2"},"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":5,"outputTokens":1,"modelCalls":1}}}}` + "\n")
	d.Decode(root, finish)
	_, got = d.Decode(root, turn)
	if got != (chat.Usage{In: 95, Out: 9, Calls: 1}) {
		t.Fatalf("later turn: %+v", got)
	}
	_, got = d.Decode(root, turn)
	if got != (chat.Usage{In: 100, Out: 10, Calls: 2}) {
		t.Fatalf("turn without finish: %+v", got)
	}
	if d.children["child"] == nil || d.children["child"].current != (chat.Usage{In: 35, Out: 4, Calls: 2}) {
		t.Fatalf("child total: %+v", d.children["child"])
	}
}

// R-DLG6-8HL8 R-GTCN-9NFH
func TestGrokInvalidChildDoesNotAccessFS(t *testing.T) {
	d := newGrokDecoder(true, "root")
	d.prompts["p"] = true
	d.spawns["a"] = "p"
	for _, child := range []string{"", ".", "..", "a/b", "root"} {
		d.Decode(fstest.MapFS{}, []byte(`{"params":{"update":{"sessionUpdate":"subagent_finished","subagent_id":"a","child_session_id":"`+child+`"}}}`))
	}
	trace := &chatTraceFS{files: fstest.MapFS{}}
	_, usage := d.Decode(trace, []byte(`{"params":{"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":5}}}}`))
	if len(trace.names) != 0 || usage.In != 5 {
		t.Fatalf("invalid child accessed filesystem: %v, %+v", trace.names, usage)
	}
}

type chatTraceFS struct {
	files    fstest.MapFS
	names    []string
	failOpen string
}

func (f *chatTraceFS) Open(name string) (fs.File, error) {
	f.names = append(f.names, "open "+name)
	if name == f.failOpen {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.files.Open(name)
}

func (f *chatTraceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	f.names = append(f.names, "list "+name)
	return f.files.ReadDir(name)
}

func (f *chatTraceFS) Stat(name string) (fs.FileInfo, error) {
	f.names = append(f.names, "stat "+name)
	return fs.Stat(f.files, name)
}

func (f *chatTraceFS) ReadFile(name string) ([]byte, error) {
	f.names = append(f.names, "read "+name)
	return fs.ReadFile(f.files, name)
}

// R-DP3V-DSTB R-5HSP-OSAY R-GTCN-9NFH
func TestGrokChatAccessBoundary(t *testing.T) {
	root := &chatTraceFS{files: fstest.MapFS{
		chatBase + "active_sessions.json":     chatFile("[]"),
		chatSessions + "x/root/summary.json":  chatFile("{}"),
		chatSessions + "x/root/updates.jsonl": chatFile(`{"params":{"update":{"sessionUpdate":"turn_completed","usage":{"inputTokens":2}}}}` + "\n"),
		chatSessions + "x/root/secret.lock":   chatFile("secret"),
		chatSessions + "x/root/events.jsonl":  chatFile("secret\n"),
	}}
	tr, _, err := Chat(root, "/home/dev", "root", "root")
	if err != nil || tr == nil || tr.Usage().In != 2 {
		t.Fatalf("chat: %v %v", tr, err)
	}
	allowed := map[string]bool{
		"stat " + strings.TrimSuffix(chatSessions, "/"): true,
		"list " + strings.TrimSuffix(chatSessions, "/"): true,
		"list " + chatSessions + "x":                    true,
		"read " + chatBase + "active_sessions.json":     true,
		"read " + chatSessions + "x/root/summary.json":  true,
		"open " + chatSessions + "x/root/updates.jsonl": true,
	}
	for _, name := range root.names {
		if !allowed[name] {
			t.Errorf("unexpected access: %q", name)
		}
	}
	for _, name := range []string{"read " + chatBase + "active_sessions.json", "read " + chatSessions + "x/root/summary.json", "open " + chatSessions + "x/root/updates.jsonl"} {
		count := 0
		for _, actual := range root.names {
			if actual == name {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s was accessed %d times", name, count)
		}
	}
}
