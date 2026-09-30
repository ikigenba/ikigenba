package browse

import (
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"
)

type runtimeRootCall struct{ method, name string }
type runtimeRootSpy struct {
	source  fstest.MapFS
	calls   []runtimeRootCall
	failure error
}

func (r *runtimeRootSpy) Open(n string) (fs.File, error) {
	r.calls = append(r.calls, runtimeRootCall{"Open", n})
	return r.source.Open(n)
}
func (r *runtimeRootSpy) ReadDir(n string) ([]fs.DirEntry, error) {
	r.calls = append(r.calls, runtimeRootCall{"ReadDir", n})
	if r.failure != nil {
		return nil, r.failure
	}
	return fs.ReadDir(r.source, n)
}
func (r *runtimeRootSpy) ReadFile(n string) ([]byte, error) {
	r.calls = append(r.calls, runtimeRootCall{"ReadFile", n})
	if r.failure != nil {
		return nil, r.failure
	}
	return fs.ReadFile(r.source, n)
}
func (r *runtimeRootSpy) Stat(n string) (fs.FileInfo, error) {
	r.calls = append(r.calls, runtimeRootCall{"Stat", n})
	return fs.Stat(r.source, n)
}
func (r *runtimeRootSpy) ReadLink(n string) (string, error) {
	r.calls = append(r.calls, runtimeRootCall{"ReadLink", n})
	return "target", r.failure
}
func (r *runtimeRootSpy) Lstat(n string) (fs.FileInfo, error) {
	r.calls = append(r.calls, runtimeRootCall{"Lstat", n})
	return fs.Stat(r.source, n)
}

func TestBrowseRootForwarding(t *testing.T) {
	// R-YYY4-BESZ
	source := &runtimeRootSpy{source: fstest.MapFS{"folder/item": &fstest.MapFile{Data: []byte("value")}}}
	root := &browseRoot{source: source}
	resetRoot(root)
	var _ interface {
		fs.ReadDirFS
		fs.ReadFileFS
		fs.StatFS
		fs.ReadLinkFS
	} = root
	file, err := root.Open("folder/item")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := root.ReadDir("folder")
	if err != nil || len(entries) != 1 || entries[0].Name() != "item" {
		t.Fatalf("ReadDir: %v %v", entries, err)
	}
	data, err := root.ReadFile("folder/item")
	if err != nil || string(data) != "value" {
		t.Fatalf("ReadFile: %q %v", data, err)
	}
	info, err := root.Stat("folder/item")
	if err != nil || info.Name() != "item" {
		t.Fatalf("Stat: %v %v", info, err)
	}
	target, err := root.ReadLink("folder/item")
	if err != nil || target != "target" {
		t.Fatalf("ReadLink: %q %v", target, err)
	}
	info, err = root.Lstat("folder/item")
	if err != nil || info.Name() != "item" {
		t.Fatalf("Lstat: %v %v", info, err)
	}
	for _, name := range []string{"Open", "ReadDir", "ReadFile", "Stat", "ReadLink", "Lstat"} {
		targetName := "folder/item"
		if name == "ReadDir" {
			targetName = "folder"
		}
		if !slices.Contains(source.calls, runtimeRootCall{name, targetName}) {
			t.Fatalf("missing unchanged call %s %s: %v", name, targetName, source.calls)
		}
	}
	sentinel := errors.New("read failure")
	source.failure = sentinel
	if _, err := root.ReadFile("missing"); reflect.ValueOf(err) != reflect.ValueOf(sentinel) {
		t.Fatalf("ReadFile error: %v", err)
	}
	if _, err := root.ReadDir("missing"); reflect.ValueOf(err) != reflect.ValueOf(sentinel) {
		t.Fatalf("ReadDir error: %v", err)
	}
	if _, err := root.ReadLink("missing"); reflect.ValueOf(err) != reflect.ValueOf(sentinel) {
		t.Fatalf("ReadLink error: %v", err)
	}
}

