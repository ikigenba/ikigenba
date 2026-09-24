package claude_test

import (
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

const rootID = "root-session"
const agentOne = "first-agent"
const agentTwo = "second-agent"
const project = projects + "work/"
const rootLog = project + rootID + ".jsonl"
const agentDir = project + rootID + "/subagents/"

func treeFixture() fstest.MapFS {
	m := fixture()
	put(m, rootLog, "")
	put(m, registry+"a.json", `{"sessionId":"root-session","pid":42,"status":"busy","name":"registered"}`)
	return m
}

func subMeta(m fstest.MapFS, agent, value string) {
	put(m, agentDir+"agent-"+agent+".meta.json", value)
}

func subLog(m fstest.MapFS, agent, value string) {
	put(m, agentDir+"agent-"+agent+".jsonl", value)
}

func getTree(t *testing.T, root fs.FS) tree.Tree {
	t.Helper()
	got, err := claude.Tree(root, "/home/dev", rootID)
	if err != nil {
		t.Fatalf("Tree error: %v", err)
	}
	return got
}

// R-0YC9-NT3U R-DKI4-OYFH
var _ func(fs.FS, string, string) (tree.Tree, error) = claude.Tree

// R-51PW-B3K9 R-52XS-OVAY R-33Y8-84BG R-XTHT-0P41
func TestTreeLocatingDirectory(t *testing.T) {
	root := &treeObserved{files: treeFixture()}
	getTree(t, root)
	if !reflect.DeepEqual(root.stats[:1], []string{"home/dev/.claude/projects"}) || !reflect.DeepEqual(root.dirs[:1], []string{"home/dev/.claude/projects"}) {
		t.Fatalf("locating directory: stats=%v dirs=%v", root.stats, root.dirs)
	}
	for _, op := range []string{"stat", "readdir"} {
		root := &treeObserved{files: treeFixture(), failName: "home/dev/.claude/projects", failOp: op, failErr: &fs.PathError{Op: op, Path: "other", Err: fs.ErrPermission}}
		got, err := claude.Tree(root, "/home/dev", rootID)
		var read *session.ReadError
		if !reflect.DeepEqual(got, tree.Tree{}) || !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(read) || read.Path != "/home/dev/.claude/projects" || !errors.Is(read.Err, fs.ErrPermission) || reflect.TypeOf(read.Err) != reflect.TypeOf(fs.ErrPermission) {
			t.Fatalf("%s: tree=%+v error=%#v", op, got, err)
		}
	}
	got, err := claude.Tree(fstest.MapFS{}, "/home/dev", rootID)
	if !errors.Is(err, tree.ErrNotFound) || !reflect.DeepEqual(err, tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("missing locating directory: %+v %v", got, err)
	}
}

// R-6AJ7-HMZN R-10S2-FCL8 R-6BR3-VEQC R-137V-6W2M R-JERO-H7QH
func TestTreeRootDiscovery(t *testing.T) {
	m := treeFixture()
	for _, id := range []string{"", ".", "..", "bad/id"} {
		root := &treeObserved{files: m}
		got, err := claude.Tree(root, "/home/dev", id)
		if !errors.Is(err, tree.ErrNotFound) || !reflect.DeepEqual(err, tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
			t.Fatalf("invalid id %q: %+v %v %v", id, got, err, root.accesses)
		}
		for _, name := range root.accesses {
			if name != "home/dev/.claude/projects" {
				t.Fatalf("invalid id %q accessed %q", id, name)
			}
		}
	}
	root := &treeObserved{files: m}
	got := getTree(t, root)
	if got.Root.ID != rootID || got.Root.Label != "registered" {
		t.Fatalf("found root: %+v", got.Root)
	}
	if !contains(root.accesses, registry+"a.json") {
		t.Fatalf("registry path: %v", root.accesses)
	}
	delete(m, rootLog)
	put(m, projects+"keep", "")
	got = getTree(t, m)
	if got.Root.ID != rootID {
		t.Fatalf("registry-only root: %+v", got)
	}
	delete(m, registry+"a.json")
	_, err := claude.Tree(m, "/home/dev", rootID)
	if !errors.Is(err, tree.ErrNotFound) || !reflect.DeepEqual(err, tree.ErrNotFound) {
		t.Fatalf("no root: %v", err)
	}
	put(m, rootLog, "")
	subLog(m, agentOne, "")
	_, err = claude.Tree(m, "/home/dev", agentOne)
	if !errors.Is(err, tree.ErrNotFound) || !reflect.DeepEqual(err, tree.ErrNotFound) {
		t.Fatalf("subagent treated as root: %v", err)
	}
}

// R-52XS-OVAY R-JERO-H7QH R-XTHT-0P41
func TestTreeOptionalFailuresAndMissingDirectory(t *testing.T) {
	m := treeFixture()
	root := &treeObserved{files: m, failName: "home/dev/.claude/projects", failOp: "readdir", failErr: fs.ErrNotExist}
	got, err := claude.Tree(root, "/home/dev", rootID)
	if !errors.Is(err, tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("missing listing: %+v %v", got, err)
	}
	root = &treeObserved{files: m, failName: "home/dev/.claude/projects/work/root-session/subagents", failOp: "readdir", failErr: fs.ErrPermission}
	got = getTree(t, root)
	if got.Root.ID != rootID || len(got.Subagents) != 0 {
		t.Fatalf("unreadable subagents: %+v", got)
	}
}

// R-6E6W-MY7Q R-183G-PZ1E R-6GMP-EHP4 R-1AJ9-HIIS R-1BR5-VA9H
func TestTreeRootFields(t *testing.T) {
	m := treeFixture()
	put(m, projects+"z/"+rootID+".jsonl", `{"type":"custom-title","customTitle":"wrong"}`+"\n")
	put(m, rootLog, strings.Join([]string{
		`{"type":"custom-title","customTitle":"first"}`,
		`{"type":"ai-title","aiTitle":"AI title"}`,
		`{"type":"custom-title","customTitle":"chosen"}`,
		`{"type":"custom-title","customTitle":""}`,
		`{"type":"ai-title","aiTitle":"later AI"}`,
	}, "\n")+"\n")
	put(m, registry+"a.json", `{"sessionId":"root-session","pid":42,"status":"busy"}`)
	got := getTree(t, m)
	if got.Root.ID != rootID || got.Root.Parent != "" || got.Root.HasStarted || !got.Root.Started.IsZero() || got.Root.Label != "chosen" || got.Root.Status != tree.StatusWorking {
		t.Fatalf("root fields: %+v", got.Root)
	}
	put(m, registry+"a.json", `{"sessionId":"root-session","pid":42,"status":"idle","name":"live name"}`)
	got = getTree(t, m)
	if got.Root.Label != "live name" || got.Root.Status != tree.StatusIdle {
		t.Fatalf("live fields: %+v", got.Root)
	}
	put(m, registry+"a.json", `{"sessionId":"root-session","pid":42,"status":"other"}`)
	if got = getTree(t, m); got.Root.Status != tree.StatusUnknown {
		t.Fatalf("unknown live status: %+v", got.Root)
	}
	delete(m, registry+"a.json")
	if got = getTree(t, m); got.Root.Status != tree.StatusEnded || got.Root.Label != "chosen" {
		t.Fatalf("ended root: %+v", got.Root)
	}
	root := &treeObserved{files: m, failName: "home/dev/.claude/sessions", failOp: "readdir", failErr: fs.ErrPermission}
	if got = getTree(t, root); got.Root.Status != tree.StatusUnknown {
		t.Fatalf("unknown liveness: %+v", got.Root)
	}
}

// R-1BR5-VA9H R-52XS-OVAY
func TestTreeTranscriptTitleFallbacks(t *testing.T) {
	m := treeFixture()
	put(m, registry+"a.json", `{"sessionId":"root-session","pid":42}`)
	put(m, rootLog, `{"type":"ai-title","aiTitle":"AI fallback"}`+"\n")
	if got := getTree(t, m); got.Root.Label != "AI fallback" {
		t.Fatalf("AI title: %+v", got.Root)
	}
	put(m, rootLog, `{"type":"custom-title","customTitle":""}`+"\n")
	if got := getTree(t, m); got.Root.Label != "" {
		t.Fatalf("empty title: %+v", got.Root)
	}
	put(m, rootLog, `{"type":"custom-title","customTitle":"unreadable"}`+"\n")
	root := &treeObserved{files: m, failName: rootLog, failOp: "readat", failErr: fs.ErrPermission}
	if got := getTree(t, root); got.Root.Label != "" || got.Root.Status != tree.StatusUnknown {
		t.Fatalf("unreadable transcript: %+v", got.Root)
	}
}

// R-6FET-0PYF R-1YX9-4XCO R-6RLS-UFDD
func TestTreeLiveRegistrationSelection(t *testing.T) {
	m := treeFixture()
	put(m, registry+"a.json", `null`)
	put(m, registry+"b.json", `{"sessionId":"root-session","pid":42,"status":"idle","name":"first"}`)
	put(m, registry+"c.json", `{"sessionId":"root-session","pid":42,"status":"busy","name":"second"}`)
	put(m, registry+"secret.key", "secret")
	root := &treeObserved{files: m}
	got := getTree(t, root)
	if got.Root.Label != "first" || got.Root.Status != tree.StatusIdle {
		t.Fatalf("first live registration: %+v", got.Root)
	}
	for _, name := range root.accesses {
		if strings.HasSuffix(name, ".key") || strings.Contains(name, "secret") {
			t.Fatalf("accessed key: %v", root.accesses)
		}
	}
	if !contains(root.readFiles, registry+"a.json") || !contains(root.readFiles, registry+"b.json") {
		t.Fatalf("whole registration reads: %v", root.readFiles)
	}
	put(m, registry+"b.json", `{"sessionId":"root-session","pid":42,"procStart":"+150","startedAt":1001499,"status":"idle"}`)
	put(m, registry+"c.json", `{"sessionId":"root-session","pid":42,"status":"busy","name":"second"}`)
	got = getTree(t, m)
	if got.Root.Status != tree.StatusWorking || got.Root.Label != "second" {
		t.Fatalf("invalid procStart must use startedAt: %+v", got.Root)
	}
}

// R-6RLS-UFDD R-1XPC-R5LZ R-1YX9-4XCO
func TestTreeWholeFileReadsAndAllowedNames(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"description":"from meta"}`)
	subLog(m, agentOne, "")
	put(m, project+"unrelated.jsonl", "secret")
	put(m, project+rootID+"/unrelated.json", "secret")
	put(m, agentDir+"agent-other.txt", "secret")
	put(m, registry+"secret.key", "secret")
	root := &treeObserved{files: m}
	got := getTree(t, root)
	if got.Subagents[0].Label != "from meta" {
		t.Fatalf("meta label: %+v", got.Subagents[0])
	}
	for _, name := range []string{registry + "a.json", agentDir + "agent-" + agentOne + ".meta.json"} {
		count := 0
		for _, called := range root.readFiles {
			if called == name {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("ReadFile calls for %s: %v", name, root.readFiles)
		}
		accessCount := 0
		for _, called := range root.accesses {
			if called == name {
				accessCount++
			}
		}
		if accessCount != 1 || contains(root.fileReads, name) || contains(root.fileReadAts, name) {
			t.Fatalf("extra access to %s: accesses=%v reads=%v readAts=%v", name, root.accesses, root.fileReads, root.fileReadAts)
		}
	}
	for _, name := range root.accesses {
		if strings.Contains(name, "unrelated") || strings.Contains(name, "other.txt") || strings.HasSuffix(name, ".key") || name == strings.TrimSuffix(project, "/") || name == project+rootID {
			t.Fatalf("forbidden access: %v", root.accesses)
		}
	}
}

// R-1CZ2-9206 R-6HUL-S9FT R-1FEV-0LHK R-6J2I-616I R-22KY-A8KR
func TestTreeSubagentEnumerationAndMeta(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"description":"Plan work"}`)
	subLog(m, agentOne, "")
	subMeta(m, agentTwo, `{"description":"Child","parentAgentId":"first-agent"}`)
	subLog(m, agentTwo, "")
	put(m, agentDir+"other.txt", "ignore")
	got := getTree(t, m)
	first, firstOK := nodeByID(got, agentOne)
	second, secondOK := nodeByID(got, agentTwo)
	if len(got.Subagents) != 2 || !firstOK || !secondOK || first.Label != "Plan work" || first.Parent != "" || second.Label != "Child" || second.Parent != agentOne {
		t.Fatalf("subagents: %+v", got.Subagents)
	}
	subMeta(m, agentTwo, `null`)
	got = getTree(t, m)
	second, secondOK = nodeByID(got, agentTwo)
	if !secondOK || second.Label != "" || second.Parent != "" {
		t.Fatalf("unreadable meta: %+v", second)
	}
	for _, bad := range []string{"", ".", "..", agentTwo, "bad/id"} {
		subMeta(m, agentTwo, `{"parentAgentId":"`+bad+`"}`)
		root := &treeObserved{files: m}
		got = getTree(t, root)
		second, secondOK = nodeByID(got, agentTwo)
		if !secondOK || second.Parent != "" {
			t.Fatalf("bad parent %q: %+v", bad, second)
		}
		for _, name := range root.accesses {
			if strings.Contains(name, "bad/") {
				t.Fatalf("bad parent path: %v", root.accesses)
			}
		}
	}
}

