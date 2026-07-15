package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSessionToolsetIsExactAndConfinesFilesystemPaths(t *testing.T) {
	// R-F5Z1-C926
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	set := New(root)
	var names []string
	byName := make(map[string]interface {
		Call(context.Context, json.RawMessage) (string, error)
	})
	for _, tool := range set {
		names = append(names, tool.Name())
		byName[tool.Name()] = tool
	}
	if want := []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}
	output, err := byName["Bash"].Call(context.Background(), json.RawMessage(`{"command":"pwd"}`))
	if err != nil || strings.TrimSpace(output) != root {
		t.Fatalf("Bash pwd = %q, %v; want %q", output, err, root)
	}
	for _, input := range []string{
		`{"path":"` + filepath.ToSlash(outside) + `"}`,
		`{"path":"../secret.txt"}`,
	} {
		if _, err := byName["Read"].Call(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("Read(%s) unexpectedly escaped worktree", input)
		}
	}
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := byName["Read"].Call(context.Background(), json.RawMessage(`{"path":"outside-link"}`)); err == nil {
		t.Fatal("Read followed an escaping symlink")
	}
}
