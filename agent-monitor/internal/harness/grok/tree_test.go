package grok_test

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type treeReadTrace struct {
	files                   fstest.MapFS
	opens, reads, readAts   map[string]int
	stats, readDirs, closes map[string]int
	ranges                  map[string][]readRange
}

type readRange struct {
	off int64
	len int
}

func (tr *treeReadTrace) Open(name string) (fs.File, error) {
	tr.opens[name]++
	f, err := tr.files.Open(name)
	if err != nil {
		return nil, err
	}
	return &treeTracedFile{File: f, trace: tr, name: name}, nil
}

type treeTracedFile struct {
	fs.File
	trace *treeReadTrace
	name  string
}

func (f *treeTracedFile) Read(p []byte) (int, error) {
	f.trace.reads[f.name]++
	return f.File.Read(p)
}

func (f *treeTracedFile) ReadAt(p []byte, off int64) (int, error) {
	f.trace.readAts[f.name]++
	f.trace.ranges[f.name] = append(f.trace.ranges[f.name], readRange{off: off, len: len(p)})
	return f.File.(io.ReaderAt).ReadAt(p, off)
}

func (f *treeTracedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	f.trace.readDirs[f.name]++
	return f.File.(fs.ReadDirFile).ReadDir(n)
}

func (f *treeTracedFile) Stat() (fs.FileInfo, error) {
	f.trace.stats[f.name]++
	return f.File.Stat()
}

func (f *treeTracedFile) Close() error {
	f.trace.closes[f.name]++
	return f.File.Close()
}

const sessions = base + "sessions/"

// R-P1AC-UJHA
var _ func(fs.FS, string, string) (tree.Tree, error) = grok.Tree

func treeRoot(index string) fstest.MapFS {
	r := root(index)
	r[sessions+"x/r/summary.json"] = file(`{"generated_title":"Root"}`)
	return r
}

func mustTree(t *testing.T, r fs.FS) tree.Tree {
	t.Helper()
	got, err := grok.Tree(r, "/home/dev", "r")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func exactNotFound(err error) bool {
	return err != nil && reflect.TypeOf(err) == reflect.TypeOf(tree.ErrNotFound) && reflect.ValueOf(err).Pointer() == reflect.ValueOf(tree.ErrNotFound).Pointer()
}

// R-DKI4-OYFH R-33Y8-84BG R-OYUK-2ZZW R-OXMN-P897 R-TZRH-OWY8
func TestTreeLocationAndErrors(t *testing.T) {
	locating := "/home/dev/.grok/sessions"
	for _, tc := range []struct {
		fail map[string]error
		want error
	}{
		{map[string]error{strings.TrimPrefix(locating, "/"): fs.ErrPermission}, fs.ErrPermission},
		{map[string]error{}, nil},
	} {
		o := &observed{files: treeRoot("[]"), fail: tc.fail}
		got, err := grok.Tree(o, "/home/dev", "bad/id")
		if !reflect.DeepEqual(got, tree.Tree{}) {
			t.Fatalf("error returned nonzero tree: %+v", got)
		}
		if tc.want == nil {
			if !exactNotFound(err) {
				t.Fatalf("invalid id: %v", err)
			}
		} else {
			var re *session.ReadError
			var pe *fs.PathError
			if !errors.As(err, &re) || reflect.TypeOf(err) != reflect.TypeOf(&session.ReadError{}) || re.Path != locating || !errors.Is(re.Err, tc.want) || errors.As(re.Err, &pe) || !path.IsAbs(re.Path) || path.Clean(re.Path) != re.Path {
				t.Fatalf("location error: %#v", err)
			}
		}
		for _, name := range append(append(append(append(append([]string{}, o.opened...), o.listed...), o.statted...), o.readFiles...), o.readLinked...) {
			if name != strings.TrimPrefix(locating, "/") {
				t.Fatalf("invalid id accessed %q", name)
			}
		}
	}
	for _, id := range []string{"", ".", "..", "a/b"} {
		_, err := grok.Tree(treeRoot("[]"), "/home/dev", id)
		if !exactNotFound(err) {
			t.Errorf("%q: %v", id, err)
		}
	}
	if got, err := grok.Tree(fstest.MapFS{}, "/home/dev", "r"); !exactNotFound(err) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("missing location: %+v %v", got, err)
	}
}

