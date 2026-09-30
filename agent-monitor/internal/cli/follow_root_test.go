package cli

import (
	"errors"
	"io"
	"io/fs"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type rootForwardProbe struct {
	root  fstest.MapFS
	calls []string
	err   error
	file  fs.File
}

func (p *rootForwardProbe) Open(n string) (fs.File, error) {
	p.calls = append(p.calls, "Open:"+n)
	if p.err != nil {
		return nil, p.err
	}
	if p.file != nil {
		return p.file, nil
	}
	return p.root.Open(n)
}
func (p *rootForwardProbe) ReadDir(n string) ([]fs.DirEntry, error) {
	p.calls = append(p.calls, "ReadDir:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.ReadDir(p.root, n)
}
func (p *rootForwardProbe) ReadFile(n string) ([]byte, error) {
	p.calls = append(p.calls, "ReadFile:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.ReadFile(p.root, n)
}
func (p *rootForwardProbe) Stat(n string) (fs.FileInfo, error) {
	p.calls = append(p.calls, "Stat:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.Stat(p.root, n)
}
func (p *rootForwardProbe) ReadLink(n string) (string, error) {
	p.calls = append(p.calls, "ReadLink:"+n)
	if p.err != nil {
		return "", p.err
	}
	return "raw/../target", nil
}
func (p *rootForwardProbe) Lstat(n string) (fs.FileInfo, error) {
	p.calls = append(p.calls, "Lstat:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.Stat(p.root, n)
}

// R-Z060-P6JO
func TestFollowRootMethodSetAndForwarding(t *testing.T) {
	r := newFollowRoot(fstest.MapFS{})
	var _ interface {
		fs.ReadDirFS
		fs.ReadFileFS
		fs.StatFS
		fs.ReadLinkFS
	} = r
	f, err := (fstest.MapFS{"file": &fstest.MapFile{Data: []byte("text")}}).Open("file")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	p := &rootForwardProbe{file: f}
	if got, err := newFollowRoot(p).Open("unchanged/../name"); got != f || err != nil {
		t.Fatalf("Open did not preserve file: %v, %v", got, err)
	}
	operations := []struct {
		name string
		call func(fs.FS, string) (any, error)
	}{
		{"Open", func(r fs.FS, n string) (any, error) {
			f, err := r.Open(n)
			if err != nil {
				return nil, err
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			return io.ReadAll(f)
		}},
		{"ReadDir", func(r fs.FS, n string) (any, error) { return fs.ReadDir(r, n) }},
		{"ReadFile", func(r fs.FS, n string) (any, error) { return fs.ReadFile(r, n) }},
		{"Stat", func(r fs.FS, n string) (any, error) { return fs.Stat(r, n) }},
		{"ReadLink", func(r fs.FS, n string) (any, error) { return fs.ReadLink(r, n) }},
		{"Lstat", func(r fs.FS, n string) (any, error) { return fs.Lstat(r, n) }},
	}
	for _, op := range operations {
		for _, n := range []string{"dir", "dir/file", "missing", "dir/../file", "/absolute", ""} {
			for _, injected := range []error{nil, errors.New("read denied")} {
				p := &rootForwardProbe{root: fstest.MapFS{"dir/file": &fstest.MapFile{Data: []byte("text")}}, err: injected}
				want, wantErr := op.call(p, n)
				p.calls = nil
				got, gotErr := op.call(newFollowRoot(p), n)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
					t.Errorf("%s(%q) = (%v,%v), want (%v,%v)", op.name, n, got, gotErr, want, wantErr)
				}
				if p.calls[len(p.calls)-1] != op.name+":"+n {
					t.Errorf("%s(%q) calls %v", op.name, n, p.calls)
				}
			}
		}
	}
}

// R-Z060-P6JO R-IE03-MT99
func TestFollowRootGlobAndSubAccountNames(t *testing.T) {
	r := newFollowRoot(fstest.MapFS{"home/dev/logs/a/file": &fstest.MapFile{Data: []byte("log")}})
	matches, err := fs.Glob(r, "home/dev/logs/*/file")
	if err != nil || !slices.Equal(matches, []string{"home/dev/logs/a/file"}) {
		t.Fatalf("Glob = %v, %v", matches, err)
	}
	sub, err := fs.Sub(r, "home/dev/logs/a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(sub, "file"); err != nil {
		t.Fatal(err)
	}
	if got := watchedFollowNames(r); !slices.Equal(got, []string{"home/dev/logs", "home/dev/logs/a"}) {
		t.Errorf("watched = %v", got)
	}
}

// R-IE03-MT99
func TestFollowWatchedNames(t *testing.T) {
	r := newFollowRoot(fstest.MapFS{
		"home/dev/.claude/sessions/41822.json": &fstest.MapFile{Data: []byte("{}")},
		"proc/41822/stat":                      &fstest.MapFile{Data: []byte("stat")},
		"process/log":                          &fstest.MapFile{Data: []byte("log")},
	})
	_, _ = r.ReadDir("home/dev/.claude/sessions")
	f, err := r.Open("home/dev/.claude/sessions/41822.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_, _ = r.ReadFile("proc/41822/stat")
	_, _ = r.Stat("proc")
	_, _ = r.Stat("home/dev/.grok")
	_, _ = r.ReadFile("home/dev/.claude/sessions/41822.json")
	_, _ = r.Lstat("home/dev/.claude/sessions")
	_, _ = r.ReadLink("home/dev/.grok/link")
	want := []string{"home/dev", "home/dev/.claude/sessions", "home/dev/.grok"}
	if got := watchedFollowNames(r); !slices.Equal(got, want) {
		t.Errorf("watched = %v, want %v", got, want)
	}
	beginFollowRender(r)
	if got := watchedFollowNames(r); len(got) != 0 {
		t.Errorf("new render watched = %v", got)
	}
	_, _ = r.Stat("process/log")
	if got := watchedFollowNames(r); !slices.Equal(got, []string{"process"}) {
		t.Errorf("proc prefix exclusion = %v", got)
	}
}

// R-IE03-MT99
func TestFollowWatchedNamesForEachMethod(t *testing.T) {
	methods := []struct {
		name string
		call func(*followRoot, string)
	}{
		{"Open", func(r *followRoot, n string) {
			f, err := r.Open(n)
			if err == nil {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}
		}},
		{"ReadDir", func(r *followRoot, n string) { _, _ = r.ReadDir(n) }},
		{"ReadFile", func(r *followRoot, n string) { _, _ = r.ReadFile(n) }},
		{"Stat", func(r *followRoot, n string) { _, _ = r.Stat(n) }},
		{"ReadLink", func(r *followRoot, n string) { _, _ = r.ReadLink(n) }},
		{"Lstat", func(r *followRoot, n string) { _, _ = r.Lstat(n) }},
	}
	cases := []struct {
		name string
		want []string
	}{
		{"home/dev/data", []string{"home/dev/data"}},
		{"home/dev/data/file", []string{"home/dev/data"}},
		{"home/dev/absent/file", []string{"home/dev/absent"}},
		{"proc", []string{}},
		{"proc/123/stat", []string{}},
		{"proc/absent", []string{}},
		{"process/file", []string{"process"}},
	}
	for _, method := range methods {
		for _, tc := range cases {
			t.Run(method.name+"/"+tc.name, func(t *testing.T) {
				r := newFollowRoot(fstest.MapFS{"home/dev/data/file": &fstest.MapFile{Data: []byte("data")}, "proc/123/stat": &fstest.MapFile{Data: []byte("stat")}})
				method.call(r, tc.name)
				if got := watchedFollowNames(r); !slices.Equal(got, tc.want) {
					t.Fatalf("watched = %v, want %v", got, tc.want)
				}
				beginFollowRender(r)
				if got := watchedFollowNames(r); len(got) != 0 {
					t.Fatalf("next render watched = %v", got)
				}
			})
		}
	}
}

// The mutating method signatures remain available on both the root and files.
// Any call fails the test immediately rather than altering the fixture.
type rootMutationTrap struct{ t *testing.T }

func (p rootMutationTrap) fail() {
	p.t.Helper()
	p.t.Fatal("following called a mutating filesystem method")
}
func (p rootMutationTrap) Write([]byte) (int, error)                   { p.fail(); return 0, nil }
func (p rootMutationTrap) WriteAt([]byte, int64) (int, error)          { p.fail(); return 0, nil }
func (p rootMutationTrap) WriteString(string) (int, error)             { p.fail(); return 0, nil }
func (p rootMutationTrap) WriteFile(string, []byte, fs.FileMode) error { p.fail(); return nil }
func (p rootMutationTrap) Create(string) (fs.File, error)              { p.fail(); return nil, nil }
func (p rootMutationTrap) OpenFile(string, int, fs.FileMode) (fs.File, error) {
	p.fail()
	return nil, nil
}
func (p rootMutationTrap) Mkdir(string, fs.FileMode) error            { p.fail(); return nil }
func (p rootMutationTrap) MkdirAll(string, fs.FileMode) error         { p.fail(); return nil }
func (p rootMutationTrap) Remove(string) error                        { p.fail(); return nil }
func (p rootMutationTrap) RemoveAll(string) error                     { p.fail(); return nil }
func (p rootMutationTrap) Rename(string, string) error                { p.fail(); return nil }
func (p rootMutationTrap) Truncate(int64) error                       { p.fail(); return nil }
func (p rootMutationTrap) Chmod(fs.FileMode) error                    { p.fail(); return nil }
func (p rootMutationTrap) Chown(int, int) error                       { p.fail(); return nil }
func (p rootMutationTrap) Chtimes(string, time.Time, time.Time) error { p.fail(); return nil }
func (p rootMutationTrap) Symlink(string, string) error               { p.fail(); return nil }
func (p rootMutationTrap) Link(string, string) error                  { p.fail(); return nil }
func (p rootMutationTrap) Sync() error                                { p.fail(); return nil }

type rootReadOnlyProbe struct {
	rootMutationTrap
	root fstest.MapFS
}
type rootReadOnlyFile struct {
	rootMutationTrap
	fs.File
}

type rootReadOnlyInfo struct {
	rootMutationTrap
	fs.FileInfo
}
type rootReadOnlyEntry struct {
	rootMutationTrap
	fs.DirEntry
}

func (f rootReadOnlyFile) ReadAt(p []byte, offset int64) (int, error) {
	return f.File.(io.ReaderAt).ReadAt(p, offset)
}

func (f rootReadOnlyFile) Stat() (fs.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return rootReadOnlyInfo{rootMutationTrap: f.rootMutationTrap, FileInfo: info}, nil
}
func (e rootReadOnlyEntry) Info() (fs.FileInfo, error) {
	info, err := e.DirEntry.Info()
	if err != nil {
		return nil, err
	}
	return rootReadOnlyInfo{rootMutationTrap: e.rootMutationTrap, FileInfo: info}, nil
}

func (p rootReadOnlyProbe) Open(n string) (fs.File, error) {
	f, err := p.root.Open(n)
	if err != nil {
		return nil, err
	}
	return rootReadOnlyFile{rootMutationTrap: p.rootMutationTrap, File: f}, nil
}
func (f rootReadOnlyFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.File.(fs.ReadDirFile).ReadDir(n)
	for i, e := range entries {
		entries[i] = rootReadOnlyEntry{rootMutationTrap: f.rootMutationTrap, DirEntry: e}
	}
	return entries, err
}

type rootSafetyWatcher struct{ changes chan struct{} }

func (rootSafetyWatcher) Watch([]string)             {}
func (w rootSafetyWatcher) Changes() <-chan struct{} { return w.changes }

// R-F075-Q4BT
func TestFollowDoesNotMutateRootOrFiles(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl":                                  &fstest.MapFile{Data: []byte("{}\n")},
		"home/dev/.claude/sessions/sample.json":                                        &fstest.MapFile{Data: []byte(`{"sessionId":"sample","pid":42}`)},
		"home/dev/.codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-sample.jsonl": &fstest.MapFile{Data: []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"sample\"}}\n")},
		"home/dev/.grok/sessions/x/sample/summary.json":                                &fstest.MapFile{Data: []byte(`{"session_kind":"headless"}`)},
		"home/dev/.grok/sessions/x/sample/updates.jsonl":                               &fstest.MapFile{Data: []byte("{}\n")},
	}
	for _, terminal := range []bool{false, true} {
		for _, harness := range []string{"claude", "codex", "grok"} {
			for _, command := range []string{"list", "tree", "chat"} {
				args := []string{command, harness}
				if command != "list" {
					args = append(args, "sample")
				}
				args = append(args, "-f")
				interrupt := make(chan struct{})
				changes := make(chan struct{})
				done := make(chan struct{})
				go func() {
					defer close(done)
					for range 2 {
						select {
						case changes <- struct{}{}:
						case <-interrupt:
							return
						}
					}
					close(interrupt)
				}()
				sys := System{Home: "/home/dev", Root: rootReadOnlyProbe{rootMutationTrap: rootMutationTrap{t: t}, root: root}, Terminal: terminal, Interrupt: interrupt, Watcher: rootSafetyWatcher{changes: changes}}
				if code := Run(args, sys, io.Discard, io.Discard); code != ExitSuccess {
					close(interrupt)
					<-done
					t.Fatalf("fixture failed before following %v: %d", args, code)
				}
				<-done
			}
		}
	}
}

// rootSameProbe is sys.Root for a following Run. It records every name any
// read reaches it with, and closes the interrupt once the last queued change
// has been received, so the render then in progress is the last.
type rootSameProbe struct {
	root      fstest.MapFS
	names     []string
	changes   chan struct{}
	interrupt chan struct{}
	closed    bool
}

func (p *rootSameProbe) seen(n string) {
	p.names = append(p.names, n)
	if len(p.changes) == 0 && !p.closed {
		p.closed = true
		close(p.interrupt)
	}
}
func (p *rootSameProbe) Open(n string) (fs.File, error) { p.seen(n); return p.root.Open(n) }
func (p *rootSameProbe) ReadDir(n string) ([]fs.DirEntry, error) {
	p.seen(n)
	return fs.ReadDir(p.root, n)
}
func (p *rootSameProbe) ReadFile(n string) ([]byte, error)   { p.seen(n); return fs.ReadFile(p.root, n) }
func (p *rootSameProbe) Stat(n string) (fs.FileInfo, error)  { p.seen(n); return fs.Stat(p.root, n) }
func (p *rootSameProbe) ReadLink(n string) (string, error)   { p.seen(n); return fs.ReadLink(p.root, n) }
func (p *rootSameProbe) Lstat(n string) (fs.FileInfo, error) { p.seen(n); return fs.Lstat(p.root, n) }

// rootSameWatcher checks, at each Watch, that the set equals the names the
// render just ended reached sys.Root with, then flips the fixture so the next
// render's watched set differs: every name that render reached, and every
// fixture file, becomes a directory, or the fixture is restored.
type rootSameWatcher struct {
	t       *testing.T
	label   string
	probe   *rootSameProbe
	files   fstest.MapFS
	changes chan struct{}
	calls   int
}

func (w *rootSameWatcher) Watch(names []string) {
	w.t.Helper()
	seen := map[string]struct{}{}
	for _, n := range w.probe.names {
		if n == "proc" || strings.HasPrefix(n, "proc/") {
			continue
		}
		dir := path.Dir(n)
		if info, err := fs.Stat(w.probe.root, n); err == nil && info.IsDir() {
			dir = n
		}
		seen[dir] = struct{}{}
	}
	want := make([]string, 0, len(seen))
	for n := range seen {
		want = append(want, n)
	}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		w.t.Errorf("%s render %d: watched %v, but sys.Root was reached with names giving %v", w.label, w.calls+1, names, want)
	}
	w.calls++
	next := fstest.MapFS{}
	for n, f := range w.files {
		next[n] = &fstest.MapFile{Data: f.Data}
	}
	if w.calls%2 == 1 {
		for n := range next {
			next[n] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
		}
		for _, n := range w.probe.names {
			if fs.ValidPath(n) && n != "." && n != "proc" && !strings.HasPrefix(n, "proc/") {
				next[n] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
			}
		}
	}
	w.probe.names = nil
	clear(w.probe.root)
	maps.Copy(w.probe.root, next)
}
func (w *rootSameWatcher) Changes() <-chan struct{} { return w.changes }

// R-Z060-P6JO
func TestFollowEveryCallReachesTheOneFollowRoot(t *testing.T) {
	files := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl":                                  &fstest.MapFile{Data: []byte(`{"type":"user","message":{"content":"hello"}}` + "\n")},
		"home/dev/.claude/sessions/sample.json":                                        &fstest.MapFile{Data: []byte(`{"sessionId":"sample","pid":42}`)},
		"home/dev/.codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-sample.jsonl": &fstest.MapFile{Data: []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"sample\"}}\n")},
		"home/dev/.grok/sessions/x/sample/summary.json":                                &fstest.MapFile{Data: []byte(`{"session_kind":"headless"}`)},
		"home/dev/.grok/sessions/x/sample/updates.jsonl":                               &fstest.MapFile{Data: []byte("{}\n")},
	}
	const renders = 5
	for _, harness := range []string{"claude", "codex", "grok"} {
		for _, command := range []string{"list", "tree", "chat"} {
			args := []string{command, harness}
			if command != "list" {
				args = append(args, "sample")
			}
			args = append(args, "--follow")
			changes := make(chan struct{}, renders-1)
			for range renders - 1 {
				changes <- struct{}{}
			}
			probe := &rootSameProbe{root: maps.Clone(files), changes: changes, interrupt: make(chan struct{})}
			w := &rootSameWatcher{t: t, label: strings.Join(args, " "), probe: probe, files: files, changes: changes}
			sys := System{Home: "/home/dev", Root: probe, Watcher: w, Interrupt: probe.interrupt}
			if code := Run(args, sys, io.Discard, io.Discard); code != ExitSuccess {
				t.Fatalf("%v: code %d", args, code)
			}
			// Every render but the last must end in a Watch: the fixture
			// flips at each one, so no two consecutive sets are equal.
			if w.calls < renders-1 || len(changes) != 0 {
				t.Errorf("%v: %d Watch calls over %d renders", args, w.calls, renders-len(changes))
			}
		}
	}
}
