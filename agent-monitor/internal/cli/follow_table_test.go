package cli

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// R-I6OP-C6T3 R-ED12-GH8M
func TestFollowViewMatchesSnapshotArguments(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		for _, terminal := range []bool{false, true} {
			sys := System{Home: "/home/dev", Root: fstest.MapFS{}, Terminal: terminal, Interrupt: followTestClosed()}
			_, snap, _ := runRecorded([]string{"list", h}, sys, nil, nil)
			out, diag := &followTestWriter{}, &followTestWriter{}
			code := Run([]string{"list", "-f", h, "--follow"}, sys, out, diag)
			prefix := ""
			if terminal {
				prefix = "\x1b[?25l\x1b[H\x1b[2J"
			}
			if code != ExitSuccess || out.writes[0] != prefix+snap.writes[0] || len(diag.writes) != 0 {
				t.Fatalf("snapshot list %s: %d %q", h, code, out.writes)
			}
		}
	}
	root := fstest.MapFS{"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")}}
	for _, opts := range [][]string{nil, {"--no-color"}} {
		for _, term := range []string{"xterm", "dumb"} {
			args := append([]string{"tree"}, opts...)
			args = append(args, "claude", "sample")
			sys := System{Home: "/home/dev", Root: root, Terminal: true, Term: term, Interrupt: followTestClosed()}
			_, snap, _ := runRecorded(args, sys, nil, nil)
			args = append(args, "-f", "--follow")
			out, diag := &followTestWriter{}, &followTestWriter{}
			if code := Run(args, sys, out, diag); code != ExitSuccess || out.writes[0] != "\x1b[?25l\x1b[H\x1b[2J"+snap.writes[0] {
				t.Fatalf("tree snapshot %q: %d %q", args, code, out.writes)
			}
		}
	}
}

// R-I94I-3QAH R-D4B2-PQNN R-EKCG-R3OS R-ELKD-4VFH R-EMS9-IN66
func TestFollowViewsRenderOnceAndWriteWholeOutputs(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		interrupt := make(chan struct{})
		renders := 0
		var views []string
		root := &followTestRoot{}
		root.onOpen = func(name string) {
			if name == "home/dev/.claude/sessions" {
				renders++
				title := "first"
				if renders >= 3 {
					title = "second"
				}
				root.data = followTestListData(title)
				if renders == 4 {
					root.data = fstest.MapFS{}
					close(interrupt)
				}
				_, out, _ := runRecorded([]string{"list", "claude"}, System{Home: "/home/dev", Root: root.data}, nil, nil)
				views = append(views, out.writes[0])
			}
		}
		watcher := &followTestWatcher{changes: followTestEvents(4)}
		out, diag := &followTestWriter{}, &followTestWriter{}
		code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: root, Terminal: terminal, Interrupt: interrupt, Watcher: watcher}, out, diag)
		want := []string{views[0], "\n" + views[2], "\n" + views[3]}
		if terminal {
			want = []string{"\x1b[?25l\x1b[H\x1b[2J" + views[0], "\x1b[H\x1b[2J" + views[2], "\x1b[H\x1b[2J" + views[3], "\x1b[?25h"}
		}
		if code != ExitSuccess || renders != 4 || !reflect.DeepEqual(out.writes, want) || len(diag.writes) != 0 || len(watcher.changes) != 1 {
			t.Fatalf("renders %d code %d out %q want %q", renders, code, out.writes, want)
		}
	}
}

// R-F1F2-3W2I
func TestFollowFailedLaterViewPreservesComparison(t *testing.T) {
	interrupt := make(chan struct{})
	renders := 0
	root := &followTestRoot{data: fstest.MapFS{"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")}}}
	root.onOpen = func(name string) {
		if name == "home/dev/.claude/projects" {
			renders++
			root.data = fstest.MapFS{"home/dev/.claude/projects/work/other.jsonl": &fstest.MapFile{Data: []byte("{}\n")}}
			if renders != 2 {
				root.data["home/dev/.claude/projects/work/sample.jsonl"] = &fstest.MapFile{Data: []byte("{}\n")}
			}
			if renders == 3 {
				close(interrupt)
			}
		}
	}
	out, diag := &followTestWriter{}, &followTestWriter{}
	watcher := &followTestWatcher{changes: followTestEvents(3)}
	code := Run([]string{"tree", "-f", "claude", "sample"}, System{Home: "/home/dev", Root: root, Interrupt: interrupt, Watcher: watcher}, out, diag)
	if code != ExitSuccess || renders != 3 || len(out.writes) != 1 || len(diag.writes) != 0 || len(watcher.watches) < 2 {
		t.Fatalf("failed view: %d %d %q %q", code, renders, out.writes, diag.writes)
	}
}

