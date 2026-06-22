package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/agentkit"
)

func TestAllReturnsSixBuiltInTools(t *testing.T) {
	// R-K0MO-FDTH
	got := All(t.TempDir())
	if len(got) != 6 {
		t.Fatalf("All returned %d tools, want 6", len(got))
	}

	gotNames := make([]string, 0, len(got))
	seen := map[string]bool{}
	for _, tool := range got {
		if tool.Name() == "" {
			t.Fatalf("tool has empty name")
		}
		if tool.Description() == "" {
			t.Fatalf("tool %q has empty description", tool.Name())
		}
		if seen[tool.Name()] {
			t.Fatalf("duplicate tool name %q", tool.Name())
		}
		seen[tool.Name()] = true
		gotNames = append(gotNames, tool.Name())

		var schema map[string]any
		if err := json.Unmarshal(tool.JSONSchema(), &schema); err != nil {
			t.Fatalf("tool %q schema is invalid JSON: %v", tool.Name(), err)
		}
		if schema["type"] != "object" {
			t.Fatalf("tool %q schema type = %v, want object", tool.Name(), schema["type"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Fatalf("tool %q schema has no object properties: %#v", tool.Name(), schema)
		}
	}

	wantNames := []string{nameRead, nameBash, nameWrite, nameEdit, nameGlob, nameGrep}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("All tool names = %v, want %v", gotNames, wantNames)
	}
}

func TestAllThreadsSandboxRootPerCall(t *testing.T) {
	// R-K1UK-T5K6
	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()

	toolsA := All(rootA)
	toolsB := All(rootB)

	callTool(t, ctx, findTool(t, toolsA, nameWrite), map[string]any{
		"file_path": "same.txt",
		"content":   "alpha",
	})
	callTool(t, ctx, findTool(t, toolsB, nameWrite), map[string]any{
		"file_path": "same.txt",
		"content":   "bravo",
	})

	assertFileContent(t, filepath.Join(rootA, "same.txt"), "alpha")
	assertFileContent(t, filepath.Join(rootB, "same.txt"), "bravo")

	gotA := callTool(t, ctx, findTool(t, toolsA, nameRead), map[string]any{"file_path": "same.txt"})
	if gotA != "alpha" {
		t.Fatalf("read from rootA = %q, want alpha", gotA)
	}
	gotB := callTool(t, ctx, findTool(t, toolsB, nameRead), map[string]any{"file_path": "same.txt"})
	if gotB != "bravo" {
		t.Fatalf("read from rootB = %q, want bravo", gotB)
	}

	callTool(t, ctx, findTool(t, toolsA, nameEdit), map[string]any{
		"file_path":   "same.txt",
		"old_string":  "alpha",
		"new_string":  "charlie",
		"replace_all": false,
	})
	assertFileContent(t, filepath.Join(rootA, "same.txt"), "charlie")
	assertFileContent(t, filepath.Join(rootB, "same.txt"), "bravo")

	callTool(t, ctx, findTool(t, toolsA, nameWrite), map[string]any{
		"file_path": "nested/match.txt",
		"content":   "needle\n",
	})
	gotGlob := mustStringSlice(t, callTool(t, ctx, findTool(t, toolsA, nameGlob), map[string]any{
		"path":    "nested",
		"pattern": "*.txt",
	}))
	if !reflect.DeepEqual(gotGlob, []string{"match.txt"}) {
		t.Fatalf("Glob in rootA nested dir = %v, want [match.txt]", gotGlob)
	}
	gotGrep := mustStringSlice(t, callTool(t, ctx, findTool(t, toolsA, nameGrep), map[string]any{
		"path":    "nested",
		"glob":    "*.txt",
		"pattern": "needle",
	}))
	if !reflect.DeepEqual(gotGrep, []string{"match.txt:1:needle"}) {
		t.Fatalf("Grep in rootA nested dir = %v, want [match.txt:1:needle]", gotGrep)
	}
	if _, err := os.Stat(filepath.Join(rootB, "nested", "match.txt")); !os.IsNotExist(err) {
		t.Fatalf("rootA nested write appeared in rootB: %v", err)
	}

	pwd := strings.TrimSpace(callTool(t, ctx, findTool(t, toolsA, nameBash), map[string]any{"command": "pwd"}))
	if pwd != filepath.Clean(rootA) {
		t.Fatalf("Bash pwd = %q, want sandbox root %q", pwd, filepath.Clean(rootA))
	}

	_, err := findTool(t, toolsA, nameRead).Call(ctx, mustJSON(t, map[string]any{"file_path": "../same.txt"}))
	if err == nil {
		t.Fatalf("Read of path escaping sandbox returned nil error")
	}
	if !strings.Contains(err.Error(), "escapes sandbox") {
		t.Fatalf("Read escape error = %v, want escapes sandbox", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(rootA), "same.txt")); !os.IsNotExist(err) {
		t.Fatalf("escape probe found unexpected file outside sandbox: %v", err)
	}

	_, err = findTool(t, toolsA, nameWrite).Call(ctx, mustJSON(t, map[string]any{
		"file_path": "../escape.txt",
		"content":   "outside",
	}))
	if err == nil {
		t.Fatalf("Write of path escaping sandbox returned nil error")
	}
	if !strings.Contains(err.Error(), "escapes sandbox") {
		t.Fatalf("Write escape error = %v, want escapes sandbox", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(rootA), "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escape created unexpected file outside sandbox: %v", err)
	}
}

func TestAllRejectsSymlinkEscapes(t *testing.T) {
	// R-K1UK-T5K6
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("classified\n"), 0o644); err != nil {
		t.Fatalf("WriteFile outside secret: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	builtins := All(root)
	expectEscapeError(t, findTool(t, builtins, nameRead), map[string]any{
		"file_path": "outside/secret.txt",
	})
	expectEscapeError(t, findTool(t, builtins, nameWrite), map[string]any{
		"file_path": "outside/new.txt",
		"content":   "leaked",
	})
	expectEscapeError(t, findTool(t, builtins, nameGlob), map[string]any{
		"path":    "outside",
		"pattern": "*.txt",
	})
	expectEscapeError(t, findTool(t, builtins, nameGrep), map[string]any{
		"path":    "outside",
		"pattern": "classified",
	})
	expectEscapeError(t, findTool(t, builtins, nameEdit), map[string]any{
		"file_path":   "outside/secret.txt",
		"old_string":  "classified",
		"new_string":  "published",
		"replace_all": false,
	})

	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("write through sandbox symlink created outside file: %v", err)
	}
	assertFileContent(t, filepath.Join(outside, "secret.txt"), "classified\n")
}

func findTool(t *testing.T, tools []agentkit.Tool, name string) agentkit.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func callTool(t *testing.T, ctx context.Context, tool agentkit.Tool, input map[string]any) string {
	t.Helper()
	out, err := tool.Call(ctx, mustJSON(t, input))
	if err != nil {
		t.Fatalf("%s.Call(%v): %v", tool.Name(), input, err)
	}
	return out
}

func expectEscapeError(t *testing.T, tool agentkit.Tool, input map[string]any) {
	t.Helper()
	_, err := tool.Call(context.Background(), mustJSON(t, input))
	if err == nil {
		t.Fatalf("%s.Call(%v) returned nil error", tool.Name(), input)
	}
	if !strings.Contains(err.Error(), "escapes sandbox") {
		t.Fatalf("%s.Call(%v) error = %v, want escapes sandbox", tool.Name(), input, err)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func mustStringSlice(t *testing.T, raw string) []string {
	t.Helper()
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", raw, err)
	}
	return got
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(b) != want {
		t.Fatalf("%s content = %q, want %q", path, string(b), want)
	}
}
