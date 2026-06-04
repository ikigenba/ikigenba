package edit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ralph/internal/engine/tools/edit"
)

// R-RTG4-Q9VK: the Edit tool performs an exact-string replacement within a
// single file. The input schema declares file_path, old_string, new_string
// (all required), and replace_all (optional boolean, default false). Shape
// matches Claude Code's Edit tool.
func TestR_RTG4_Q9VK_EditSchemaShape(t *testing.T) {
	if edit.Name != "Edit" {
		t.Errorf("Name = %q, want %q", edit.Name, "Edit")
	}

	var schema map[string]any
	if err := json.Unmarshal(edit.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	if got := schema["type"]; got != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, got)
	}

	required, _ := schema["required"].([]any)
	reqSet := map[string]bool{}
	for _, r := range required {
		if s, ok := r.(string); ok {
			reqSet[s] = true
		}
	}
	for _, field := range []string{"file_path", "old_string", "new_string"} {
		if !reqSet[field] {
			t.Errorf("required does not contain %q", field)
		}
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type")
	}
	for _, field := range []string{"file_path", "old_string", "new_string"} {
		if _, ok := props[field]; !ok {
			t.Errorf("properties[%q] missing", field)
		}
	}
	if _, ok := props["replace_all"]; !ok {
		t.Errorf("properties[\"replace_all\"] missing")
	}
}

// R-8XBN-MP2C: file_path must be absolute and must exist as a regular file.
// Relative path, non-existent path, and directory all return error tool_result.
func TestR_8XBN_MP2C_EditFilePathValidation(t *testing.T) {
	dir := t.TempDir()

	t.Run("relative_path_rejected", func(t *testing.T) {
		block, err := edit.Edit("toolu_8xbn_rel", "relative/path.txt", "old", "new", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true for relative path")
		}
	})

	t.Run("nonexistent_file_rejected", func(t *testing.T) {
		path := filepath.Join(dir, "no_such_file.txt")
		block, err := edit.Edit("toolu_8xbn_ne", path, "old", "new", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true for non-existent path")
		}
	})

	t.Run("directory_rejected", func(t *testing.T) {
		block, err := edit.Edit("toolu_8xbn_dir", dir, "old", "new", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true for directory path")
		}
	})
}

// R-LFJD-7HRO: replacement is byte-exact — no whitespace normalization,
// line-ending fuzzing, or encoding conversion.
func TestR_LFJD_7HRO_EditByteExactReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exact.txt")

	// Use content with Windows-style CRLF in old_string — the tool must not
	// normalize these; only the literal bytes must match.
	original := "line1\r\nline2\r\nline3\r\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := edit.Edit("toolu_lfjd", path, "line2\r\n", "replaced\r\n", false)
	if err != nil {
		t.Fatalf("unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false; byte-exact match should succeed")
	}

	got, _ := os.ReadFile(path)
	want := "line1\r\nreplaced\r\nline3\r\n"
	if string(got) != want {
		t.Errorf("file content = %q, want %q", got, want)
	}
}

// R-3CWS-EAYI: when replace_all is false, old_string must occur exactly once.
// Zero occurrences → error. More than one → error with count hint.
func TestR_3CWS_EAYI_EditExactlyOnceWhenReplaceAllFalse(t *testing.T) {
	dir := t.TempDir()

	t.Run("not_found_is_error", func(t *testing.T) {
		path := filepath.Join(dir, "notfound.txt")
		if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_3cws_nf", path, "missing", "x", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true when old_string not found")
		}
	})

	t.Run("multiple_occurrences_is_error", func(t *testing.T) {
		path := filepath.Join(dir, "multi.txt")
		if err := os.WriteFile(path, []byte("foo bar foo baz foo"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_3cws_multi", path, "foo", "x", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true when old_string appears multiple times")
		}
		var msg string
		if err := json.Unmarshal(block.Content, &msg); err == nil {
			// Error message should mention the count and hint about replace_all.
			if !strings.Contains(msg, "3") {
				t.Errorf("error message should mention occurrence count (3); got: %q", msg)
			}
			if !strings.Contains(msg, "replace_all") {
				t.Errorf("error message should hint at replace_all; got: %q", msg)
			}
		}
	})

	t.Run("exactly_one_succeeds", func(t *testing.T) {
		path := filepath.Join(dir, "one.txt")
		if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_3cws_one", path, "world", "earth", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false for single occurrence")
		}
		got, _ := os.ReadFile(path)
		if string(got) != "hello earth" {
			t.Errorf("content = %q, want %q", got, "hello earth")
		}
	})
}

