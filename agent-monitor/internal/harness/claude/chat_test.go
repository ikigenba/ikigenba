package claude_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// R-L0UP-RB3J
var _ func(fs.FS, string, string, string) (*chat.Transcript, []chat.Entry, error) = claude.Chat

func getChat(t *testing.T, root fs.FS, agent string) (*chat.Transcript, []chat.Entry) {
	t.Helper()
	tr, e, err := claude.Chat(root, "/home/dev", rootID, agent)
	if err != nil {
		t.Fatal(err)
	}
	return tr, e
}

// R-LROI-69ET R-LQGL-SHO4 R-PO1U-SH9N
func TestChatSessionErrors(t *testing.T) {
	for _, root := range []fs.FS{fstest.MapFS{}, treeFixture()} {
		for _, id := range []string{"missing", "", ".", "..", "bad/id"} {
			_, te := claude.Tree(root, "/home/dev", id)
			tr, e, ce := claude.Chat(root, "/home/dev", id, rootID)
			if !reflect.DeepEqual(te, tree.ErrNotFound) || !reflect.DeepEqual(ce, tree.ErrNotFound) || tr != nil || e != nil {
				t.Fatalf("%q: %v %v", id, te, ce)
			}
		}
	}
	for _, op := range []string{"stat", "readdir"} {
		root := &treeObserved{files: treeFixture(), failName: "home/dev/.claude/projects", failOp: op, failErr: &fs.PathError{Op: op, Path: "other", Err: fs.ErrPermission}}
		tr, e, err := claude.Chat(root, "/home/dev", rootID, agentOne)
		var re *session.ReadError
		if tr != nil || e != nil || !errors.As(err, &re) || reflect.TypeOf(err) != reflect.TypeOf(re) || re.Path != "/home/dev/.claude/projects" || !errors.Is(re.Err, fs.ErrPermission) || reflect.TypeOf(re.Err) != reflect.TypeOf(fs.ErrPermission) {
			t.Fatalf("%s: %v %#v", op, tr, err)
		}
	}
}

// R-LSWE-K15I R-0S08-OGG1 R-LU4A-XSW7 R-LXS0-344A R-0QSC-AOPC
func TestChatAgentSelection(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, "{\"parentAgentId\":\"second-agent\"}")
	for _, id := range []string{"", "first", agentTwo, "unknown"} {
		tr, e, err := claude.Chat(m, "/home/dev", rootID, id)
		if !reflect.DeepEqual(err, chat.ErrAgentNotFound) || tr != nil || e != nil {
			t.Fatalf("%q: %v", id, err)
		}
	}
	for _, tc := range []struct{ id, path string }{{rootID, "/" + strings.TrimSuffix(rootLog, "/")}, {agentOne, "/" + agentDir + "agent-" + agentOne + ".jsonl"}} {
		tr, e := getChat(t, m, tc.id)
		if tr.Path() != tc.path || len(e) != 0 || tr.Recorded() != (chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}) {
			t.Fatalf("%s: %q %+v", tc.id, tr.Path(), tr.Recorded())
		}
	}
}

// R-HUB1-QIKY R-YOQ4-6NDT R-HVIY-4ABN
func TestChatUserRecords(t *testing.T) {
	m := treeFixture()
	put(m, rootLog, strings.Join([]string{
		"{\"type\":\"user\",\"message\":{\"content\":\"<command-message>review</command-message>\\n<command-name>/review</command-name>\\n<command-args>cart.go</command-args>\"},\"timestamp\":\"2026-09-24T19:57:40Z\"}",
		"{\"type\":\"user\",\"origin\":{\"kind\":\"human\"},\"isMeta\":true,\"message\":{\"content\":\"hello\"}}",
		"{\"type\":\"user\",\"origin\":{\"kind\":\"coordinator\"},\"message\":{\"content\":\"work\"}}",
		"{\"type\":\"user\",\"isMeta\":true,\"message\":{\"content\":\"hidden\"}}",
		"{\"type\":\"user\",\"message\":{\"content\":\"<local-command-stdout>hidden\"}}",
		"{\"type\":\"system\",\"message\":{\"content\":\"hidden\"}}",
	}, "\n")+"\n")
	_, got := getChat(t, m, rootID)
	want := []chat.Entry{{Kind: chat.KindUser, Text: "/review cart.go", HasTime: true}, {Kind: chat.KindUser, Text: "hello"}, {Kind: chat.KindAgent, Text: "work"}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i].Kind != want[i].Kind || got[i].Text != want[i].Text || got[i].HasTime != want[i].HasTime || got[i].Tool != "" {
			t.Fatalf("%d: %+v", i, got[i])
		}
	}
	if got[0].Time.Format("2006-01-02T15:04:05Z07:00") != "2026-09-24T19:57:40Z" {
		t.Fatal(got[0].Time)
	}
	subLog(m, agentOne, "{\"type\":\"user\",\"message\":{\"content\":\"task\"}}\n")
	_, sub := getChat(t, m, agentOne)
	if len(sub) != 1 || sub[0].Kind != chat.KindAgent {
		t.Fatalf("%+v", sub)
	}
}

