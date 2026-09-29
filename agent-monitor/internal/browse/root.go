package browse

import (
	"io/fs"
	"path"
	"slices"
	"strings"
)

// browseRoot deliberately exposes only the read interfaces used by io/fs.
type browseRoot struct {
	source fs.FS
	names  map[string]struct{}
}

func resetRoot(r *browseRoot) { r.names = make(map[string]struct{}) }

func recordRoot(r *browseRoot, name string) {
	if name == "proc" || strings.HasPrefix(name, "proc/") {
		return
	}
	directory := path.Dir(name)
	if info, err := fs.Stat(r.source, name); err == nil && info.IsDir() {
		directory = name
	}
	r.names[directory] = struct{}{}
}

func watchedRoot(r *browseRoot) []string {
	names := make([]string, 0, len(r.names))
	for name := range r.names {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (r *browseRoot) Open(name string) (fs.File, error) {
	recordRoot(r, name)
	return r.source.Open(name)
}

func (r *browseRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	recordRoot(r, name)
	return fs.ReadDir(r.source, name)
}

func (r *browseRoot) ReadFile(name string) ([]byte, error) {
	recordRoot(r, name)
	return fs.ReadFile(r.source, name)
}

func (r *browseRoot) Stat(name string) (fs.FileInfo, error) {
	recordRoot(r, name)
	return fs.Stat(r.source, name)
}

func (r *browseRoot) ReadLink(name string) (string, error) {
	recordRoot(r, name)
	return fs.ReadLink(r.source, name)
}

func (r *browseRoot) Lstat(name string) (fs.FileInfo, error) {
	recordRoot(r, name)
	return fs.Lstat(r.source, name)
}