func nodeByID(got tree.Tree, id string) (tree.Node, bool) {
	for _, node := range got.Subagents {
		if node.ID == id {
			return node, true
		}
	}
	return tree.Node{}, false
}

// R-6HUL-S9FT R-1FEV-0LHK R-6J2I-616I
func TestTreeUnreadableMetaAndStarterSelection(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"description":"valid","parentAgentId":"second-agent"}`)
	subLog(m, agentOne, "")
	subLog(m, agentTwo, subNotice("failed", "2026-01-01T00:00:01Z"))
	got := getTree(t, m)
	node, found := nodeByID(got, agentOne)
	if !found || node.Parent != agentTwo || node.Status != tree.StatusFailed {
		t.Fatalf("parent starter: %+v", node)
	}
	for _, value := range []string{"null", `[]`, `"scalar"`, `{`} {
		subMeta(m, agentOne, value)
		got = getTree(t, m)
		node, found = nodeByID(got, agentOne)
		if !found || node.Label != "" || node.Parent != "" || node.Status != tree.StatusUnknown {
			t.Fatalf("unreadable meta %q: %+v", value, node)
		}
	}
	subMeta(m, agentOne, `{"description":"ignored"}`)
	root := &treeObserved{files: m, failName: agentDir + "agent-" + agentOne + ".meta.json", failOp: "readfile", failErr: fs.ErrPermission}
	got = getTree(t, root)
	node, found = nodeByID(got, agentOne)
	if !found || node.Label != "" || node.Parent != "" || node.Status != tree.StatusUnknown {
		t.Fatalf("meta read failure: %+v", node)
	}
	subMeta(m, agentOne, `{"parentAgentId":"bad/id"}`)
	got = getTree(t, m)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusUnknown {
		t.Fatalf("bad parent selected starter: %+v", node)
	}
}

// R-1HUN-S4YY R-38TT-R7A8 R-37LX-DFJJ R-3564-LW25
func TestTreeSubagentStartFromRecords(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"description":"one"}`)
	subLog(m, agentOne, strings.Join([]string{
		`{"timestamp":7}`,
		`["2020-01-01T00:00:00Z"]`,
		`{"timestamp":"bad"}`,
		`{"timestamp":"2025-01-01T01:02:03.123456789+01:00"}`,
		`{"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"timestamp":"2030-01-01T00:00:00Z"`,
	}, "\n"))
	got := getTree(t, m)
	want, _ := time.Parse(time.RFC3339Nano, "2025-01-01T01:02:03.123456789+01:00")
	if len(got.Subagents) != 1 || !got.Subagents[0].HasStarted || got.Subagents[0].Started.Compare(want) != 0 {
		t.Fatalf("first valid timestamp: %+v", got.Subagents)
	}
	put(m, rootLog, `{"timestamp":"2020-01-01T00:00:00Z"}`+"\n")
	subMeta(m, agentOne, `{"timestamp":"2021-01-01T00:00:00Z"}`)
	got = getTree(t, m)
	if !got.Subagents[0].HasStarted || got.Subagents[0].Started.Compare(want) != 0 {
		t.Fatalf("other files affected start: %+v", got.Subagents[0])
	}
	root := &treeObserved{files: m, failName: agentDir + "agent-" + agentOne + ".jsonl", failOp: "readat", failErr: fs.ErrPermission}
	got = getTree(t, root)
	if got.Subagents[0].HasStarted {
		t.Fatalf("unreadable own log set start: %+v", got.Subagents[0])
	}
	subLog(m, agentOne, `{"timestamp":"2030-01-01T00:00:00Z"}`)
	got = getTree(t, m)
	if got.Subagents[0].HasStarted {
		t.Fatalf("fragment set start: %+v", got.Subagents[0])
	}
}