// R-TYJL-B57J R-P4Y1-ZUPD R-P02G-GRQL R-8ZUD-GIUS R-U27A-GGFM R-U3F6-U86B
func TestTreeRootDiscoveryAndStatus(t *testing.T) {
	liveIndex := `[{"session_id":"r","pid":42,"opened_at":"1970-01-01T00:16:42Z"}]`
	for _, tc := range []struct {
		index, kind, events string
		want                tree.Status
		found               bool
	}{
		{liveIndex, "headless", "{\"type\":\"turn_started\"}\n", tree.StatusWorking, true},
		{liveIndex, "", "{\"type\":\"turn_ended\"}\n", tree.StatusIdle, true},
		{liveIndex, "", "", tree.StatusIdle, true},
		{liveIndex, "", "missing", tree.StatusUnknown, true},
		{"[]", "headless", "", tree.StatusEnded, true},
		{"[]", "subagent", "", "", false},
		{"[]", "subagent_resume", "", "", false},
		{"[]", "other", "", tree.StatusEnded, true},
		{"not json", "", "", tree.StatusUnknown, true},
	} {
		r := treeRoot(tc.index)
		r[sessions+"x/r/summary.json"] = file(`{"generated_title":"Root","session_kind":"` + tc.kind + `"}`)
		if tc.events != "missing" {
			r[sessions+"x/r/events.jsonl"] = file(tc.events)
		}
		got, err := grok.Tree(r, "/home/dev", "r")
		if !tc.found {
			if !exactNotFound(err) || !reflect.DeepEqual(got, tree.Tree{}) {
				t.Errorf("kind %q: %+v %v", tc.kind, got, err)
			}
			continue
		}
		if err != nil || got.Root != (tree.Node{ID: "r", Label: "Root", Status: tc.want}) {
			t.Errorf("index %q kind %q events %q: %+v %v", tc.index, tc.kind, tc.events, got, err)
		}
	}
	r := treeRoot(liveIndex)
	delete(r, sessions+"x/r/summary.json")
	r[sessions+"x/other/dummy"] = file("x")
	got := mustTree(t, r)
	if got.Root.ID != "r" || got.Root.Label != "" || got.Root.Status != tree.StatusUnknown {
		t.Fatalf("index-only root = %+v", got.Root)
	}
}

// R-P4Y1-ZUPD R-U27A-GGFM R-U3F6-U86B
func TestTreeIndexLivenessAndDirectoryPriority(t *testing.T) {
	for _, tc := range []struct {
		index string
		want  tree.Status
	}{
		{`[{"session_id":"r","pid":42,"opened_at":"1970-01-01T00:16:41Z"}]`, tree.StatusEnded},
		{`[{"session_id":"r","pid":42,"opened_at":"bad"}]`, tree.StatusUnknown},
		{`[{"session_id":"r","pid":99}]`, tree.StatusEnded},
		{`[{"session_id":"r","pid":42},null]`, tree.StatusUnknown},
	} {
		r := treeRoot(tc.index)
		got := mustTree(t, r)
		if got.Root.Status != tc.want {
			t.Errorf("index %q: status %s, want %s", tc.index, got.Root.Status, tc.want)
		}
	}
	r := treeRoot("[]")
	r[sessions+"a/r"] = file("not a directory")
	if got, err := grok.Tree(r, "/home/dev", "r"); !exactNotFound(err) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("first matching entry is not a directory: %+v %v", got, err)
	}
	r = treeRoot("[]")
	r[sessions+"x/r/summary.json"] = file(`{"session_kind":"subagent_resume"}`)
	if got, err := grok.Tree(r, "/home/dev", "r"); !exactNotFound(err) || !reflect.DeepEqual(got, tree.Tree{}) {
		t.Fatalf("subagent session: %+v %v", got, err)
	}
}

