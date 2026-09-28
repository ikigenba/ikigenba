package cli

import (
	"io/fs"
	"path"
	"slices"
	"strings"
)

// followRoot exposes only the read operations whose names a render watches.
type followRoot struct {
	root fs.FS
	dirs map[string]struct{}
}

func newFollowRoot(root fs.FS) *followRoot {
	return &followRoot{root: root, dirs: make(map[string]struct{})}
}

func beginFollowRender(r *followRoot) {
	clear(r.dirs)
}

func watchedFollowNames(r *followRoot) []string {
	names := make([]string, 0, len(r.dirs))
	for name := range r.dirs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func recordFollowName(r *followRoot, name string) {
	if name == "proc" || strings.HasPrefix(name, "proc/") {
		return
	}
	dir := path.Dir(name)
	if info, err := fs.Stat(r.root, name); err == nil && info.IsDir() {
		dir = name
	}
	r.dirs[dir] = struct{}{}
}

func (r *followRoot) Open(name string) (fs.File, error) {
	recordFollowName(r, name)
	return r.root.Open(name)
}

func (r *followRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	recordFollowName(r, name)
	return fs.ReadDir(r.root, name)
}

func (r *followRoot) ReadFile(name string) ([]byte, error) {
	recordFollowName(r, name)
	return fs.ReadFile(r.root, name)
}

func (r *followRoot) Stat(name string) (fs.FileInfo, error) {
	recordFollowName(r, name)
	return fs.Stat(r.root, name)
}

func (r *followRoot) ReadLink(name string) (string, error) {
	recordFollowName(r, name)
	return fs.ReadLink(r.root, name)
}

func (r *followRoot) Lstat(name string) (fs.FileInfo, error) {
	recordFollowName(r, name)
	return fs.Lstat(r.root, name)
}