func rootNotice(status, stamp string) string {
	return `{"type":"queue-operation","operation":"enqueue","timestamp":"` + stamp + `","content":"<task-notification><task-id>` + agentOne + `</task-id><status>` + status + `</status></task-notification>"}` + "\n"
}

func subNotice(status, stamp string) string {
	return `{"type":"user","origin":{"kind":"task-notification"},"message":{"content":"<task-notification><task-id>` + agentOne + `</task-id><status>` + status + `</status></task-notification>"},"timestamp":"` + stamp + `"}` + "\n"
}

func sendMessage(agent, stamp string) string {
	return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"SendMessage","input":{"to":"` + agent + `"}}]},"timestamp":"` + stamp + `"}` + "\n"
}

// R-1J2K-5WPN R-1KAG-JOGC R-1LIC-XG71 R-1MQ9-B7XQ R-6MQ7-BCEL
func TestTreeNotificationResults(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{}`)
	subLog(m, agentOne, subNotice("failed", "2026-01-01T00:00:01Z"))
	put(m, rootLog, strings.Join([]string{
		`{"type":"queue-operation","operation":"dequeue","content":"<task-notification><task-id>first-agent</task-id><status>killed</status></task-notification>"}`,
		`{"type":"user","content":"<task-notification><task-id>first-agent</task-id><status>failed</status></task-notification>"}`,
		strings.TrimSuffix(rootNotice("completed", "2026-01-01T00:00:02Z"), "\n"),
		strings.TrimSuffix(rootNotice("killed", "2026-01-01T00:00:03Z"), "\n"),
	}, "\n")+"\n")
	got := getTree(t, m)
	if got.Subagents[0].Status != tree.StatusKilled {
		t.Fatalf("last root notification: %+v", got.Subagents[0])
	}
	put(m, rootLog, rootNotice("other", "2026-01-01T00:00:03Z"))
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusUnknown {
		t.Fatalf("other status word: %+v", got.Subagents[0])
	}
	put(m, rootLog, `{"type":"queue-operation","operation":"enqueue","content":"<task-notification><status>completed</status>"}`+"\n")
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusWorking {
		t.Fatalf("notification without agent id: %+v", got.Subagents[0])
	}
}