// R-P3Q5-M2YO R-P4Y1-ZUPD R-P02G-GRQL R-U9IO-R2VS
func TestTreeWholeJSONValues(t *testing.T) {
	r := treeRoot(`[{"session_id":"r","pid":42}] trailing`)
	r[sessions+"x/r/summary.json"] = file(`{"generated_title":"ignored"} trailing`)
	r[sessions+"x/r/subagents/a/meta.json"] = file(`{"description":"ignored","status":"completed"} trailing`)
	r[sessions+"x/r/events.jsonl"] = file("{\"type\":\"turn_ended\"}\n")
	got := mustTree(t, r)
	if got.Root.Label != "" || got.Root.Status != tree.StatusUnknown || len(got.Subagents) != 1 || got.Subagents[0].Label != "" || got.Subagents[0].Status != tree.StatusUnknown {
		t.Fatalf("trailing JSON accepted: %+v", got)
	}
	r[base+"active_sessions.json"] = file(`[{"session_id":"r","pid":42},null]`)
	got = mustTree(t, r)
	if got.Root.Status != tree.StatusUnknown {
		t.Fatalf("non-object index element accepted: %+v", got)
	}
}

// R-U8AS-DB53 R-U9IO-R2VS R-UAQL-4UMH R-O2GJ-U46Q R-UEEA-A5UK
func TestTreeSubagentMeta(t *testing.T) {
	r := treeRoot(`[{"session_id":"r","pid":42}]`)
	r[sessions+"x/r/subagents/a/meta.json"] = file(`{"description":"A \u263a","started_at":"2026-01-02T03:04:05.1Z","status":"running"}`)
	r[sessions+"x/r/subagents/b/meta.json"] = file(`{"description":4,"started_at":"bad","status":"completed"}`)
	r[sessions+"x/r/subagents/c/meta.json"] = file(`{"status":"failed"}`)
	r[sessions+"x/r/subagents/d/meta.json"] = file(`{"status":"cancelled"}`)
	r[sessions+"x/r/subagents/e/meta.json"] = file(`[]`)
	r[sessions+"x/r/subagents/f/dummy"] = file("x")
	r[sessions+"x/r/subagents/g"] = file("not a directory")
	r[sessions+"x/r/subagents/h/meta.json"] = file(`{"status":"paused"}`)
	r[sessions+"x/r/subagents/i/meta.json"] = file(`{"status":7}`)
	got := mustTree(t, r)
	byID := make(map[string]tree.Node)
	for _, n := range got.Subagents {
		byID[n.ID] = n
	}
	if len(got.Subagents) != 9 || len(byID) != 9 || byID["a"].Label != "A ☺" || byID["a"].Status != tree.StatusWorking || byID["b"].Label != "" || byID["b"].Status != tree.StatusDone || byID["c"].Status != tree.StatusFailed || byID["d"].Status != tree.StatusKilled || byID["e"].Status != tree.StatusUnknown || byID["f"].Status != tree.StatusUnknown || byID["g"].Status != tree.StatusUnknown || byID["h"].Status != tree.StatusUnknown || byID["i"].Status != tree.StatusUnknown {
		t.Fatalf("subagents: %+v", got.Subagents)
	}
	when, _ := time.Parse(time.RFC3339Nano, "2026-01-02T03:04:05.1Z")
	if !byID["a"].HasStarted || byID["a"].Started.Compare(when) != 0 || byID["b"].HasStarted {
		t.Fatalf("starts: %+v %+v", byID["a"], byID["b"])
	}
	r[base+"active_sessions.json"] = file("[]")
	got = mustTree(t, r)
	for _, n := range got.Subagents {
		if n.ID == "a" && n.Status != tree.StatusUnknown {
			t.Fatalf("ended running subagent: %+v", n)
		}
	}
	r[base+"active_sessions.json"] = file("not-json")
	got = mustTree(t, r)
	for _, n := range got.Subagents {
		if n.ID == "a" && n.Status != tree.StatusUnknown {
			t.Fatalf("indeterminate running subagent: %+v", n)
		}
	}
}