// R-X80B-K5O5
func TestFollowWatchOccursAfterWriteAndChangesOnly(t *testing.T) {
	interrupt := make(chan struct{})
	renders := 0
	root := &followTestRoot{}
	root.onOpen = func(name string) {
		if name == "home/dev/.claude/sessions" {
			renders++
			root.data = followTestListData("same")
			if renders == 2 {
				root.data = fstest.MapFS{}
			}
			if renders == 3 {
				close(interrupt)
			}
		}
	}
	var events []string
	watcher := &followTestWatcher{changes: followTestEvents(3)}
	watcher.onWatch = func(names []string) { events = append(events, "watch:"+strings.Join(names, ",")) }
	out := &followTestWriter{onWrite: func(_ int, _ string) error { events = append(events, "write"); return nil }}
	diag := &followTestWriter{}
	code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: root, Interrupt: interrupt, Watcher: watcher}, out, diag)
	want := []string{"write", "watch:home/dev/.claude,home/dev/.claude/sessions", "write", "watch:home/dev/.claude/sessions", "write", "watch:home/dev/.claude,home/dev/.claude/sessions"}
	if code != ExitSuccess || !reflect.DeepEqual(events, want) {
		t.Fatalf("watch order %d: %q want %q", code, events, want)
	}
	// The initial watch also occurs when the interrupt was already closed.
	watcher = &followTestWatcher{}
	out = &followTestWriter{}
	code = Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: fstest.MapFS{}, Interrupt: followTestClosed(), Watcher: watcher}, out, &followTestWriter{})
	if code != ExitSuccess || len(watcher.watches) != 1 {
		t.Fatalf("closed interrupt watch: %d %#v", code, watcher)
	}
	watcher = &followTestWatcher{}
	code = Run([]string{"list", "-f", "claude"}, System{Home: "/proc", Root: fstest.MapFS{}, Interrupt: followTestClosed(), Watcher: watcher}, &followTestWriter{}, &followTestWriter{})
	if code != ExitSuccess || len(watcher.watches) != 1 || len(watcher.watches[0]) != 0 {
		t.Fatalf("empty watched set: %d %#v", code, watcher)
	}
}

type followCountRoot struct {
	fstest.MapFS
	boundary  string
	calls     int
	interrupt chan struct{}
	changes   chan struct{}
	t         *testing.T
}

func (r *followCountRoot) count(name string) {
	if name != r.boundary {
		return
	}
	r.calls++
	if got, want := len(r.changes), 3-(r.calls-1); got != want {
		r.t.Errorf("render %d consumed %d changes; buffer %d want %d", r.calls, 3-got, got, want)
	}
	if r.calls == 3 {
		close(r.interrupt)
	}
}
func (r *followCountRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	r.count(name)
	return r.MapFS.ReadDir(name)
}
func (r *followCountRoot) ReadFile(name string) ([]byte, error) {
	r.count(name)
	return r.MapFS.ReadFile(name)
}

// R-I94I-3QAH R-D4B2-PQNN
func TestFollowEachHarnessListAndTreeCallPerChange(t *testing.T) {
	data := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl":                                  &fstest.MapFile{Data: []byte("{}\n")},
		"home/dev/.codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-sample.jsonl": &fstest.MapFile{Data: []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"sample\"}}\n")},
		"home/dev/.grok/sessions/x/sample/summary.json":                                &fstest.MapFile{Data: []byte(`{"session_kind":"headless"}`)},
		"home/dev/.grok/sessions/x/sample/updates.jsonl":                               &fstest.MapFile{Data: []byte("{}\n")},
	}
	for _, kind := range []string{"list", "tree"} {
		for _, h := range []string{"claude", "codex", "grok"} {
			boundary := map[string]string{"claude": "home/dev/.claude/sessions", "codex": "home/dev/.codex/thread-writer-locks", "grok": "home/dev/.grok/active_sessions.json"}[h]
			args := []string{kind, "-f", h}
			if kind == "tree" {
				boundary = map[string]string{"claude": "home/dev/.claude/projects", "codex": "home/dev/.codex/sessions/2026/09/24", "grok": "home/dev/.grok/sessions"}[h]
				args = append(args, "sample")
			}
			changes := followTestEvents(3)
			root := &followCountRoot{MapFS: data, boundary: boundary, interrupt: make(chan struct{}), changes: changes, t: t}
			out, diag := &followTestWriter{}, &followTestWriter{}
			code := Run(args, System{Home: "/home/dev", Root: root, Interrupt: root.interrupt, Watcher: &followTestWatcher{changes: changes}}, out, diag)
			if code != ExitSuccess || root.calls != 3 || len(diag.writes) != 0 {
				t.Fatalf("%q: %d calls %d diag %q", args, code, root.calls, diag.writes)
			}
		}
	}
}

// R-X80B-K5O5
func TestFollowDoesNotWatchAnUnchangedSetAgain(t *testing.T) {
	interrupt := make(chan struct{})
	renders := 0
	root := &followTestRoot{data: followTestListData("first")}
	root.onOpen = func(name string) {
		if name != "home/dev/.claude/sessions" {
			return
		}
		renders++
		title := []string{"first", "second", "third"}[renders-1]
		root.data = followTestListData(title)
		if renders == 3 {
			close(interrupt)
		}
	}
	watcher := &followTestWatcher{changes: followTestEvents(3)}
	var events []string
	out := &followTestWriter{onWrite: func(n int, _ string) error {
		events = append(events, "write")
		if got, want := len(watcher.changes), 4-n; got != want {
			t.Errorf("write %d: remaining changes %d, want %d", n, got, want)
		}
		return nil
	}}
	watcher.onWatch = func(_ []string) {
		events = append(events, "watch")
		if len(out.writes) != 1 || len(watcher.changes) != 3 {
			t.Errorf("initial Watch occurred with %d writes and %d pending changes", len(out.writes), len(watcher.changes))
		}
	}
	diag := &followTestWriter{}
	code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: root, Interrupt: interrupt, Watcher: watcher}, out, diag)
	wantNames := [][]string{{"home/dev/.claude", "home/dev/.claude/sessions"}}
	if code != ExitSuccess || renders != 3 || len(diag.writes) != 0 {
		t.Fatalf("follow result: %d renders %d diagnostics %q", code, renders, diag.writes)
	}
	if !reflect.DeepEqual(watcher.watches, wantNames) || !reflect.DeepEqual(events, []string{"write", "watch", "write", "write"}) {
		t.Fatalf("watch calls %q events %q; want %q after the initial write only", watcher.watches, events, wantNames)
	}
}