// R-1J2K-5WPN R-1KAG-JOGC R-1MQ9-B7XQ
func TestTreeNotificationShapesAndNestedStatus(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"parentAgentId":"second-agent"}`)
	subLog(m, agentOne, "")
	subLog(m, agentTwo, `{"type":"attachment","attachment":{"commandMode":"task-notification","prompt":"prefix <task-notification><task-id>first-agent</task-id><status>failed</status><result><status>completed</status></result></task-notification>"}}`+"\n")
	got := getTree(t, m)
	node, found := nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusFailed {
		t.Fatalf("attachment notification: %+v", node)
	}
	subLog(m, agentTwo, subNotice("killed", "2026-01-01T00:00:00Z"))
	got = getTree(t, m)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusKilled {
		t.Fatalf("user notification: %+v", node)
	}
	put(m, rootLog, rootNotice("", "2026-01-01T00:00:01Z"))
	got = getTree(t, m)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusUnknown {
		t.Fatalf("empty status word: %+v", node)
	}
}

// R-6NY3-P45A R-6P60-2VVZ R-6QDW-GNMO R-1WHG-DDVA
func TestTreeStarterAndUnknownStatus(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"requestShape":"foreground","toolUseId":"call-one"}`)
	subLog(m, agentOne, `{"timestamp":"2026-01-01T00:00:00Z","type":"queue-operation","operation":"enqueue","content":"<task-notification><task-id>first-agent</task-id><status>failed</status></task-notification>"}`+"\n")
	put(m, rootLog, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-one"}]}}`+"\n")
	got := getTree(t, m)
	if got.Subagents[0].Status != tree.StatusDone {
		t.Fatalf("foreground tool result: %+v", got.Subagents[0])
	}
	put(m, rootLog, "")
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusWorking {
		t.Fatalf("live with both passes: %+v", got.Subagents[0])
	}
	delete(m, registry+"a.json")
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusUnknown {
		t.Fatalf("ended with both passes: %+v", got.Subagents[0])
	}
	subMeta(m, agentOne, `{"parentAgentId":null}`)
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusUnknown {
		t.Fatalf("no starter: %+v", got.Subagents[0])
	}
	subMeta(m, agentOne, `{}`)
	subLog(m, agentOne, subNotice("killed", "2026-01-01T00:00:01Z"))
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusUnknown {
		t.Fatalf("own transcript changed status: %+v", got.Subagents[0])
	}
	put(m, rootLog, subNotice("killed", "2026-01-01T00:00:01Z"))
	if got = getTree(t, m); got.Subagents[0].Status != tree.StatusUnknown {
		t.Fatalf("root user notification changed status: %+v", got.Subagents[0])
	}
}

