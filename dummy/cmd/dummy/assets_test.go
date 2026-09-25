package main

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/dummy"
)

// R-5EOI-Q5FN
func TestAssetsIsExportedEmbedFSVariable(t *testing.T) {
	if reflect.TypeOf(dummy.Assets) != reflect.TypeFor[embed.FS]() {
		t.Fatalf("Assets has type %T, want embed.FS", dummy.Assets)
	}
}

// R-T6W6-W28R
func TestAssetDirectoryEntries(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(mainProjectRoot(t), "assets"))
	if err != nil {
		t.Fatalf("read assets directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("assets directory is empty")
	}
	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			t.Errorf("stat asset %q: %v", name, err)
			continue
		}
		if !info.Mode().IsRegular() {
			t.Errorf("asset %q is not a regular file: %v", name, info.Mode())
		}
		if !validAssetName(name) {
			t.Errorf("asset name %q is not embeddable", name)
		}
	}
}

func validAssetName(name string) bool {
	if !utf8.ValidString(name) || name == "go.mod" || strings.HasSuffix(name, ".") {
		return false
	}
	for _, reserved := range []string{".bzr", ".git", ".hg", ".svn"} {
		if name == reserved {
			return false
		}
	}
	stem := strings.SplitN(name, ".", 2)[0]
	for _, reserved := range []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"} {
		if strings.EqualFold(stem, reserved) {
			return false
		}
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(" !#$%&()+,-.=@[]^_{}~", r) || r > 127 && unicode.IsLetter(r) {
			continue
		}
		return false
	}
	return true
}

// R-5FWF-3X6C
func TestAssetsExactlyMatchDirectory(t *testing.T) {
	assetDir := filepath.Join(mainProjectRoot(t), "assets")
	entries, err := os.ReadDir(assetDir)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(assetDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("close assets directory: %v", err)
		}
	}()
	wantPaths := []string{".", "assets"}
	for _, entry := range entries {
		wantPaths = append(wantPaths, path.Join("assets", entry.Name()))
		want, err := root.ReadFile(entry.Name())
		if err != nil {
			t.Errorf("read asset %q: %v", entry.Name(), err)
			continue
		}
		got, err := dummy.Assets.ReadFile(path.Join("assets", entry.Name()))
		if err != nil {
			t.Errorf("read embedded asset %q: %v", entry.Name(), err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("embedded asset %q differs from disk", entry.Name())
		}
	}
	var gotPaths []string
	err = fs.WalkDir(dummy.Assets, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		gotPaths = append(gotPaths, name)
		if name != "." && name != "assets" && !entry.Type().IsRegular() {
			t.Errorf("embedded entry %q is not a regular file", name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Assets: %v", err)
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Errorf("embedded paths = %q, want %q", gotPaths, wantPaths)
	}
}

// R-WYWT-KUV5
func TestAssetsHoldThemeCSS(t *testing.T) {
	info, err := fs.Stat(dummy.Assets, "assets/theme.css")
	if err != nil {
		t.Fatalf("stat embedded theme.css: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("embedded theme.css is not regular: %v", info.Mode())
	}
}