// R-IRQR-KRW0 R-ISYN-YJMP R-IU6K-CBDE
func TestChatBlocks(t *testing.T) {
	m := treeFixture()
	put(m, rootLog, strings.Join([]string{
		"{\"type\":\"attachment\",\"attachment\":{\"type\":\"queued_command\",\"prompt\":\"start\",\"origin\":{\"kind\":\"human\"}}}",
		"{\"type\":\"attachment\",\"attachment\":{\"type\":\"queued_command\",\"prompt\":\"continue\",\"commandMode\":\"task-notification\"}}",
		"{\"type\":\"attachment\",\"attachment\":{\"type\":\"other\",\"prompt\":\"skip\",\"origin\":{\"kind\":\"human\"}}}",
		"{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"skip\"},{\"type\":\"tool_result\"},{\"type\":\"tool_result\",\"is_error\":true}]}}",
		"{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"answer\"},{\"type\":\"thinking\",\"thinking\":\"thought\"},{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\": {\"command\":\"ls\"}},{\"type\":\"tool_use\",\"name\":\"Read\"}]}}",
	}, "\n")+"\n")
	_, got := getChat(t, m, rootID)
	want := []chat.Entry{{Kind: chat.KindUser, Text: "start"}, {Kind: chat.KindAgent, Text: "continue"}, {Kind: chat.KindResultOK}, {Kind: chat.KindResultError}, {Kind: chat.KindAssistant, Text: "answer"}, {Kind: chat.KindReasoning, Text: "thought"}, {Kind: chat.KindTool, Tool: "Bash", Text: "{\"command\":\"ls\"}"}, {Kind: chat.KindTool, Tool: "Read"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

// R-IWMD-3UUS R-IXU9-HMLH R-PP9R-690C
func TestChatUsage(t *testing.T) {
	m := treeFixture()
	first := "{\"type\":\"assistant\",\"message\":{\"id\":\"same\",\"usage\":{\"input_tokens\":3,\"cache_creation_input_tokens\":9,\"cache_read_input_tokens\":7,\"output_tokens\":24,\"output_tokens_details\":{\"thinking_tokens\":4}}}}\n"
	put(m, rootLog, first)
	tr, _ := getChat(t, m, rootID)
	if tr.Usage() != (chat.Usage{In: 3, CacheWrite: 9, CacheRead: 7, Out: 24, Reasoning: 4, Calls: 1}) {
		t.Fatal(tr.Usage())
	}
	put(m, rootLog, first+"{\"type\":\"assistant\",\"message\":{\"id\":\"same\",\"usage\":{\"input_tokens\":2,\"cache_creation_input_tokens\":11,\"cache_read_input_tokens\":1,\"output_tokens\":210,\"output_tokens_details\":{\"thinking_tokens\":1}}}}\n")
	_, _, err := tr.Read(m)
	if err != nil || tr.Usage() != (chat.Usage{In: 3, CacheWrite: 11, CacheRead: 7, Out: 210, Reasoning: 4, Calls: 1}) {
		t.Fatalf("%+v %v", tr.Usage(), err)
	}
	subLog(m, agentOne, "{\"type\":\"assistant\",\"message\":{\"id\":\"other\",\"usage\":{\"input_tokens\":100,\"output_tokens\":100}}}\n")
	sub, _ := getChat(t, m, agentOne)
	if sub.Usage().Calls != 1 || tr.Usage().Calls != 1 || tr.Usage().In != 3 {
		t.Fatalf("root=%+v sub=%+v", tr.Usage(), sub.Usage())
	}
}

// R-IWMD-3UUS
func TestChatUsageNumberValues(t *testing.T) {
	m := treeFixture()
	put(m, rootLog, strings.Join([]string{
		"{\"type\":\"assistant\",\"message\":{\"id\":\"decimal\",\"usage\":{\"input_tokens\":3.0,\"cache_creation_input_tokens\":1e2,\"cache_read_input_tokens\":9223372036854775807.0,\"output_tokens\":-4,\"output_tokens_details\":{\"thinking_tokens\":1.50e1}}}}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"fraction\",\"usage\":{\"input_tokens\":3.1,\"cache_creation_input_tokens\":\"8\",\"cache_read_input_tokens\":9223372036854775808,\"output_tokens\":1e-2}}}",
	}, "\n")+"\n")
	tr, _ := getChat(t, m, rootID)
	want := chat.Usage{In: 3, CacheWrite: 100, CacheRead: 9223372036854775807, Reasoning: 15, Calls: 2}
	if tr.Usage() != want {
		t.Fatalf("usage=%+v want=%+v", tr.Usage(), want)
	}
}

// R-IPAY-T8EM
func TestChatForkHead(t *testing.T) {
	m := treeFixture()
	put(m, rootLog, strings.Join([]string{
		"{\"type\":\"fork-context-ref\"}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"parent\",\"content\":[{\"type\":\"text\",\"text\":\"copied\"}],\"usage\":{\"output_tokens\":40}}}",
		"{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\"},{\"type\":\"text\",\"text\":\"<fork-boilerplate>directive\"}]}}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"own\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"usage\":{\"output_tokens\":5}}}",
	}, "\n")+"\n")
	tr, got := getChat(t, m, rootID)
	if len(got) != 2 || got[0].Kind != chat.KindAgent || got[0].Text != "<fork-boilerplate>directive" || got[1].Text != "answer" || tr.Usage().Calls != 1 || tr.Usage().Out != 5 {
		t.Fatalf("%+v %+v", got, tr.Usage())
	}
	put(m, rootLog, "{\"type\":\"fork-context-ref\"}\n{\"type\":\"assistant\",\"message\":{\"id\":\"copied\",\"content\":[{\"type\":\"text\",\"text\":\"copy\"}],\"usage\":{\"output_tokens\":8}}}\n")
	noDirective, entries := getChat(t, m, rootID)
	if len(entries) != 0 || noDirective.Usage() != (chat.Usage{}) {
		t.Fatalf("no directive: %+v %+v", entries, noDirective.Usage())
	}
	put(m, rootLog, "{\"type\":\"assistant\",\"message\":{\"id\":\"own\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"usage\":{\"output_tokens\":5}}}\n")
	plain, entries := getChat(t, m, rootID)
	if len(entries) != 1 || entries[0].Text != "answer" || plain.Usage().Calls != 1 || plain.Usage().Out != 5 {
		t.Fatalf("plain: %+v %+v", entries, plain.Usage())
	}
}

// R-YH2G-31ED R-IMV6-1OX8 R-IO32-FGNX R-5HSP-OSAY R-GTCN-9NFH
func TestChatReadBoundary(t *testing.T) {
	m := treeFixture()
	subLog(m, agentOne, "{\"type\":\"user\",\"message\":{\"content\":\"task\"}}\n")
	subMeta(m, agentOne, "{\"parentAgentId\":\"second-agent\"}")
	root := &treeObserved{files: m}
	tr, _ := getChat(t, root, agentOne)
	_, _, err := tr.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range root.accesses {
		if strings.HasSuffix(name, ".meta.json") || strings.HasSuffix(name, ".key") || strings.Contains(name, "agent-"+agentTwo) {
			t.Fatalf("forbidden access: %q", name)
		}
	}
	if contains(root.readFiles, rootLog) || contains(root.fileReadAts, rootLog) {
		t.Fatalf("root transcript content read: %+v", root)
	}
	if len(root.fileReads) != 0 || len(root.fileReadAts) != 1 {
		t.Fatalf("reads=%v readats=%v", root.fileReads, root.fileReadAts)
	}
	pass := &passFS{files: m, opens: map[string]int{}, reads: map[string]int{}, readAts: map[string]int{}, readDirs: map[string]int{}, stats: map[string]int{}, closes: map[string]int{}, ranges: map[string][][2]int64{}}
	passTr, _ := getChat(t, pass, agentOne)
	if _, _, err := passTr.Read(pass); err != nil || len(pass.writes) != 0 || pass.reads[agentDir+"agent-"+agentOne+".jsonl"] != 0 {
		t.Fatalf("pass error=%v writes=%v reads=%v", err, pass.writes, pass.reads)
	}
}

// R-LQGL-SHO4 R-PO1U-SH9N
func TestChatTranscriptReadError(t *testing.T) {
	m := treeFixture()
	put(m, rootLog, "{}\n")
	root := &treeObserved{files: m, failName: rootLog, failOp: "readat", failErr: &fs.PathError{Op: "readat", Path: "other", Err: fs.ErrPermission}}
	tr, entries, err := claude.Chat(root, "/home/dev", rootID, rootID)
	var read *session.ReadError
	if tr != nil || entries != nil || !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(read) || read.Path != "/"+rootLog || !errors.Is(read.Err, fs.ErrPermission) || reflect.TypeOf(read.Err) != reflect.TypeOf(fs.ErrPermission) {
		t.Fatalf("transcript error: %v %v %#v", tr, entries, err)
	}
}

func storyRoot(m fstest.MapFS) string {
	const id = "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"
	name := projects + "-home-dev-src-shop/" + id + ".jsonl"
	put(m, name, strings.Join([]string{
		"{\"type\":\"user\",\"isMeta\":true,\"message\":{\"role\":\"user\",\"content\":\"<system-reminder>\\nContents of /home/dev/src/shop/CLAUDE.md:\\n\\nRun make test before committing.\\n</system-reminder>\"},\"timestamp\":\"2026-09-24T19:57:40.101Z\"}",
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"<command-name>/review</command-name>\\n<command-message>review</command-message>\\n<command-args>cart.go</command-args>\"},\"timestamp\":\"2026-09-24T19:57:40.518Z\"}",
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"<local-command-stdout>Review started for cart.go</local-command-stdout>\"},\"timestamp\":\"2026-09-24T19:57:40.602Z\"}",
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"commit all files\"},\"timestamp\":\"2026-09-24T19:58:03.412Z\"}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"msg_01Qm4\",\"role\":\"assistant\",\"content\":[{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"EqQBCkYIBxgCKkA\"}],\"usage\":{\"input_tokens\":3,\"cache_creation_input_tokens\":5120,\"cache_read_input_tokens\":11840,\"output_tokens\":96,\"output_tokens_details\":{\"thinking_tokens\":30}}},\"timestamp\":\"2026-09-24T19:58:05.030Z\"}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"msg_01Qm4\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"I'll check the tree first.\"}],\"usage\":{\"input_tokens\":3,\"cache_creation_input_tokens\":5120,\"cache_read_input_tokens\":11840,\"output_tokens\":96,\"output_tokens_details\":{\"thinking_tokens\":30}}},\"timestamp\":\"2026-09-24T19:58:05.377Z\"}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"msg_01Qm4\",\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_01Hx\",\"name\":\"Bash\",\"input\":{\"command\":\"git status --short\",\"description\":\"Show working tree status\"}}],\"usage\":{\"input_tokens\":3,\"cache_creation_input_tokens\":5120,\"cache_read_input_tokens\":11840,\"output_tokens\":96,\"output_tokens_details\":{\"thinking_tokens\":30}}},\"timestamp\":\"2026-09-24T19:58:06.204Z\"}",
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"toolu_01Hx\",\"content\":\" M cart.go\\n?? refund.go\"}]},\"timestamp\":\"2026-09-24T19:58:07.650Z\"}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"msg_01Rt7\",\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_01Jk\",\"name\":\"Bash\",\"input\":{\"command\":\"git add -A && git commit -m \\\"Fix refund rounding\\\"\",\"description\":\"Commit all changes\"}}],\"usage\":{\"input_tokens\":1,\"cache_creation_input_tokens\":180,\"cache_read_input_tokens\":16960,\"output_tokens\":64,\"output_tokens_details\":{\"thinking_tokens\":0}}},\"timestamp\":\"2026-09-24T19:58:09.118Z\"}",
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"toolu_01Jk\",\"content\":\"error: gpg failed to sign the data\",\"is_error\":true}]},\"timestamp\":\"2026-09-24T19:58:10.402Z\"}",
		"{\"type\":\"assistant\",\"message\":{\"id\":\"msg_01Sv2\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"The commit failed: gpg could not sign it.\\nUnlock your key and I'll retry.\"}],\"usage\":{\"input_tokens\":1,\"cache_creation_input_tokens\":91,\"cache_read_input_tokens\":17331,\"output_tokens\":41,\"output_tokens_details\":{\"thinking_tokens\":0}}},\"timestamp\":\"2026-09-24T19:58:12.870Z\"}",
		"{\"type\":\"system\",\"subtype\":\"turn_duration\",\"durationMs\":9458,\"timestamp\":\"2026-09-24T19:58:12.871Z\"}",
	}, "\n")+"\n")
	return id
}

// R-9EAC-AJT0
func TestChatStoryRoot(t *testing.T) {
	m := treeFixture()
	id := storyRoot(m)
	tr, entries, err := claude.Chat(m, "/home/dev", id, id)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, e := range entries {
		got += chat.Format(e)
	}
	want := "2026-09-24T19:57:40Z user\n/review cart.go\n\n2026-09-24T19:58:03Z user\ncommit all files\n\n2026-09-24T19:58:05Z assistant\nI'll check the tree first.\n\n2026-09-24T19:58:06Z tool Bash\n{\"command\":\"git status --short\",\"description\":\"Show working tree status\"}\n\n2026-09-24T19:58:07Z result ok\n\n2026-09-24T19:58:09Z tool Bash\n{\"command\":\"git add -A && git commit -m \\\"Fix refund rounding\\\"\",\"description\":\"Commit all changes\"}\n\n2026-09-24T19:58:10Z result error\n\n2026-09-24T19:58:12Z assistant\nThe commit failed: gpg could not sign it.\nUnlock your key and I'll retry.\n\n"
	if got != want || tr.Usage() != (chat.Usage{In: 5, CacheWrite: 5391, CacheRead: 46131, Out: 201, Reasoning: 30, Calls: 3}) {
		t.Fatalf("output:\n%s\nusage:%+v", got, tr.Usage())
	}
}

// R-9FI8-OBJP
func TestChatStorySubagent(t *testing.T) {
	m := treeFixture()
	id := storyRoot(m)
	const agent = "a1c4e7f09b2d38561"
	put(m, projects+"-home-dev-src-shop/"+id+"/subagents/agent-"+agent+".jsonl", strings.Join([]string{
		"{\"type\":\"user\",\"isSidechain\":true,\"agentId\":\"a1c4e7f09b2d38561\",\"message\":{\"role\":\"user\",\"content\":\"Find where checkout requests are handled.\\nReport the file and the function.\"},\"timestamp\":\"2026-09-24T20:01:14.220Z\"}",
		"{\"type\":\"assistant\",\"isSidechain\":true,\"agentId\":\"a1c4e7f09b2d38561\",\"message\":{\"id\":\"msg_02Ab9\",\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_02Kp\",\"name\":\"Grep\",\"input\":{\"pattern\":\"func .*Checkout\",\"path\":\"/home/dev/src/shop\"}}],\"usage\":{\"input_tokens\":3,\"cache_creation_input_tokens\":4210,\"cache_read_input_tokens\":9870,\"output_tokens\":58,\"output_tokens_details\":{\"thinking_tokens\":0}}},\"timestamp\":\"2026-09-24T20:01:16.045Z\"}",
		"{\"type\":\"user\",\"isSidechain\":true,\"agentId\":\"a1c4e7f09b2d38561\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"toolu_02Kp\",\"content\":\"internal/cart/checkout.go:42:func HandleCheckout(w http.ResponseWriter, r *http.Request) {\"}]},\"timestamp\":\"2026-09-24T20:01:16.913Z\"}",
		"{\"type\":\"assistant\",\"isSidechain\":true,\"agentId\":\"a1c4e7f09b2d38561\",\"message\":{\"id\":\"msg_02Bc4\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"The handler is HandleCheckout in internal/cart/checkout.go, line 42.\"}],\"usage\":{\"input_tokens\":1,\"cache_creation_input_tokens\":160,\"cache_read_input_tokens\":14080,\"output_tokens\":37,\"output_tokens_details\":{\"thinking_tokens\":0}}},\"timestamp\":\"2026-09-24T20:01:19.588Z\"}",
	}, "\n")+"\n")
	root, _, err := claude.Chat(m, "/home/dev", id, id)
	if err != nil {
		t.Fatal(err)
	}
	before := root.Usage()
	tr, entries, err := claude.Chat(m, "/home/dev", id, agent)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, e := range entries {
		got += chat.Format(e)
	}
	want := "2026-09-24T20:01:14Z agent\nFind where checkout requests are handled.\nReport the file and the function.\n\n2026-09-24T20:01:16Z tool Grep\n{\"pattern\":\"func .*Checkout\",\"path\":\"/home/dev/src/shop\"}\n\n2026-09-24T20:01:16Z result ok\n\n2026-09-24T20:01:19Z assistant\nThe handler is HandleCheckout in internal/cart/checkout.go, line 42.\n\n"
	if got != want || tr.Usage() != (chat.Usage{In: 4, CacheWrite: 4370, CacheRead: 23950, Out: 95, Calls: 2}) || before != (chat.Usage{In: 5, CacheWrite: 5391, CacheRead: 46131, Out: 201, Reasoning: 30, Calls: 3}) || root.Usage() != before {
		t.Fatalf("output:\n%s\nusage:%+v root:%+v", got, tr.Usage(), root.Usage())
	}
}