// R-1LIC-XG71 R-1MQ9-B7XQ R-6NY3-P45A R-6QDW-GNMO
func TestTreeStarterResultFiltersAndPassError(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"requestShape":"foreground","toolUseId":"call-one"}`)
	subLog(m, agentOne, "")
	put(m, rootLog, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"wrong"}]}}`+"\n")
	got := getTree(t, m)
	if got.Subagents[0].Status != tree.StatusWorking {
		t.Fatalf("wrong tool use id: %+v", got.Subagents[0])
	}
	put(m, rootLog, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-one"}]}}`+"\n")
	subMeta(m, agentOne, `{"requestShape":"background","toolUseId":"call-one"}`)
	got = getTree(t, m)
	if got.Subagents[0].Status != tree.StatusWorking {
		t.Fatalf("background tool result: %+v", got.Subagents[0])
	}
	subMeta(m, agentOne, `{}`)
	put(m, rootLog, "")
	subMeta(m, agentOne, `{"parentAgentId":"second-agent"}`)
	subLog(m, agentTwo, subNotice("completed", "2026-01-01T00:00:01Z")+subNotice("failed", "2026-01-01T00:00:02Z"))
	got = getTree(t, m)
	node, found := nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusFailed {
		t.Fatalf("latest starter result: %+v", node)
	}
	subMeta(m, agentOne, `{"parentAgentId":"second-agent","requestShape":"foreground","toolUseId":"parent-call"}`)
	subLog(m, agentTwo, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"parent-call"}]}}`+"\n")
	got = getTree(t, m)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusDone {
		t.Fatalf("parent starter tool result: %+v", node)
	}
	subLog(m, agentTwo, subNotice("failed", "2026-01-01T00:00:02Z"))
	root := &treeObserved{files: m, failName: agentDir + "agent-" + agentTwo + ".jsonl", failOp: "readat", failErr: fs.ErrPermission}
	got = getTree(t, root)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusUnknown {
		t.Fatalf("starter pass error: %+v", node)
	}
	root = &treeObserved{files: m, failName: rootLog, failOp: "readat", failErr: fs.ErrPermission}
	got = getTree(t, root)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusFailed {
		t.Fatalf("root pass error with starter result: %+v", node)
	}
	subLog(m, agentTwo, "")
	put(m, rootLog, `{"type":"ai-title","aiTitle":"content"}`+"\n")
	got = getTree(t, root)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusUnknown {
		t.Fatalf("root pass error without result: %+v", node)
	}
}

// R-6KAE-JSX7 R-6LIA-XKNW
func TestTreeRemessageAcrossCountedLogs(t *testing.T) {
	m := treeFixture()
	subMeta(m, agentOne, `{"parentAgentId":"second-agent"}`)
	subLog(m, agentOne, "")
	subLog(m, agentTwo, subNotice("completed", "2026-01-01T00:00:04Z")+sendMessage(agentOne, "2026-01-01T00:00:05Z"))
	put(m, rootLog, rootNotice("failed", "2026-01-01T00:00:09Z"))
	got := getTree(t, m)
	node, found := nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusFailed {
		t.Fatalf("later root result wins: %+v", node)
	}
	put(m, rootLog, rootNotice("failed", "2026-01-01T00:00:03Z"))
	got = getTree(t, m)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusWorking {
		t.Fatalf("re-messaged live agent: %+v", node)
	}
	delete(m, registry+"a.json")
	got = getTree(t, m)
	node, found = nodeByID(got, agentOne)
	if !found || node.Status != tree.StatusUnknown {
		t.Fatalf("re-messaged ended agent: %+v", node)
	}
}

// R-1XPC-R5LZ R-21D1-WGU2 R-BC89-8UF3 R-6RLS-UFDD
func TestTreeReadBoundaryAndSingleLogPass(t *testing.T) {
	m := treeFixture()
	put(m, rootLog, `{"type":"ai-title","aiTitle":"title"}`+"\n")
	subMeta(m, agentOne, `{}`)
	subLog(m, agentOne, `{"timestamp":"2026-01-01T00:00:00Z"}`+"\n")
	put(m, project+"secret.txt", "secret")
	put(m, project+rootID+"/secret.txt", "secret")
	put(m, agentDir+"other.log", "secret")
	root := &passFS{files: m, opens: map[string]int{}, reads: map[string]int{}, readAts: map[string]int{}, readDirs: map[string]int{}, stats: map[string]int{}, closes: map[string]int{}, ranges: map[string][][2]int64{}}
	got := getTree(t, root)
	if len(got.Subagents) != 1 {
		t.Fatalf("subagents: %+v", got.Subagents)
	}
	if root.opens[rootLog] != 2 || root.opens[agentDir+"agent-"+agentOne+".jsonl"] != 1 || root.reads[rootLog] != 0 || root.reads[agentDir+"agent-"+agentOne+".jsonl"] != 0 || root.readAts[rootLog] != 1 || root.readAts[agentDir+"agent-"+agentOne+".jsonl"] != 1 {
		t.Fatalf("log passes: opens=%v reads=%v readAts=%v", root.opens, root.reads, root.readAts)
	}
	if root.stats[rootLog] != 2 || root.stats[agentDir+"agent-"+agentOne+".jsonl"] != 1 || root.closes[rootLog] != 2 || root.closes[agentDir+"agent-"+agentOne+".jsonl"] != 1 {
		t.Fatalf("log Stat/Close: stats=%v closes=%v", root.stats, root.closes)
	}
	for _, name := range []string{rootLog, agentDir + "agent-" + agentOne + ".jsonl"} {
		ranges := root.ranges[name]
		if len(ranges) != 1 || ranges[0] != [2]int64{0, int64(len(m[name].Data))} {
			t.Fatalf("incomplete log pass for %s: %v", name, ranges)
		}
		for i, read := range ranges {
			if read[0] < 0 || read[1] > int64(len(m[name].Data)) || read[0] >= read[1] {
				t.Fatalf("bad ReadAt range for %s: %v", name, ranges)
			}
			for _, earlier := range ranges[:i] {
				if read[0] < earlier[1] && earlier[0] < read[1] {
					t.Fatalf("overlapping ReadAt ranges for %s: %v", name, ranges)
				}
			}
		}
	}
	if len(root.writes) != 0 {
		t.Fatalf("mutating methods called: %v", root.writes)
	}
	for name := range root.opens {
		if strings.Contains(name, "secret") || strings.Contains(name, "other.log") {
			t.Fatalf("accessed unrelated file: %v", root.opens)
		}
	}
}

type passFS struct {
	files    fstest.MapFS
	opens    map[string]int
	reads    map[string]int
	readAts  map[string]int
	readDirs map[string]int
	stats    map[string]int
	closes   map[string]int
	ranges   map[string][][2]int64
	writes   []string
	failName string
	failErr  error
}

func (f *passFS) Open(name string) (fs.File, error) {
	f.opens[name]++
	if name == f.failName {
		return nil, f.failErr
	}
	file, err := f.files.Open(name)
	if err != nil {
		return nil, err
	}
	return &passFile{file: file, name: name, parent: f}, nil
}

type passFile struct {
	file   fs.File
	name   string
	parent *passFS
}

func (f *passFile) Read(p []byte) (int, error) {
	f.parent.reads[f.name]++
	return f.file.Read(p)
}

func (f *passFile) ReadAt(p []byte, off int64) (int, error) {
	f.parent.readAts[f.name]++
	f.parent.ranges[f.name] = append(f.parent.ranges[f.name], [2]int64{off, off + int64(len(p))})
	return f.file.(io.ReaderAt).ReadAt(p, off)
}

func (f *passFile) ReadDir(n int) ([]fs.DirEntry, error) {
	f.parent.readDirs[f.name]++
	return f.file.(fs.ReadDirFile).ReadDir(n)
}

func (f *passFile) Stat() (fs.FileInfo, error) {
	f.parent.stats[f.name]++
	return f.file.Stat()
}

func (f *passFile) Close() error {
	f.parent.closes[f.name]++
	return f.file.Close()
}

func (f *passFile) Write(_ []byte) (int, error) {
	f.parent.writes = append(f.parent.writes, "Write:"+f.name)
	return 0, fs.ErrPermission
}

func (f *passFS) WriteFile(name string, _ []byte, _ fs.FileMode) error {
	f.writes = append(f.writes, "WriteFile:"+name)
	return fs.ErrPermission
}

func (f *passFS) Create(name string) (fs.File, error) {
	f.writes = append(f.writes, "Create:"+name)
	return nil, fs.ErrPermission
}

func (f *passFS) OpenFile(name string, _ int, _ fs.FileMode) (fs.File, error) {
	f.writes = append(f.writes, "OpenFile:"+name)
	return nil, fs.ErrPermission
}

func (f *passFS) Mkdir(name string, _ fs.FileMode) error {
	f.writes = append(f.writes, "Mkdir:"+name)
	return fs.ErrPermission
}

func (f *passFS) MkdirAll(name string, _ fs.FileMode) error {
	f.writes = append(f.writes, "MkdirAll:"+name)
	return fs.ErrPermission
}

func (f *passFS) Remove(name string) error {
	f.writes = append(f.writes, "Remove:"+name)
	return fs.ErrPermission
}

func (f *passFS) RemoveAll(name string) error {
	f.writes = append(f.writes, "RemoveAll:"+name)
	return fs.ErrPermission
}

func (f *passFS) Rename(oldName, newName string) error {
	f.writes = append(f.writes, "Rename:"+oldName+":"+newName)
	return fs.ErrPermission
}

func (f *passFS) Chmod(name string, _ fs.FileMode) error {
	f.writes = append(f.writes, "Chmod:"+name)
	return fs.ErrPermission
}

func (f *passFS) Chtimes(name string, _, _ time.Time) error {
	f.writes = append(f.writes, "Chtimes:"+name)
	return fs.ErrPermission
}

func (f *passFS) Symlink(oldName, newName string) error {
	f.writes = append(f.writes, "Symlink:"+oldName+":"+newName)
	return fs.ErrPermission
}

func contains(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}

type treeObserved struct {
	files                                fstest.MapFS
	failName, failOp                     string
	failErr                              error
	accesses, stats                      []string
	dirs, readFiles                      []string
	fileStats, fileCloses                []string
	fileReads, fileReadAts, fileReadDirs []string
}

func (f *treeObserved) Open(name string) (fs.File, error) {
	f.accesses = append(f.accesses, name)
	file, err := f.files.Open(name)
	if err != nil {
		return nil, err
	}
	return &treeObservedFile{file: file, name: name, parent: f}, nil
}

func (f *treeObserved) Stat(name string) (fs.FileInfo, error) {
	f.accesses = append(f.accesses, name)
	f.stats = append(f.stats, name)
	if f.failOp == "stat" && name == f.failName {
		return nil, f.failErr
	}
	return fs.Stat(f.files, name)
}

func (f *treeObserved) ReadDir(name string) ([]fs.DirEntry, error) {
	f.accesses = append(f.accesses, name)
	f.dirs = append(f.dirs, name)
	if f.failOp == "readdir" && name == f.failName {
		return nil, f.failErr
	}
	return f.files.ReadDir(name)
}

func (f *treeObserved) ReadFile(name string) ([]byte, error) {
	f.accesses = append(f.accesses, name)
	f.readFiles = append(f.readFiles, name)
	if f.failOp == "readfile" && name == f.failName {
		return nil, f.failErr
	}
	return f.files.ReadFile(name)
}

func (f *treeObserved) ReadLink(name string) (string, error) {
	f.accesses = append(f.accesses, name)
	return fs.ReadLink(f.files, name)
}

func (f *treeObserved) Lstat(name string) (fs.FileInfo, error) {
	f.accesses = append(f.accesses, name)
	return fs.Lstat(f.files, name)
}

type treeObservedFile struct {
	file   fs.File
	name   string
	parent *treeObserved
}

func (f *treeObservedFile) Read(p []byte) (int, error) {
	f.parent.fileReads = append(f.parent.fileReads, f.name)
	return f.file.Read(p)
}
func (f *treeObservedFile) ReadAt(p []byte, off int64) (int, error) {
	f.parent.fileReadAts = append(f.parent.fileReadAts, f.name)
	if f.parent.failOp == "readat" && f.parent.failName == f.name {
		return 0, f.parent.failErr
	}
	return f.file.(io.ReaderAt).ReadAt(p, off)
}
func (f *treeObservedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	f.parent.fileReadDirs = append(f.parent.fileReadDirs, f.name)
	return f.file.(fs.ReadDirFile).ReadDir(n)
}
func (f *treeObservedFile) Stat() (fs.FileInfo, error) {
	f.parent.fileStats = append(f.parent.fileStats, f.name)
	return f.file.Stat()
}
func (f *treeObservedFile) Close() error {
	f.parent.fileCloses = append(f.parent.fileCloses, f.name)
	return f.file.Close()
}
