package codex

import (
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

const treeDate = "2026/09/23"

func treeFixture() *observedFS {
	f := fixture()
	f.MapFS[sessions+"."] = &fstest.MapFile{Mode: fs.ModeDir}
	return f
}

func (f *observedFS) treeRollout(id, content string) string {
	file := sessions + treeDate + "/rollout-2026-09-23T12-00-00-" + id + ".jsonl"
	f.MapFS[file] = &fstest.MapFile{Data: []byte(content)}
	return file
}

func started(id, path, stamp string) string {
	return `{"type":"event_msg","timestamp":"` + stamp + `","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"` + id + `","agent_path":"` + path + `"}}}` + "\n"
}

func TestTreeLocationAndErrors(t *testing.T) {
	// R-HSS7-IYUS R-HP4I-DNMP R-8J28-VOD3 R-HRKB-5743 R-DKI4-OYFH: signature, invalid id, absent directory and zero tree on error.
	if got, want := reflect.TypeOf(Tree), reflect.TypeOf((func(fs.FS, string, string) (tree.Tree, error))(nil)); got != want {
		t.Fatalf("Tree type = %v, want %v", got, want)
	}
	f := fixture()
	got, err := Tree(f, home, "x")
	if reflect.ValueOf(err) != reflect.ValueOf(tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) || !reflect.DeepEqual(f.calls, []string{"stat:" + strings.TrimSuffix(sessions, "/")}) {
		t.Fatalf("missing location: %#v %v %v", got, err, f.calls)
	}
	for _, id := range []string{"", ".", "..", "a/b"} {
		f = treeFixture()
		got, err = Tree(f, home, id)
		if reflect.ValueOf(err) != reflect.ValueOf(tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
			t.Fatalf("invalid %q: %#v %v", id, got, err)
		}
		for _, call := range f.calls {
			if !strings.HasSuffix(call, ":"+strings.TrimSuffix(sessions, "/")) {
				t.Fatalf("invalid %q accessed %s", id, call)
			}
		}
	}
	// R-33Y8-84BG: a locating error exposes a clean absolute path and the bare filesystem cause.
	f = treeFixture()
	f.fail["dir:"+strings.TrimSuffix(sessions, "/")] = &fs.PathError{Op: "readdir", Path: "x", Err: fs.ErrPermission}
	got, err = Tree(f, home, "x")
	var read *session.ReadError
	if !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(&session.ReadError{}) || !reflect.DeepEqual(got, tree.Tree{}) || read.Path != "/"+strings.TrimSuffix(sessions, "/") || reflect.ValueOf(read.Err) != reflect.ValueOf(fs.ErrPermission) {
		t.Fatalf("unreadable location: %#v %#v", got, err)
	}
}

func TestTreeRootLivenessAndFields(t *testing.T) {
	// R-4117-ZUHJ R-44OX-55PM R-837J-WNQ2 R-85NC-O77G R-86V9-1YY5: exact paths, root admission and zero start fields.
	// R-8835-FQOU R-89B1-TIFJ R-HU03-WQLH R-HV80-AIC6: index title and root status depend on their own sources.
	f := treeFixture()
	f.lock("root", 17, 321)
	f.treeRollout("root", `{"type":"session_meta"}`+"\n"+`{"type":"event_msg","payload":{"type":"task_started"}}`+"\n")
	f.MapFS[index] = &fstest.MapFile{Data: []byte(`{"id":"root","thread_name":"old"}` + "\n" + `{"id":"root","thread_name":"O'Reilly"}` + "\n" + `{"id":"root","thread_name":"fragment"}`)}
	got, err := Tree(f, home, "root")
	if err != nil || got.Root.ID != "root" || got.Root.Parent != "" || got.Root.HasStarted || !got.Root.Started.IsZero() || got.Root.Label != "O'Reilly" || got.Root.Status != tree.StatusWorking {
		t.Fatalf("live root: %#v %v", got, err)
	}
	if !has(f.calls, "stat:"+lockDir+"/root.lock") || !has(f.calls, "read:proc/locks") {
		t.Fatalf("liveness paths: %v", f.calls)
	}
	for _, call := range f.calls {
		if call == "open:"+lockDir+"/root.lock" || call == "read:"+lockDir+"/root.lock" || call == "dir:"+lockDir {
			t.Fatalf("lock inspected beyond stat: %s", call)
		}
	}
	delete(f.MapFS, lockDir+"/root.lock")
	got, err = Tree(f, home, "root")
	if err != nil || got.Root.Status != tree.StatusEnded || got.Root.Label != "O'Reilly" {
		t.Fatalf("ended root: %#v %v", got, err)
	}
	f.MapFS[lockDir+"/root.lock"] = &fstest.MapFile{}
	got, err = Tree(f, home, "root")
	if err != nil || got.Root.Status != tree.StatusUnknown {
		t.Fatalf("unknown liveness: %#v %v", got, err)
	}
	f.MapFS[lockDir+"/root.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 17}}
	f.treeRollout("sub", `{"type":"session_meta","payload":{"source":{"subagent":{"thread_spawn":{"parent_thread_id":"root"}}}}}`+"\n")
	f.lock("sub", 18, 322)
	got, err = Tree(f, home, "sub")
	if reflect.ValueOf(err) != reflect.ValueOf(tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("subagent as root: %#v %v", got, err)
	}
	// A held lock finds a root before its rollout is written.
	got, err = Tree(f, home, "early")
	if reflect.ValueOf(err) != reflect.ValueOf(tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("absent root: %#v %v", got, err)
	}
	f.lock("early", 19, 323)
	got, err = Tree(f, home, "early")
	if err != nil || got.Root.Status != tree.StatusUnknown || got.Root.ID != "early" {
		t.Fatalf("live without rollout: %#v %v", got, err)
	}
}

func TestTreeRolloutMatchAndSubagents(t *testing.T) {
	// R-4294-DM88: strict stamp and whole id match, with the bytewise-first rollout winning.
	// R-45WT-IXGB R-8CYQ-YTNM R-HQCE-RFDE: started items alone form a recursively closed set; parent rules handle duplicate namers.
	// R-8FEJ-QD50 R-8GMG-44VP R-37LX-DFJJ R-38TT-R7A8: first naming item gives label and parsed start time.
	f := treeFixture()
	f.treeRollout("root", `{"type":"session_meta"}`+"\n"+
		started("a", "/root/forecast/radar", "2026-09-23T12:01:00Z")+
		started("a", "/wrong", "2026-09-23T12:02:00Z")+
		started("b", "/root/", "bad")+
		started("root", "/self", "2026-09-23T12:03:00Z")+
		`{"type":"event_msg","payload":{"thread_id":"copied","item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"copied","agent_path":"/copied"}}}`+"\n"+
		`{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"completed","agent_thread_id":"completed"}}}`+"\n")
	f.treeRollout("a", `{"type":"session_meta"}`+"\n"+started("c", "/child/c", "2026-09-23T12:04:00Z")+started("shared", "/from/a", "2026-09-23T12:04:00Z"))
	f.treeRollout("b", `{"type":"session_meta"}`+"\n"+started("shared", "/from/b", "2026-09-23T12:05:00Z"))
	f.treeRollout("shared", `{"type":"session_meta"}`+"\n"+started("a", "/cycle", "2026-09-23T12:06:00Z"))
	f.MapFS[sessions+treeDate+"/rollout-wrong-root.jsonl"] = &fstest.MapFile{Data: []byte(started("wrong", "/wrong", "2026-09-23T12:00:00Z"))}
	f.MapFS[sessions+treeDate+"/rollout-2026-09-23T11-00-00-superroot.jsonl"] = &fstest.MapFile{Data: []byte(started("suffix", "/suffix", "2026-09-23T12:00:00Z"))}
	f.MapFS[sessions+treeDate+"/rollout-2026-09-23T13-00-00-root.jsonl"] = &fstest.MapFile{Data: []byte(started("later", "/later", "2026-09-23T13:00:00Z"))}
	got, err := Tree(f, home, "root")
	if err != nil || len(got.Subagents) != 4 {
		t.Fatalf("children: %#v %v", got, err)
	}
	byID := map[string]tree.Node{}
	for _, child := range got.Subagents {
		byID[child.ID] = child
	}
	want := time.Date(2026, 9, 23, 12, 1, 0, 0, time.UTC)
	if byID["a"].Parent != "" || byID["a"].Label != "radar" || !byID["a"].HasStarted || byID["a"].Started.Compare(want) != 0 || byID["b"].Label != "" || byID["b"].HasStarted || byID["c"].Parent != "a" || byID["shared"].Parent != "" || byID["shared"].Label != "a" {
		t.Fatalf("child fields: %#v", byID)
	}
}

func TestTreeMetaAndUnreadableRollout(t *testing.T) {
	// R-43H0-RDYX R-837J-WNQ2: only first-record meta identifies a subagent; an unreadable rollout has no records.
	f := treeFixture()
	f.treeRollout("late-meta", `{"type":"event_msg"}`+"\n"+`{"type":"session_meta","payload":{"parent_thread_id":"root"}}`+"\n")
	got, err := Tree(f, home, "late-meta")
	if err != nil || got.Root.ID != "late-meta" || got.Root.Status != tree.StatusEnded {
		t.Fatalf("late meta: %#v %v", got, err)
	}
	f.treeRollout("direct", `{"type":"session_meta","payload":{"parent_thread_id":"root"}}`+"\n")
	got, err = Tree(f, home, "direct")
	if reflect.ValueOf(err) != reflect.ValueOf(tree.ErrNotFound) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("direct parent: %#v %v", got, err)
	}
	file := f.treeRollout("unreadable", `{"type":"session_meta"}`+"\n")
	f.fail["open:"+file] = fs.ErrPermission
	got, err = Tree(f, home, "unreadable")
	if err != nil || got.Root.ID != "unreadable" || got.Root.Status != tree.StatusEnded {
		t.Fatalf("unreadable rollout still found: %#v %v", got, err)
	}
}

func TestTreeStatusFallbacks(t *testing.T) {
	// R-HWFW-OA2V R-HXNT-21TK R-HYVP-FTK9: final child status record wins; unfinished work needs a live root.
	f := treeFixture()
	f.lock("root", 17, 321)
	f.treeRollout("root", `{"type":"session_meta"}`+"\n"+started("working", "/working", "2026-09-23T12:01:00Z")+started("done", "/done", "2026-09-23T12:01:00Z")+started("killed", "/killed", "2026-09-23T12:01:00Z")+started("missing", "/missing", "2026-09-23T12:01:00Z")+started("no-event", "/no-event", "2026-09-23T12:01:00Z"))
	for id, kind := range map[string]string{"working": "task_started", "done": "task_complete", "killed": "turn_aborted"} {
		f.treeRollout(id, `{"type":"session_meta"}`+"\n"+`{"type":"event_msg","payload":{"type":"`+kind+`"}}`+"\n")
	}
	f.treeRollout("no-event", `{"type":"session_meta"}`+"\n")
	check := func(live bool) {
		t.Helper()
		got, err := Tree(f, home, "root")
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]tree.Status{}
		for _, child := range got.Subagents {
			byID[child.ID] = child.Status
		}
		working := tree.StatusUnknown
		if live {
			working = tree.StatusWorking
		}
		if byID["working"] != working || byID["done"] != tree.StatusDone || byID["killed"] != tree.StatusKilled || byID["missing"] != tree.StatusUnknown || byID["no-event"] != tree.StatusUnknown {
			t.Fatalf("statuses: %#v", byID)
		}
	}
	check(true)
	delete(f.MapFS, lockDir+"/root.lock")
	check(false)
}

