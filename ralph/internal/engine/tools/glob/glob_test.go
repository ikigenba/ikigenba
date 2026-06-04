package glob_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ralph/internal/engine/tools/glob"
)

// R-Q4PX-7KJN: the Glob tool's input JSON schema declares pattern (required
// string) and path (optional string). Shape matches Claude Code's Glob tool.
func TestR_Q4PX_7KJN_GlobSchemaShape(t *testing.T) {
	if glob.Name != "Glob" {
		t.Errorf("Name = %q, want %q", glob.Name, "Glob")
	}

	var schema map[string]any
	if err := json.Unmarshal(glob.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	if got := schema["type"]; got != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, got)
	}

	required, _ := schema["required"].([]any)
	if len(required) != 1 {
		t.Fatalf("required has %d entries, want 1; got %v", len(required), required)
	}
	if required[0] != "pattern" {
		t.Errorf("required[0] = %v, want %q", required[0], "pattern")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type")
	}
	for _, field := range []string{"pattern", "path"} {
		p, ok := props[field].(map[string]any)
		if !ok {
			t.Errorf("properties[%q] missing", field)
			continue
		}
		if p["type"] != "string" {
			t.Errorf("properties[%q].type = %v, want \"string\"", field, p["type"])
		}
	}
}

// R-NMVL-8WCR: when path is omitted, Glob searches from session cwd; when
// supplied, path must be absolute and an existing directory.
func TestR_NMVL_8WCR_GlobPathValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("omitted_uses_cwd", func(t *testing.T) {
		t.Chdir(dir)
		block, err := glob.Glob("id1", "*.txt", "")
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false")
		}
		var body string
		if e := json.Unmarshal(block.Content, &body); e != nil {
			t.Fatalf("content not JSON string: %v", e)
		}
		if !strings.Contains(body, "a.txt") {
			t.Errorf("result does not contain a.txt: %q", body)
		}
	})

	t.Run("relative_path_error", func(t *testing.T) {
		block, err := glob.Glob("id2", "*.txt", "relative/path")
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true for relative path")
		}
	})

	t.Run("nonexistent_path_error", func(t *testing.T) {
		block, err := glob.Glob("id3", "*.txt", filepath.Join(dir, "nonexistent"))
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true for nonexistent path")
		}
	})

	t.Run("file_as_path_error", func(t *testing.T) {
		block, err := glob.Glob("id4", "*.txt", filepath.Join(dir, "a.txt"))
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true when path is a file not a dir")
		}
	})

	t.Run("valid_absolute_dir", func(t *testing.T) {
		block, err := glob.Glob("id5", "*.txt", dir)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false for valid absolute dir")
		}
	})
}

// R-3BHF-CKQT: pattern syntax supports *, ?, [...], and ** (recursive).
func TestR_3BHF_CKQT_GlobPatternSyntax(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	deep := filepath.Join(sub, "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	files := map[string]string{
		filepath.Join(dir, "foo.go"):         "go",
		filepath.Join(dir, "bar.txt"):        "txt",
		filepath.Join(sub, "baz.go"):         "go",
		filepath.Join(deep, "qux.go"):        "go",
		filepath.Join(dir, "abc.go"):         "go",
		filepath.Join(dir, "a1.go"):          "go",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("setup write %s: %v", path, err)
		}
	}

	t.Run("star_single_segment", func(t *testing.T) {
		block, _ := glob.Glob("id", "*.go", dir)
		if block.IsError {
			t.Fatalf("is_error true")
		}
		body := bodyStr(t, block.Content)
		if !strings.Contains(body, "foo.go") {
			t.Errorf("expected foo.go in result: %q", body)
		}
		if strings.Contains(body, "baz.go") {
			t.Errorf("* should not cross directory boundary — unexpected baz.go: %q", body)
		}
	})

	t.Run("double_star_recursive", func(t *testing.T) {
		block, _ := glob.Glob("id", "**/*.go", dir)
		if block.IsError {
			t.Fatalf("is_error true")
		}
		body := bodyStr(t, block.Content)
		for _, name := range []string{"foo.go", "baz.go", "qux.go"} {
			if !strings.Contains(body, name) {
				t.Errorf("expected %s in ** result: %q", name, body)
			}
		}
	})

	t.Run("question_mark", func(t *testing.T) {
		// a1.go: a + one char + .go
		block, _ := glob.Glob("id", "??.go", dir)
		if block.IsError {
			t.Fatalf("is_error true")
		}
		body := bodyStr(t, block.Content)
		if !strings.Contains(body, "a1.go") {
			t.Errorf("? pattern should match a1.go: %q", body)
		}
		if strings.Contains(body, "foo.go") {
			t.Errorf("? pattern should not match foo.go (3 chars before .go): %q", body)
		}
	})

	t.Run("character_class", func(t *testing.T) {
		// [fb]oo.go matches foo.go (bar.txt has no .go)
		block, _ := glob.Glob("id", "[fb]oo.go", dir)
		if block.IsError {
			t.Fatalf("is_error true")
		}
		body := bodyStr(t, block.Content)
		if !strings.Contains(body, "foo.go") {
			t.Errorf("[fb]oo.go should match foo.go: %q", body)
		}
	})
}

