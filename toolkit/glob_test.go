package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGlobConstructor(t *testing.T) {
	tool, err := Glob(t.TempDir())
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Glob() returned a nil tool")
	}
	if got, want := tool.Name(), "Glob"; got != want {
		t.Errorf("tool.Name() = %q, want %q", got, want)
	}
	if _, err := Glob(t.TempDir(), WithSkip("x")); err != nil {
		t.Fatalf("Glob() with option error = %v", err)
	}
	// R-C7QQ-WA2X
}

func TestGlobSchema(t *testing.T) {
	tool, err := Glob(t.TempDir())
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", schema["properties"])
	}
	if got, want := sortedKeys(properties), []string{"path", "pattern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("property names = %v, want %v", got, want)
	}
	pattern := properties["pattern"].(map[string]any)
	if got := pattern["type"]; got != "string" {
		t.Errorf("pattern type = %#v, want string", got)
	}
	if got := pattern["minLength"]; got != float64(1) {
		t.Errorf("pattern minLength = %#v, want 1", got)
	}
	path := properties["path"].(map[string]any)
	if got := path["type"]; got != "string" {
		t.Errorf("path type = %#v, want string", got)
	}
	if got, want := schema["required"], []any{"pattern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("required = %#v, want %#v", got, want)
	}
	// R-DGV1-1SIM
}

func TestGlobMatchesFilesWithDoublestar(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"top.go", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "sub.go"), 0o700); err != nil {
		t.Fatalf("mkdir matching directory: %v", err)
	}
	nestedDir := filepath.Join(root, "nested", "deep")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatalf("mkdir nested directory: %v", err)
	}
	nestedFile := filepath.Join(nestedDir, "nested.go")
	if err := os.WriteFile(nestedFile, []byte("nested"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	if want := filepath.Join(root, "top.go"); got != want {
		t.Errorf("Glob *.go = %q, want only regular file %q", got, want)
	}
	if strings.Contains(got, filepath.Join(root, "sub.go")) {
		t.Errorf("Glob returned matching directory: %q", got)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("Glob doublestar call error = %v", err)
	}
	lines := strings.Split(got, "\n")
	if !contains(lines, nestedFile) {
		t.Errorf("Glob doublestar result %q does not contain nested file %q", got, nestedFile)
	}
	// R-DI2X-FK9B
}

func TestGlobOrdersAbsolutePathsByModificationTime(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "newest.txt"),
		filepath.Join(root, "alpha.txt"),
		filepath.Join(root, "beta.txt"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	old := time.Unix(1_700_000_000, 0)
	newest := old.Add(time.Hour)
	for _, path := range paths[1:] {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("set times for %s: %v", path, err)
		}
	}
	if err := os.Chtimes(paths[0], newest, newest); err != nil {
		t.Fatalf("set times for %s: %v", paths[0], err)
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"*.txt"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	want := strings.Join(paths, "\n")
	if got != want {
		t.Errorf("Glob ordering = %q, want %q", got, want)
	}
	for _, path := range strings.Split(got, "\n") {
		if !filepath.IsAbs(path) {
			t.Errorf("Glob path %q is not absolute", path)
		}
	}
	// R-DJAT-TC00
}

func TestGlobNoFilesFound(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "present.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	if got != "No files found" {
		t.Errorf("Glob no-match result = %q, want %q", got, "No files found")
	}
	// R-DKIQ-73QP
}

func TestGlobRejectsInvalidPattern(t *testing.T) {
	tool, err := Glob(t.TempDir())
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	_, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"a[bc"}`))
	if err == nil {
		t.Fatal("Glob invalid pattern returned nil error")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("Glob error = %q, want it to name pattern", err)
	}
	// R-DLQM-KVHE
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