func TestWatchedSet(t *testing.T) {
	// R-W71K-4SM5
	root := &browseRoot{source: fstest.MapFS{
		"home/dev/.claude/sessions/41822.json": &fstest.MapFile{Data: []byte("{}")},
		"proc/41822/stat":                      &fstest.MapFile{Data: []byte("state")},
	}}
	resetRoot(root)
	_, _ = root.ReadDir("home/dev/.claude/sessions")
	_, _ = root.ReadFile("home/dev/.claude/sessions/41822.json")
	_, _ = root.Stat("home/dev/.grok")
	_, _ = root.ReadFile("proc/41822/stat")
	_, _ = root.Stat("proc")
	_, _ = root.Open("home/dev/.claude/sessions/41822.json")
	want := []string{"home/dev", "home/dev/.claude/sessions"}
	if !slices.Equal(watchedRoot(root), want) {
		t.Fatalf("watched: %v", watchedRoot(root))
	}
	resetRoot(root)
	if got := watchedRoot(root); len(got) != 0 {
		t.Fatalf("empty: %#v", got)
	}
}

// These methods expose write-capable values to make an accidental mutation
// fail immediately instead of silently changing an injected fixture.
type runtimeReadOnlyRoot struct{ *runtimeRootSpy }

func (*runtimeReadOnlyRoot) Write([]byte) (int, error)                   { panic("Write called") }
func (*runtimeReadOnlyRoot) WriteAt([]byte, int64) (int, error)          { panic("WriteAt called") }
func (*runtimeReadOnlyRoot) WriteString(string) (int, error)             { panic("WriteString called") }
func (*runtimeReadOnlyRoot) WriteFile(string, []byte, fs.FileMode) error { panic("WriteFile called") }
func (*runtimeReadOnlyRoot) Create(string) (fs.File, error)              { panic("Create called") }
func (*runtimeReadOnlyRoot) OpenFile(string, int, fs.FileMode) (fs.File, error) {
	panic("OpenFile called")
}
func (*runtimeReadOnlyRoot) Mkdir(string, fs.FileMode) error    { panic("Mkdir called") }
func (*runtimeReadOnlyRoot) MkdirAll(string, fs.FileMode) error { panic("MkdirAll called") }
func (*runtimeReadOnlyRoot) Remove(string) error                { panic("Remove called") }
func (*runtimeReadOnlyRoot) RemoveAll(string) error             { panic("RemoveAll called") }
func (*runtimeReadOnlyRoot) Rename(string, string) error        { panic("Rename called") }
func (*runtimeReadOnlyRoot) Truncate(int64) error               { panic("Truncate called") }
func (*runtimeReadOnlyRoot) Chmod(fs.FileMode) error            { panic("Chmod called") }
func (*runtimeReadOnlyRoot) Chown(int, int) error               { panic("Chown called") }
func (*runtimeReadOnlyRoot) Chtimes() error                     { panic("Chtimes called") }
func (*runtimeReadOnlyRoot) Symlink(string, string) error       { panic("Symlink called") }
func (*runtimeReadOnlyRoot) Link(string, string) error          { panic("Link called") }
func (*runtimeReadOnlyRoot) Sync() error                        { panic("Sync called") }

func (r *runtimeReadOnlyRoot) Open(name string) (fs.File, error) {
	file, err := r.runtimeRootSpy.Open(name)
	if err != nil {
		return nil, err
	}
	return &runtimeReadOnlyFile{runtimeReadOnlyRoot: r, file: file}, nil
}

type runtimeReadOnlyFile struct {
	*runtimeReadOnlyRoot
	file fs.File
}

func (f *runtimeReadOnlyFile) Read(data []byte) (int, error) { return f.file.Read(data) }
func (f *runtimeReadOnlyFile) Stat() (fs.FileInfo, error)    { return f.file.Stat() }
func (f *runtimeReadOnlyFile) Close() error                  { return f.file.Close() }