// R-Y8ZE-5DPM: Glob returns regular files only, sorted by mtime descending.
func TestR_Y8ZE_5DPM_GlobRegularFilesOnlySortedByMtime(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now()

	// Create files with explicit mtimes so sort order is deterministic.
	older := filepath.Join(dir, "older.txt")
	newer := filepath.Join(dir, "newer.txt")
	for _, p := range []string{older, newer} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	if err := os.Chtimes(older, now, now.Add(-2*time.Second)); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatalf("chtimes newer: %v", err)
	}

	block, err := glob.Glob("id", "*.txt", dir)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false")
	}
	body := bodyStr(t, block.Content)

	// Directory itself must not appear.
	if strings.Contains(body, sub) {
		t.Errorf("result contains directory path %s; should return regular files only", sub)
	}

	// newer must appear before older.
	idxNewer := strings.Index(body, "newer.txt")
	idxOlder := strings.Index(body, "older.txt")
	if idxNewer == -1 || idxOlder == -1 {
		t.Fatalf("expected both newer.txt and older.txt in result: %q", body)
	}
	if idxNewer > idxOlder {
		t.Errorf("newer.txt should precede older.txt (mtime desc sort); body: %q", body)
	}
}

// R-LGRA-2VTW: every path in the Glob result is absolute.
func TestR_LGRA_2VTW_GlobResultPathsAreAbsolute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := glob.Glob("id", "*.go", dir)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true")
	}
	body := bodyStr(t, block.Content)
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		if !filepath.IsAbs(line) {
			t.Errorf("path %q is not absolute", line)
		}
	}
}

// R-X7CN-9MFB: an empty match set is a successful tool_result with a short
// body indicating no matches; it is not an error.
func TestR_X7CN_9MFB_GlobEmptyMatchIsSuccess(t *testing.T) {
	dir := t.TempDir()

	block, err := glob.Glob("id", "*.nonexistent", dir)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true for empty match; want false")
	}
	body := bodyStr(t, block.Content)
	if body == "" {
		t.Errorf("body is empty; want a short 'no files found' message")
	}
}

// R-EJSO-1HUK: result is capped at 100 matched paths; when cap is reached a
// visible truncation notice is appended.
func TestR_EJSO_1HUK_GlobCapAt100WithTruncationNotice(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 101; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file%03d.txt", i))
		if err := os.WriteFile(name, []byte(""), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	block, err := glob.Glob("id", "*.txt", dir)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false")
	}
	body := bodyStr(t, block.Content)

	// Count non-notice lines (paths).
	var pathLines int
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		pathLines++
	}
	if pathLines != 100 {
		t.Errorf("got %d path lines, want 100", pathLines)
	}

	if !strings.Contains(body, "truncated") {
		t.Errorf("truncation notice missing from result: %q", body)
	}
}

func bodyStr(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("content is not a JSON string: %v", err)
	}
	return s
}