// R-UD6D-WE3V R-UFM6-NXL9 R-W2G6-CN1A R-3564-LW25
func TestTreeSpawnParents(t *testing.T) {
	r := treeRoot("[]")
	for _, id := range []string{"a", "b", "c", "d"} {
		r[sessions+"x/r/subagents/"+id+"/meta.json"] = file(`{"parent_session_id":"wrong"}`)
	}
	r[sessions+"x/r/updates.jsonl"] = file(strings.Join([]string{
		`{"params":{"_meta":{"promptId":"root-prompt"}}}`,
		`{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"a","child_session_id":"a-session","parent_prompt_id":"root-prompt"}}}`,
		`{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"b","child_session_id":"b-session","parent_prompt_id":"root-prompt"}}}`,
		`{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"c","parent_prompt_id":"deep-prompt"}}}`,
		`{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"d","parent_prompt_id":"missing"}}}`,
	}, "\n") + "\n{\"params\":")
	r[sessions+"x/a-session/updates.jsonl"] = file("{\"params\":{\"_meta\":{\"promptId\":\"deep-prompt\"}}}\n")
	r[sessions+"x/b-session/updates.jsonl"] = file("{\"params\":{\"_meta\":{\"promptId\":\"other\"}}}\n")
	got := mustTree(t, r)
	parents := map[string]string{}
	for _, n := range got.Subagents {
		parents[n.ID] = n.Parent
	}
	if !reflect.DeepEqual(parents, map[string]string{"a": "", "b": "", "c": "a", "d": ""}) {
		t.Fatalf("parents = %v", parents)
	}
	r[sessions+"x/b-session/updates.jsonl"] = file("{\"params\":{\"_meta\":{\"promptId\":\"deep-prompt\"}}}\n")
	got = mustTree(t, r)
	for _, n := range got.Subagents {
		if n.ID == "c" && n.Parent != "" {
			t.Fatalf("ambiguous parent = %+v", n)
		}
	}
	// The last spawn supersedes the earlier child-session field, even if absent.
	r[sessions+"x/r/updates.jsonl"] = file(strings.TrimSuffix(string(r[sessions+"x/r/updates.jsonl"].Data), "{\"params\":") + `{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"a","parent_prompt_id":"root-prompt"}}}` + "\n")
	r[sessions+"x/b-session/updates.jsonl"] = file("{\"params\":{\"_meta\":{\"promptId\":\"other\"}}}\n")
	got = mustTree(t, r)
	for _, n := range got.Subagents {
		if n.ID == "c" && n.Parent != "" {
			t.Fatalf("superseded child session still used: %+v", n)
		}
	}
	r[sessions+"x/r/updates.jsonl"] = file(string(r[sessions+"x/r/updates.jsonl"].Data) + `{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"a","child_session_id":"a-session"}}}` + "\n")
	got = mustTree(t, r)
	for _, n := range got.Subagents {
		if n.ID == "c" && n.Parent != "a" {
			t.Fatalf("restored child session not used: %+v", n)
		}
	}
	r[sessions+"x/r/updates.jsonl"] = file(string(r[sessions+"x/r/updates.jsonl"].Data) + `{"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"c","child_session_id":"c-session"}}}` + "\n")
	got = mustTree(t, r)
	for _, n := range got.Subagents {
		if n.ID == "c" && n.Parent != "" {
			t.Fatalf("superseded spawn prompt still used: %+v", n)
		}
	}
}