func TestTreeLiveRootStatusRecords(t *testing.T) {
	// R-89B1-TIFJ: a usable live rollout starts idle and its last qualifying event determines status.
	f := treeFixture()
	f.lock("root", 17, 321)
	file := f.treeRollout("root", `{"type":"session_meta"}`+"\n")
	for _, tc := range []struct {
		appendLine string
		want       tree.Status
	}{
		{"", tree.StatusIdle},
		{`{"type":"event_msg","payload":{"type":"task_started"}}` + "\n", tree.StatusWorking},
		{`{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n", tree.StatusIdle},
		{`{"type":"event_msg","payload":{"type":"turn_aborted"}}` + "\n", tree.StatusIdle},
	} {
		f.MapFS[file].Data = append(f.MapFS[file].Data, []byte(tc.appendLine)...)
		got, err := Tree(f, home, "root")
		if err != nil || got.Root.Status != tc.want {
			t.Fatalf("root status %q: %#v %v", tc.appendLine, got, err)
		}
	}
}

type treeOpenOnly struct {
	files  fstest.MapFS
	opened map[string]int
	reads  map[string]int
	readAt map[string]int
	stats  map[string]int
	dirs   map[string]int
	closes map[string]int
	ranges map[string][][2]int64
}

func (f *treeOpenOnly) Open(name string) (fs.File, error) {
	f.opened[name]++
	file, err := f.files.Open(name)
	if err != nil {
		return nil, err
	}
	return &treeRecordedFile{File: file, name: name, owner: f}, nil
}

type treeRecordedFile struct {
	fs.File
	name  string
	owner *treeOpenOnly
}

func (f *treeRecordedFile) Read(p []byte) (int, error) {
	f.owner.reads[f.name]++
	return f.File.Read(p)
}

func (f *treeRecordedFile) ReadAt(p []byte, offset int64) (int, error) {
	f.owner.readAt[f.name]++
	f.owner.ranges[f.name] = append(f.owner.ranges[f.name], [2]int64{offset, offset + int64(len(p))})
	return f.File.(io.ReaderAt).ReadAt(p, offset)
}

func (f *treeRecordedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	f.owner.dirs[f.name]++
	return f.File.(fs.ReadDirFile).ReadDir(n)
}

func (f *treeRecordedFile) Stat() (fs.FileInfo, error) {
	f.owner.stats[f.name]++
	return f.File.Stat()
}

func (f *treeRecordedFile) Close() error {
	f.owner.closes[f.name]++
	return f.File.Close()
}

func TestTreeLogAccessAndScope(t *testing.T) {
	// R-43H0-RDYX R-3564-LW25 R-BC89-8UF3: each relevant JSONL log gets one session.Log pass and no Read.
	// R-8KA5-9G3S R-8LI1-N7UH R-8NXU-ERBV: only allowed paths and methods are used; lock file is stat-only.
	f := &treeOpenOnly{files: fstest.MapFS{}, opened: map[string]int{}, reads: map[string]int{}, readAt: map[string]int{}, stats: map[string]int{}, dirs: map[string]int{}, closes: map[string]int{}, ranges: map[string][][2]int64{}}
	f.files[sessions+treeDate+"/rollout-2026-09-23T12-00-00-root.jsonl"] = &fstest.MapFile{Data: []byte(`{"type":"session_meta"}` + "\n" + started("child", "/child", "2026-09-23T12:01:00Z") + `{"type":"event_msg","payload":{"type":"task_started"}}` + "\n" + `{"type":"event_msg","payload":{"type":"task_complete"}}`)}
	f.files[sessions+treeDate+"/rollout-2026-09-23T12-00-00-child.jsonl"] = &fstest.MapFile{Data: []byte(`{"type":"session_meta"}` + "\n")}
	decoy := sessions + treeDate + "/rollout-2026-09-23T12-00-00-other.jsonl"
	f.files[decoy] = &fstest.MapFile{Data: []byte(started("wrong", "/wrong", "2026-09-23T12:01:00Z"))}
	f.files[index] = &fstest.MapFile{Data: []byte(`{"id":"root","thread_name":"title"}` + "\n")}
	got, err := Tree(f, home, "root")
	if err != nil || got.Root.Label != "title" || got.Root.Status != tree.StatusEnded || len(got.Subagents) != 1 {
		t.Fatalf("tree: %#v %v", got, err)
	}
	for _, file := range []string{sessions + treeDate + "/rollout-2026-09-23T12-00-00-root.jsonl", sessions + treeDate + "/rollout-2026-09-23T12-00-00-child.jsonl", index} {
		if f.opened[file] != 1 || f.reads[file] != 0 || f.readAt[file] == 0 {
			t.Fatalf("log access %s: opens %d reads %d readAt %d", file, f.opened[file], f.reads[file], f.readAt[file])
		}
		for i, first := range f.ranges[file] {
			if first[0] < 0 || first[0] >= first[1] || first[1] > int64(len(f.files[file].Data)) {
				t.Fatalf("invalid ReadAt range %s: %v", file, first)
			}
			for _, second := range f.ranges[file][i+1:] {
				if first[0] < second[1] && second[0] < first[1] {
					t.Fatalf("overlapping ReadAt ranges %s: %v %v", file, first, second)
				}
			}
		}
	}
	if f.opened[lockDir+"/root.lock"] != 1 || f.reads[lockDir+"/root.lock"] != 0 {
		t.Fatalf("lock access: %#v %#v", f.opened, f.reads)
	}
	if f.opened[decoy] != 0 {
		t.Fatalf("unrelated rollout read: %#v", f.opened)
	}
	allowed := map[string]bool{
		strings.TrimSuffix(sessions, "/"): true,
		sessions + "2026":                 true,
		sessions + "2026/09":              true,
		sessions + treeDate:               true,
		sessions + treeDate + "/rollout-2026-09-23T12-00-00-root.jsonl":  true,
		sessions + treeDate + "/rollout-2026-09-23T12-00-00-child.jsonl": true,
		index:                  true,
		lockDir + "/root.lock": true,
	}
	for opened := range f.opened {
		if !allowed[opened] {
			t.Fatalf("outside Tree read scope: %s", opened)
		}
	}
}

func TestTreeReadErrorType(t *testing.T) {
	// R-DKI4-OYFH R-33Y8-84BG: only the three specified error forms escape.
	f := treeFixture()
	f.fail["stat:"+strings.TrimSuffix(sessions, "/")] = fs.ErrPermission
	_, err := Tree(f, home, "x")
	var read *session.ReadError
	if !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(read) || read.Path != "/"+strings.TrimSuffix(sessions, "/") {
		t.Fatalf("error type: %#v", err)
	}
}

func TestTreeRecordAndItemFallbacks(t *testing.T) {
	// R-3564-LW25: malformed and incomplete task_complete lines cannot change the live root's working status.
	// R-45WT-IXGB: equal and non-string thread_id values name children, while a different string names none.
	// R-8FEJ-QD50: missing and non-string agent_path values leave child labels empty.
	// R-37LX-DFJJ R-38TT-R7A8: absent and non-string timestamps leave starts unknown.
	f := treeFixture()
	f.lock("root", 17, 321)
	f.treeRollout("root", `{"type":"session_meta"}`+"\n"+
		`{"type":"event_msg","payload":{"type":"task_started"}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"task_complete"}`+"\n"+
		`{"type":"event_msg","payload":{"thread_id":"root","item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"equal"}}}`+"\n"+
		`{"type":"event_msg","timestamp":42,"payload":{"thread_id":42,"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"non-string","agent_path":42}}}`+"\n"+
		`{"type":"event_msg","payload":{"thread_id":"other","item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"copied"}}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"task_complete"}}`)
	got, err := Tree(f, home, "root")
	if err != nil || got.Root.Status != tree.StatusWorking || len(got.Subagents) != 2 {
		t.Fatalf("records and thread ids: %#v %v", got, err)
	}
	for _, child := range got.Subagents {
		if child.Label != "" || child.HasStarted {
			t.Fatalf("fallback fields: %#v", child)
		}
	}
}

func TestTreeLivenessFallbacks(t *testing.T) {
	// R-85NC-O77G R-HU03-WQLH R-HV80-AIC6: an unheld known lock is ended; unreadable /proc/locks makes liveness unknown.
	f := treeFixture()
	f.MapFS[lockDir+"/root.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 17}}
	f.MapFS["proc/locks"] = &fstest.MapFile{}
	f.treeRollout("root", `{"type":"session_meta"}`+"\n")
	got, err := Tree(f, home, "root")
	if err != nil || got.Root.Status != tree.StatusEnded {
		t.Fatalf("unheld lock: %#v %v", got, err)
	}
	f.fail["read:proc/locks"] = fs.ErrPermission
	got, err = Tree(f, home, "root")
	if err != nil || got.Root.Status != tree.StatusUnknown {
		t.Fatalf("unreadable locks: %#v %v", got, err)
	}
}

func TestTreeIndexTitleFallbacks(t *testing.T) {
	// R-8835-FQOU: the last matching record clears an earlier title when the field is absent or non-string.
	f := treeFixture()
	f.treeRollout("root", `{"type":"session_meta"}`+"\n")
	for _, last := range []string{`{"id":"root"}`, `{"id":"root","thread_name":42}`} {
		f.MapFS[index] = &fstest.MapFile{Data: []byte(`{"id":"root","thread_name":"old"}` + "\n" + last + "\n")}
		got, err := Tree(f, home, "root")
		if err != nil || got.Root.Label != "" {
			t.Fatalf("last title %s: %#v %v", last, got, err)
		}
	}
	delete(f.MapFS, index)
	got, err := Tree(f, home, "root")
	if err != nil || got.Root.Label != "" {
		t.Fatalf("missing index: %#v %v", got, err)
	}
	f.MapFS[index] = &fstest.MapFile{Data: []byte(`{"id":"root","thread_name":"old"}` + "\n")}
	f.fail["open:"+index] = fs.ErrPermission
	got, err = Tree(f, home, "root")
	if err != nil || got.Root.Label != "" {
		t.Fatalf("unreadable index: %#v %v", got, err)
	}
}
