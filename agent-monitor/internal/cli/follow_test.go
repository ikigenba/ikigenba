package cli

import (
	"errors"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

type followTestWatcher struct {
	changes      chan struct{}
	watches      [][]string
	changesCalls int
	onWatch      func([]string)
}

func (w *followTestWatcher) Watch(names []string) {
	w.watches = append(w.watches, append([]string{}, names...))
	if w.onWatch != nil {
		w.onWatch(names)
	}
}
func (w *followTestWatcher) Changes() <-chan struct{} { w.changesCalls++; return w.changes }

type followTestWriter struct {
	writes  []string
	onWrite func(int, string) error
}

func (w *followTestWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	if w.onWrite != nil {
		if err := w.onWrite(len(w.writes), string(p)); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

type followTestRoot struct {
	data   fstest.MapFS
	onOpen func(string)
}

func (r *followTestRoot) Open(name string) (fs.File, error) {
	if r.onOpen != nil {
		r.onOpen(name)
	}
	return r.data.Open(name)
}
func (r *followTestRoot) Stat(name string) (fs.FileInfo, error) { return fs.Stat(r.data, name) }
func followTestClosed() chan struct{}                           { c := make(chan struct{}); close(c); return c }
func followTestEvents(n int) chan struct{} {
	c := make(chan struct{}, n)
	for range n {
		c <- struct{}{}
	}
	return c
}
func followTestListData(title string) fstest.MapFS {
	return fstest.MapFS{
		"home/dev/.claude/sessions/a.json": &fstest.MapFile{Data: []byte(`{"sessionId":"alpha","pid":42,"status":"busy","name":"` + title + `","cwd":"/work"}`)},
		"proc/42/stat":                     &fstest.MapFile{Data: []byte("42 (a) " + strings.Repeat("0 ", 19) + "150")},
	}
}

// R-EJ9J-A3CZ
func TestFollowNilWatcher(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		var results []struct {
			code      ExitCode
			out, diag []string
		}
		for _, watcher := range []Watcher{nil, &followTestWatcher{changes: make(chan struct{})}} {
			out, diag := &followTestWriter{}, &followTestWriter{}
			code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: fstest.MapFS{}, Terminal: terminal, Interrupt: followTestClosed(), Watcher: watcher}, out, diag)
			results = append(results, struct {
				code      ExitCode
				out, diag []string
			}{code, out.writes, diag.writes})
		}
		if !reflect.DeepEqual(results[0], results[1]) || results[0].code != ExitSuccess {
			t.Fatalf("nil watcher results: %#v", results)
		}
	}
}

// R-N8JL-F6LI
func TestFollowNilInterruptEndsOnlyOnFailure(t *testing.T) {
	for _, args := range [][]string{{"list", "-f", "claude"}, {"tree", "-f", "claude", "missing"}, {"chat", "-f", "claude", "missing"}} {
		out, diag := &followTestWriter{onWrite: func(int, string) error { return errors.New("broken") }}, &followTestWriter{}
		code := Run(args, System{Home: "/home/dev", Root: fstest.MapFS{}}, out, diag)
		want := ExitNotFound
		if args[0] == "list" {
			want = ExitWriteFailed
		}
		if code != want {
			t.Fatalf("nil interrupt %q: %d", args, code)
		}
	}
	out, diag := &followTestWriter{}, &followTestWriter{}
	code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: &deniedFS{}}, out, diag)
	if code != ExitDataUnreadable {
		t.Fatalf("nil interrupt unreadable: %d", code)
	}
}

// R-I7WL-PYJS
func TestFollowStartupFailureDoesNotTouchWatcher(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		for _, kind := range []string{"list", "tree", "chat"} {
			args := []string{kind, h}
			if kind != "list" {
				args = append(args, "missing")
			}
			snapCode, snapOut, snapErr := runRecorded(args, System{Home: "/home/dev", Root: &deniedFS{}}, nil, nil)
			watcher := &followTestWatcher{}
			out, diag := &followTestWriter{}, &followTestWriter{}
			code := Run(append(args, "-f"), System{Home: "/home/dev", Root: &deniedFS{}, Watcher: watcher}, out, diag)
			if code != snapCode || !reflect.DeepEqual(out.writes, snapOut.writes) || !reflect.DeepEqual(diag.writes, snapErr.writes) || len(watcher.watches) != 0 || watcher.changesCalls != 0 {
				t.Fatalf("startup %q: %d %q %q watcher %#v", args, code, out.writes, diag.writes, watcher)
			}
		}
	}
}

// R-HII3-JDHF
func TestFollowInterruptPriorityAndCompletesRender(t *testing.T) {
	for _, already := range []bool{false, true} {
		interrupt := make(chan struct{})
		if already {
			close(interrupt)
		}
		renders := 0
		root := &followTestRoot{data: fstest.MapFS{}}
		root.onOpen = func(name string) {
			if name == "home/dev/.claude/sessions" {
				renders++
				if !already {
					close(interrupt)
				}
			}
		}
		watcher := &followTestWatcher{changes: followTestEvents(2)}
		out, diag := &followTestWriter{}, &followTestWriter{}
		code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: root, Terminal: true, Interrupt: interrupt, Watcher: watcher}, out, diag)
		if code != ExitSuccess || renders != 1 || len(watcher.changes) != 2 || len(out.writes) != 2 || out.writes[1] != "\x1b[?25h" || len(diag.writes) != 0 {
			t.Fatalf("interrupt: code %d renders %d out %q diag %q", code, renders, out.writes, diag.writes)
		}
	}
}

// R-HJPZ-X584
func TestFollowWriteFailureStopsImmediately(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		interrupt := make(chan struct{})
		renders := 0
		root := &followTestRoot{}
		root.onOpen = func(name string) {
			if name == "home/dev/.claude/sessions" {
				renders++
				root.data = followTestListData(strconv.Itoa(renders))
				if renders == 2 {
					close(interrupt)
				}
			}
		}
		watcher := &followTestWatcher{changes: followTestEvents(3)}
		out := &followTestWriter{onWrite: func(n int, _ string) error {
			if n == failAt {
				return errors.New("broken")
			}
			return nil
		}}
		diag := &followTestWriter{}
		code := Run([]string{"list", "-f", "claude"}, System{Home: "/home/dev", Root: root, Terminal: true, Interrupt: interrupt, Watcher: watcher}, out, diag)
		wantRenders := 2
		if failAt == 1 {
			wantRenders = 1
		}
		if code != ExitWriteFailed || len(out.writes) != failAt || renders != wantRenders || len(diag.writes) != 1 || diag.writes[0] != "agent-monitor: write error: broken\n" {
			t.Fatalf("failure %d: %d %q %q renders %d", failAt, code, out.writes, diag.writes, renders)
		}
		if failAt == 1 && (len(watcher.watches) != 0 || watcher.changesCalls != 0) {
			t.Fatal("watcher touched after initial write error")
		}
		if len(watcher.changes) != 3-(wantRenders-1) {
			t.Fatal("received after failed write")
		}
	}
}
