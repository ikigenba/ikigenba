package linux

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

var errSeedReadOutsideTee = errors.New("seed file read outside command input consumption")

type guardedSeedFS struct {
	files     fstest.MapFS
	consuming bool
}

func (s *guardedSeedFS) Open(path string) (fs.File, error) {
	f, err := s.files.Open(path)
	if err != nil || path == "." {
		return f, err
	}
	return guardedSeedFile{File: f, source: s}, nil
}

type guardedSeedFile struct {
	fs.File
	source *guardedSeedFS
}

func (f guardedSeedFile) Read(p []byte) (int, error) {
	if !f.source.consuming {
		return 0, errSeedReadOutsideTee
	}
	return f.File.Read(p)
}

func TestSeedFilesReadDuringTee(t *testing.T) {
	// R-KR34-ANBD
	const folder = "/srv/citron"
	const file = "sample"
	const content = "seed bytes\n"
	source := &guardedSeedFS{files: fstest.MapFS{file: {Data: []byte(content)}}}
	if _, err := fs.ReadFile(source, file); !errors.Is(err, errSeedReadOutsideTee) {
		t.Fatalf("eager read bypassed the guard: %v", err)
	}
	root := t.TempDir()
	count := 0
	h := New(Env{Root: root, Execute: func(_ context.Context, c Command) (Result, error) {
		count++
		want := []string{"-u", "citron", "--", "tee", filepath.Join(root, folder, file)}
		if c.Name != "runuser" || !slices.Equal(c.Args, want) || c.Stdin == nil {
			t.Fatalf("seed command: %s %q", c.Name, c.Args)
		}
		source.consuming = true
		data, err := io.ReadAll(c.Stdin)
		source.consuming = false
		if err != nil || string(data) != content {
			t.Fatalf("seed command input = %q, error = %v", data, err)
		}
		return Result{}, nil
	}})
	if err := h.copySeed(t.Context(), "citron", folder, source); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("seed commands = %d, want 1", count)
	}
}
