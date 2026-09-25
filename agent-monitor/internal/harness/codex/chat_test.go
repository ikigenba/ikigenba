package codex

import (
	"errors"
	"io"
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

func decode(t *testing.T, d *chatDecoder, record string) ([]chat.Entry, chat.Usage) {
	t.Helper()
	return d.Decode(fstest.MapFS{}, []byte(record))
}

func chatFixture() *observedFS {
	f := fixture()
	f.MapFS[sessions+"2026/09/24/."] = &fstest.MapFile{Mode: fs.ModeDir}
	return f
}

func chatRollout(f *observedFS, id, content string) string {
	const file = "rollout-2026-09-24T12-00-00-"
	name := sessions + "2026/09/24/" + file + id + ".jsonl"
	f.MapFS[name] = &fstest.MapFile{Data: []byte(content)}
	return name
}

type trackedChatFS struct {
	*observedFS
	fileCalls []string
}

func (f *trackedChatFS) Open(name string) (fs.File, error) {
	file, err := f.observedFS.Open(name)
	if err != nil {
		return nil, err
	}
	return &trackedChatFile{File: file, owner: f, name: name}, nil
}

func (f *trackedChatFS) WriteFile(name string, _ []byte, _ fs.FileMode) error {
	f.calls = append(f.calls, "writefile:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) Create(name string) (fs.File, error) {
	f.calls = append(f.calls, "create:"+name)
	return nil, fs.ErrPermission
}

func (f *trackedChatFS) OpenFile(name string, _ int, _ fs.FileMode) (fs.File, error) {
	f.calls = append(f.calls, "openfile:"+name)
	return nil, fs.ErrPermission
}

func (f *trackedChatFS) Mkdir(name string, _ fs.FileMode) error {
	f.calls = append(f.calls, "mkdir:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) MkdirAll(name string, _ fs.FileMode) error {
	f.calls = append(f.calls, "mkdirall:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) Remove(name string) error {
	f.calls = append(f.calls, "remove:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) RemoveAll(name string) error {
	f.calls = append(f.calls, "removeall:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) Rename(oldName, newName string) error {
	f.calls = append(f.calls, "rename:"+oldName+":"+newName)
	return fs.ErrPermission
}

func (f *trackedChatFS) Chmod(name string, _ fs.FileMode) error {
	f.calls = append(f.calls, "chmod:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) Chtimes(name string, _, _ time.Time) error {
	f.calls = append(f.calls, "chtimes:"+name)
	return fs.ErrPermission
}

func (f *trackedChatFS) Symlink(oldName, newName string) error {
	f.calls = append(f.calls, "symlink:"+oldName+":"+newName)
	return fs.ErrPermission
}

type trackedChatFile struct {
	fs.File
	owner *trackedChatFS
	name  string
}

func (f *trackedChatFile) Read(b []byte) (int, error) {
	f.owner.fileCalls = append(f.owner.fileCalls, "read:"+f.name)
	return f.File.Read(b)
}

func (f *trackedChatFile) ReadAt(b []byte, offset int64) (int, error) {
	f.owner.fileCalls = append(f.owner.fileCalls, "readat:"+f.name)
	return f.File.(io.ReaderAt).ReadAt(b, offset)
}

func (f *trackedChatFile) Write([]byte) (int, error) {
	f.owner.fileCalls = append(f.owner.fileCalls, "write:"+f.name)
	return 0, fs.ErrPermission
}

func countCall(calls []string, wanted string) int {
	n := 0
	for _, call := range calls {
		if call == wanted {
			n++
		}
	}
	return n
}

func TestChatSignature(_ *testing.T) {
	// R-L0UP-RB3J
	assertShape := func(func(fs.FS, string, string, string) (*chat.Transcript, []chat.Entry, error)) {}
	assertShape(Chat)
}

func TestChatNamedPath(t *testing.T) {
	// R-EA5P-8WAS
	f := chatFixture()
	file := chatRollout(f, "root", `{"type":"session_meta"}`+"\n")
	tr, _, err := Chat(f, home, "root", "root")
	if err != nil || tr.Path() != "/"+file {
		t.Fatalf("path=%q err=%v", tr.Path(), err)
	}
	chatRollout(f, "root", `{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"child"}}}`+"\n")
	child, _, err := Chat(f, home, "root", "child")
	if err != nil || child.Path() != "" {
		t.Fatalf("missing child path=%q err=%v", child.Path(), err)
	}
}

func TestChatRecorded(t *testing.T) {
	// R-EBDL-MO1H R-LXS0-344A
	f := chatFixture()
	chatRollout(f, "root", "{}\n")
	tr, _, err := Chat(f, home, "root", "root")
	if err != nil || tr.Recorded() != (chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}) {
		t.Fatalf("recorded=%+v err=%v", tr.Recorded(), err)
	}
}

func TestChatRootOnePassAndSubagent(t *testing.T) {
	// R-ECLI-0FS6
	f := chatFixture()
	file := chatRollout(f, "root", `{"type":"session_meta","payload":{"parent_thread_id":"parent"}}`+"\n")
	tr, entries, err := Chat(f, home, "root", "root")
	if tr != nil || entries != nil || !reflect.DeepEqual(err, tree.ErrNotFound) {
		t.Fatalf("subagent: %v %v %v", tr, entries, err)
	}
	opened := 0
	for _, call := range f.calls {
		if call == "open:"+file {
			opened++
		}
	}
	if opened != 1 {
		t.Fatalf("rollout opened %d times: %v", opened, f.calls)
	}
}

func TestChatUnmatchedAgentNeverOpened(t *testing.T) {
	// R-EDTE-E7IV
	f := chatFixture()
	chatRollout(f, "root", `{"type":"session_meta"}`+"\n")
	target := chatRollout(f, "else", `{"type":"response_item"}`+"\n")
	_, _, err := Chat(f, home, "root", "else")
	if !reflect.DeepEqual(err, chat.ErrAgentNotFound) || has(f.calls, "open:"+target) {
		t.Fatalf("err=%v calls=%v", err, f.calls)
	}
}

func TestChatUnreadableOtherAgent(t *testing.T) {
	// R-EF1A-RZ9K
	f := chatFixture()
	chatRollout(f, "root", `{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"middle"}}}`+"\n")
	middle := chatRollout(f, "middle", `{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"leaf"}}}`+"\n")
	f.fail["open:"+middle] = fs.ErrPermission
	_, _, err := Chat(f, home, "root", "leaf")
	if !reflect.DeepEqual(err, chat.ErrAgentNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestChatAllowedPaths(t *testing.T) {
	// R-EG97-5R09
	f := &trackedChatFS{observedFS: chatFixture()}
	file := chatRollout(f.observedFS, "root", "{}\n")
	f.lock("root", 17, 321)
	f.MapFS[index] = &fstest.MapFile{Data: []byte("{}\n")}
	f.MapFS[base+"unrelated"] = &fstest.MapFile{Data: []byte("secret")}
	_, _, err := Chat(f, home, "root", "root")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"stat:" + strings.TrimSuffix(sessions, "/"): true,
		"dir:" + strings.TrimSuffix(sessions, "/"):  true,
		"dir:" + sessions + "2026":                  true,
		"dir:" + sessions + "2026/09":               true,
		"dir:" + sessions + "2026/09/24":            true,
		"stat:" + lockDir + "/root.lock":            true,
		"read:proc/locks":                           true,
		"open:" + file:                              true,
	}
	for _, call := range f.calls {
		if !allowed[call] {
			t.Fatalf("outside allowed operations: %q in %v", call, f.calls)
		}
	}
	if countCall(f.calls, "stat:"+lockDir+"/root.lock") != 1 || countCall(f.calls, "read:proc/locks") != 1 {
		t.Fatalf("lock liveness calls: %v", f.calls)
	}
	for _, call := range f.fileCalls {
		if call != "readat:"+file {
			t.Fatalf("outside allowed file methods: %q in %v", call, f.fileCalls)
		}
	}
}

func TestDecoderFreshAndFilesystemFree(t *testing.T) {
	// R-EIOZ-XAHN
	f := chatFixture()
	chatRollout(f, "root", `{"type":"session_meta"}`+"\n"+
		`{"type":"session_meta"}`+"\n"+
		`{"type":"token_usage_record","payload":{"thread_id":"root","usage":{"input_tokens":100}}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_id":"root"}}`+"\n"+
		`{"type":"token_usage_record","payload":{"thread_id":"root","usage":{"input_tokens":3}}}`+"\n")
	for range 2 {
		tr, _, err := Chat(f, home, "root", "root")
		if err != nil || tr == nil || tr.Usage().In != 3 || tr.Usage().Calls != 1 {
			t.Fatalf("fresh root decoder: transcript=%v err=%v", tr, err)
		}
	}
	chatRollout(f, "other", `{"type":"token_usage_record","payload":{"thread_id":"other","usage":{"input_tokens":7}}}`+"\n")
	tr, _, err := Chat(f, home, "other", "other")
	if err != nil || tr == nil || tr.Usage().In != 7 {
		t.Fatalf("own thread id: transcript=%v err=%v", tr, err)
	}
	d := &chatDecoder{ownID: "root"}
	probe := fixture()
	d.Decode(probe, []byte(`{"type":"response_item","payload":{"type":"function_call"}}`))
	if len(probe.calls) != 0 || d.count != 1 {
		t.Fatalf("calls=%v count=%d", probe.calls, d.count)
	}
}

func TestDecoderCopiedHistory(t *testing.T) {
	// R-EJWW-B28C
	d := &chatDecoder{ownID: "child"}
	decode(t, d, `{"type":"session_meta"}`)
	decode(t, d, `{"type":"session_meta"}`)
	entries, usage := decode(t, d, `{"type":"token_usage_record","payload":{"thread_id":"child","usage":{"input_tokens":5}}}`)
	if len(entries) != 0 || usage != (chat.Usage{}) {
		t.Fatalf("copied: %v %+v", entries, usage)
	}
	decode(t, d, `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_id":"child"}}`)
	_, usage = decode(t, d, `{"type":"token_usage_record","payload":{"thread_id":"child","usage":{"input_tokens":5}}}`)
	if usage.In != 5 {
		t.Fatalf("own usage=%+v", usage)
	}
}

func TestDecoderTopLevelTypes(t *testing.T) {
	// R-EL4S-OTZ1
	d := &chatDecoder{ownID: "root"}
	for _, typ := range []string{"event_msg", "session_meta", "turn_context", "world_state", "compacted", "inter_agent_communication_metadata"} {
		entries, usage := decode(t, d, `{"type":"`+typ+`","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"shown"}]}}`)
		if len(entries) != 0 || usage != (chat.Usage{}) {
			t.Fatalf("type %s: %v %+v", typ, entries, usage)
		}
	}
}

func TestDecoderTimestamp(t *testing.T) {
	// R-EMCP-2LPQ
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","timestamp":"2026-09-24T12:30:20.456Z","payload":{"type":"function_call"}}`)
	want := time.Date(2026, 9, 24, 12, 30, 20, 456000000, time.UTC)
	if len(entries) != 1 || !entries[0].HasTime || entries[0].Time.Compare(want) != 0 {
		t.Fatalf("timestamp=%v", entries)
	}
}

func TestDecoderUserContentKinds(t *testing.T) {
	// R-ENKL-GDGF
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hidden"},{"type":"input_text","text":"hello"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["developer.injected","user.text"]}}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindUser || entries[0].Text != "hello" {
		t.Fatalf("user entries=%v", entries)
	}
}

func TestDecoderAssistant(t *testing.T) {
	// R-EOSH-U574
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a"},{"type":"other","text":"x"},{"type":"output_text","text":"b"}]}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindAssistant || entries[0].Text != "ab" {
		t.Fatalf("assistant entries=%v", entries)
	}
}

func TestDecoderAgentMessage(t *testing.T) {
	// R-SHRO-3TIN
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"agent_message","content":[{"type":"input_text","text":"Sender: /root\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"cipher"}]}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindAgent || entries[0].Text != "Sender: /root" {
		t.Fatalf("agent entries=%v", entries)
	}
}

func TestDecoderReasoningSummary(t *testing.T) {
	// R-ER8A-LOOI
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"one"},{"type":"other","text":"skip"},{"type":"summary_text","text":"two"}],"content":[{"text":"private"}]}}`)
	if len(entries) != 2 || entries[0].Text != "one" || entries[1].Text != "two" {
		t.Fatalf("reasoning entries=%v", entries)
	}
}

func TestDecoderToolRedaction(t *testing.T) {
	// R-XFBG-18LH
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","arguments":"{\"task_name\":\"radar\",\"message\":\"secret\"}"}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindTool || entries[0].Tool != "spawn_agent" || entries[0].Text != `{"task_name":"radar"}` {
		t.Fatalf("tool entries=%v", entries)
	}
}

func TestDecoderResultKind(t *testing.T) {
	// R-ETO3-D85W
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"custom_tool_call_output","output":"Script completed"}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindResultOK || entries[0].Text != "" || entries[0].Tool != "" {
		t.Fatalf("result entries=%v", entries)
	}
}

func TestDecoderFailedOutput(t *testing.T) {
	// R-EUVZ-QZWL
	d := &chatDecoder{ownID: "root"}
	entries, _ := decode(t, d, `{"type":"response_item","payload":{"type":"function_call_output","output":[{"text":"fine"},{"text":"{\"exit_code\":-1}"}]}}`)
	if len(entries) != 1 || entries[0].Kind != chat.KindResultError {
		t.Fatalf("result entries=%v", entries)
	}
	for _, tc := range []struct {
		code string
		kind chat.Kind
	}{
		{code: "1e-1000", kind: chat.KindResultError},
		{code: "1e999", kind: chat.KindResultError},
		{code: "0e999", kind: chat.KindResultOK},
		{code: `\"1\"`, kind: chat.KindResultOK},
	} {
		record := `{"type":"response_item","payload":{"type":"custom_tool_call_output","output":"{\"exit_code\":` + tc.code + `}"}}`
		entries, _ := decode(t, d, record)
		if len(entries) != 1 || entries[0].Kind != tc.kind {
			t.Fatalf("exit_code %s: entries=%v", tc.code, entries)
		}
	}
}

func TestDecoderOwnUsage(t *testing.T) {
	// R-EW3W-4RNA R-PP9R-690C
	d := &chatDecoder{ownID: "root"}
	_, other := decode(t, d, `{"type":"token_usage_record","payload":{"thread_id":"child","usage":{"input_tokens":10}}}`)
	_, own := decode(t, d, `{"type":"token_usage_record","payload":{"thread_id":"root","usage":{"input_tokens":14408,"cached_input_tokens":7808,"output_tokens":74,"reasoning_output_tokens":9}}}`)
	if other != (chat.Usage{}) || own != (chat.Usage{In: 6600, CacheRead: 7808, Out: 74, Reasoning: 9, Calls: 1}) {
		t.Fatalf("other=%+v own=%+v", other, own)
	}
}

func TestChatErrorForms(t *testing.T) {
	// R-LQGL-SHO4 R-LROI-69ET R-PO1U-SH9N
	f := fixture()
	tr, entries, err := Chat(f, home, "root", "root")
	if tr != nil || entries != nil || !reflect.DeepEqual(err, tree.ErrNotFound) {
		t.Fatalf("missing: %v %v %v", tr, entries, err)
	}
	f.fail["stat:"+strings.TrimSuffix(sessions, "/")] = &fs.PathError{Op: "stat", Path: sessions, Err: fs.ErrPermission}
	tr, entries, err = Chat(f, home, "root", "root")
	var read *session.ReadError
	if tr != nil || entries != nil || !errors.As(err, &read) || read.Path != "/"+strings.TrimSuffix(sessions, "/") || !errors.Is(read.Err, fs.ErrPermission) {
		t.Fatalf("unreadable: %v %v %#v", tr, entries, err)
	}
}

func TestChatUnknownAgent(t *testing.T) {
	// R-LSWE-K15I
	f := chatFixture()
	chatRollout(f, "root", "{}\n")
	for _, id := range []string{"", "roo", "other"} {
		tr, entries, err := Chat(f, home, "root", id)
		if tr != nil || entries != nil || !reflect.DeepEqual(err, chat.ErrAgentNotFound) {
			t.Fatalf("%q: %v %v %v", id, tr, entries, err)
		}
	}
}

func TestChatOnePassEntries(t *testing.T) {
	// R-LU4A-XSW7 R-5HSP-OSAY
	f := &trackedChatFS{observedFS: chatFixture()}
	rootFile := chatRollout(f.observedFS, "root", `{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"child"}}}`+"\n")
	childFile := chatRollout(f.observedFS, "child", `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`+"\n")
	tr, entries, err := Chat(f, home, "root", "child")
	if err != nil || tr == nil || len(entries) != 1 || entries[0].Text != "hello" || tr.Path() != "/"+childFile {
		t.Fatalf("transcript=%v entries=%v err=%v", tr, entries, err)
	}
	for _, file := range []string{rootFile, childFile} {
		if countCall(f.calls, "open:"+file) != 1 || countCall(f.fileCalls, "readat:"+file) != 1 {
			t.Fatalf("file %s was not read in one pass: root=%v files=%v", file, f.calls, f.fileCalls)
		}
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "read:") || strings.HasPrefix(call, "open:") && call != "open:"+rootFile && call != "open:"+childFile {
			t.Fatalf("log read outside allowed passes: %v", f.calls)
		}
	}
	for _, call := range f.fileCalls {
		if call != "readat:"+rootFile && call != "readat:"+childFile {
			t.Fatalf("log file method outside passes: %v", f.fileCalls)
		}
	}
}

func TestDecoderNoFSCalls(t *testing.T) {
	// R-GTCN-9NFH
	d := &chatDecoder{ownID: "root"}
	f := fixture()
	d.Decode(f, []byte(`{"type":"token_usage_record","payload":{"thread_id":"root","usage":{}}}`))
	if len(f.calls) != 0 {
		t.Fatalf("calls=%v", f.calls)
	}
}