// R-VK0M-5BTL: when replace_all is true, every occurrence is replaced.
// Zero occurrences is still an error.
func TestR_VK0M_5BTL_EditReplaceAllMode(t *testing.T) {
	dir := t.TempDir()

	t.Run("replaces_all_occurrences", func(t *testing.T) {
		path := filepath.Join(dir, "all.txt")
		if err := os.WriteFile(path, []byte("a b a c a"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_vk0m_all", path, "a", "X", true)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false")
		}
		got, _ := os.ReadFile(path)
		if string(got) != "X b X c X" {
			t.Errorf("content = %q, want %q", got, "X b X c X")
		}
	})

	t.Run("zero_occurrences_still_error", func(t *testing.T) {
		path := filepath.Join(dir, "zero.txt")
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_vk0m_zero", path, "missing", "x", true)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if !block.IsError {
			t.Fatalf("is_error = false, want true for zero occurrences with replace_all=true")
		}
	})
}

// R-NJZH-1XPE: new_string must differ from old_string; byte-equal returns error.
func TestR_NJZH_1XPE_EditRejectsIdenticalStrings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := edit.Edit("toolu_njzh", path, "hello", "hello", false)
	if err != nil {
		t.Fatalf("unexpected go-error: %v", err)
	}
	if !block.IsError {
		t.Fatalf("is_error = false, want true when old_string == new_string")
	}
}

// R-O6QT-FAUR: empty old_string must be rejected with an error tool_result.
func TestR_O6QT_FAUR_EditRejectsEmptyOldString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := edit.Edit("toolu_o6qt", path, "", "x", false)
	if err != nil {
		t.Fatalf("unexpected go-error: %v", err)
	}
	if !block.IsError {
		t.Fatalf("is_error = false, want true for empty old_string")
	}
}

// R-DM8K-9SCG: Edit is atomic via temp-file-plus-rename; no temp files remain
// after a successful edit.
func TestR_DM8K_9SCG_EditIsAtomicNoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := edit.Edit("toolu_dm8k", path, "world", "earth", false)
	if err != nil {
		t.Fatalf("unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".iki-tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}

	got, _ := os.ReadFile(path)
	if string(got) != "hello earth" {
		t.Errorf("content = %q, want %q", got, "hello earth")
	}
}

// R-HEFY-4WJN: Edit preserves the target file's mode across the edit.
func TestR_HEFY_4WJN_EditPreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("echo hello"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := edit.Edit("toolu_hefy", path, "hello", "world", false)
	if err != nil {
		t.Fatalf("unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("mode = %o, want 0755 (mode should be preserved)", got)
	}
}

// R-MZWT-K8VR: Edit does NOT enforce a "Read before Edit" precondition at the
// tool layer — a fresh call without a prior Read must succeed when the file
// content and old_string are valid.
func TestR_MZWT_K8VR_EditNoReadBeforePrecondition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no_prior_read.txt")
	if err := os.WriteFile(path, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Call Edit without any preceding Read — must succeed.
	block, err := edit.Edit("toolu_mzwt", path, "line two", "line 2", false)
	if err != nil {
		t.Fatalf("unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false (no Read-before precondition)")
	}

	got, _ := os.ReadFile(path)
	if string(got) != "line one\nline 2\n" {
		t.Errorf("content = %q, want %q", got, "line one\nline 2\n")
	}
}

// R-PQXB-J5LC: on success Edit returns a tool_result with is_error=false and a
// confirmation that includes the absolute path and the number of replacements.
func TestR_PQXB_J5LC_EditSuccessMessageIncludesPathAndCount(t *testing.T) {
	dir := t.TempDir()

	t.Run("single_replacement", func(t *testing.T) {
		path := filepath.Join(dir, "single.txt")
		if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_pqxb_one", path, "world", "earth", false)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false")
		}
		var msg string
		if err := json.Unmarshal(block.Content, &msg); err != nil {
			t.Fatalf("content not a JSON string: %v", err)
		}
		if !strings.Contains(msg, path) {
			t.Errorf("success message should contain path %q; got: %q", path, msg)
		}
		if !strings.Contains(msg, "1") {
			t.Errorf("success message should contain replacement count; got: %q", msg)
		}
	})

	t.Run("multiple_replacements_with_replace_all", func(t *testing.T) {
		path := filepath.Join(dir, "multi.txt")
		if err := os.WriteFile(path, []byte("a b a c a"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := edit.Edit("toolu_pqxb_multi", path, "a", "X", true)
		if err != nil {
			t.Fatalf("unexpected go-error: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false")
		}
		var msg string
		if err := json.Unmarshal(block.Content, &msg); err != nil {
			t.Fatalf("content not a JSON string: %v", err)
		}
		if !strings.Contains(msg, "3") {
			t.Errorf("success message should contain count 3; got: %q", msg)
		}
	})
}