// R-P2I9-8B7Z R-BC89-8UF3 R-P3Q5-M2YO R-W4VZ-46IO R-W63V-HY9D
func TestTreeReadBoundaries(t *testing.T) {
	r := treeRoot(`[{"session_id":"r","pid":42}]`)
	r[sessions+"x/r/events.jsonl"] = file("{\"type\":\"turn_started\"}\n")
	r[sessions+"x/r/updates.jsonl"] = file("{\"params\":{\"_meta\":{\"promptId\":\"p\"}}}\n{\"params\":{\"update\":{\"sessionUpdate\":\"subagent_spawned\",\"subagent_id\":\"a\",\"child_session_id\":\"r\"}}}\n{\"params\":{\"update\":{\"sessionUpdate\":\"subagent_spawned\",\"subagent_id\":\"b\",\"child_session_id\":\"child\"}}}\n")
	r[sessions+"x/r/subagents/a/meta.json"] = file(`{"status":"completed"}`)
	r[sessions+"x/r/subagents/b/meta.json"] = file(`{"status":"completed"}`)
	r[sessions+"x/child/updates.jsonl"] = file("{\"params\":{\"_meta\":{\"promptId\":\"child-prompt\"}}}\n")
	r[sessions+"x/r/subagents/a/secret.lock"] = file("secret")
	o := &observed{files: r, fail: map[string]error{}}
	got := mustTree(t, o)
	if len(got.Subagents) != 2 || got.Root.Status != tree.StatusWorking {
		t.Fatalf("tree = %+v", got)
	}
	allowedLists := map[string]bool{strings.TrimSuffix(sessions, "/"): true, sessions + "x": true, sessions + "x/r/subagents": true}
	for _, name := range o.listed {
		if !allowedLists[name] {
			t.Errorf("unexpected directory list %q", name)
		}
	}
	allowedFiles := map[string]bool{
		base + "active_sessions.json":          true,
		sessions + "x/r/summary.json":          true,
		sessions + "x/r/events.jsonl":          true,
		sessions + "x/r/updates.jsonl":         true,
		sessions + "x/r/subagents/a/meta.json": true,
		sessions + "x/r/subagents/b/meta.json": true,
		sessions + "x/child/updates.jsonl":     true,
	}
	for _, name := range o.readFiles {
		if strings.HasPrefix(name, strings.TrimSuffix(sessions, "/")) && !allowedFiles[name] {
			t.Errorf("unexpected file read %q", name)
		}
	}
	for _, name := range o.actualOpens {
		if strings.HasPrefix(name, strings.TrimSuffix(sessions, "/")) && !allowedFiles[name] && !allowedLists[name] {
			t.Errorf("unexpected file open %q", name)
		}
	}
	for _, name := range append(o.statted, o.readLinked...) {
		if strings.HasPrefix(name, strings.TrimSuffix(sessions, "/")) && !allowedFiles[name] && !allowedLists[name] {
			t.Errorf("unexpected stat or link read %q", name)
		}
	}
	for _, call := range o.fileCalls {
		parts := strings.SplitN(call, " ", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed file call %q", call)
		}
		name := parts[1]
		if strings.HasPrefix(name, strings.TrimSuffix(sessions, "/")) && !allowedFiles[name] && !allowedLists[name] {
			t.Errorf("unexpected opened-file method %q", call)
		}
	}
	for _, name := range []string{base + "active_sessions.json", sessions + "x/r/summary.json", sessions + "x/r/subagents/a/meta.json", sessions + "x/r/subagents/b/meta.json"} {
		if countName(o.readFiles, name) != 1 || countName(o.actualOpens, name) != 0 {
			t.Errorf("%s: ReadFile %d Open %d", name, countName(o.readFiles, name), countName(o.actualOpens, name))
		}
	}
	if len(o.writes) != 0 {
		t.Fatalf("writes: %v", o.writes)
	}
	tr := &treeReadTrace{files: r, opens: map[string]int{}, reads: map[string]int{}, readAts: map[string]int{}, stats: map[string]int{}, readDirs: map[string]int{}, closes: map[string]int{}, ranges: map[string][]readRange{}}
	got = mustTree(t, tr)
	if got.Root.Status != tree.StatusWorking {
		t.Fatalf("open-only tree = %+v", got)
	}
	for _, name := range []string{sessions + "x/r/events.jsonl", sessions + "x/r/updates.jsonl", sessions + "x/child/updates.jsonl"} {
		if tr.opens[name] != 1 || tr.reads[name] != 0 || tr.readAts[name] == 0 {
			t.Errorf("log %s opened %d read %d readAt %d", name, tr.opens[name], tr.reads[name], tr.readAts[name])
		}
		for i, a := range tr.ranges[name] {
			if a.len <= 0 || a.off < 0 || a.off+int64(a.len) > int64(len(r[name].Data)) {
				t.Errorf("out-of-bounds read of %s: %+v", name, a)
			}
			for _, b := range tr.ranges[name][:i] {
				if a.off < b.off+int64(b.len) && b.off < a.off+int64(a.len) {
					t.Errorf("overlapping reads of %s: %+v %+v", name, a, b)
				}
			}
		}
	}
}

func countName(names []string, want string) int {
	count := 0
	for _, name := range names {
		if name == want {
			count++
		}
	}
	return count
}
