package write_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentkit/tools/write"
)

// R-W0PK-4XBN: the Write tool writes textual content to a file at an
// absolute file_path. On success the file's bytes are exactly the bytes the
// model supplied as content.
func TestR_W0PK_4XBN_WriteCreatesFileWithExactContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	content := "hello\nworld\n"

	block, err := write.Write("toolu_w0pk", path, content)
	if err != nil {
		t.Fatalf("Write returned unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content = %q, want %q", got, content)
	}
}

// R-JR8E-92QM: the input JSON schema declares exactly two required
// properties — file_path (string) and content (string). Shape matches
// Claude Code's Write tool.
func TestR_JR8E_92QM_WriteSchemaShape(t *testing.T) {
	if write.Name != "Write" {
		t.Errorf("Name = %q, want %q", write.Name, "Write")
	}

	var schema map[string]any
	if err := json.Unmarshal(write.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}

	if got := schema["type"]; got != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, got)
	}

	required, _ := schema["required"].([]any)
	if len(required) != 2 {
		t.Fatalf("required has %d entries, want 2; got %v", len(required), required)
	}
	reqSet := map[string]bool{}
	for _, r := range required {
		s, _ := r.(string)
		reqSet[s] = true
	}
	for _, field := range []string{"file_path", "content"} {
		if !reqSet[field] {
			t.Errorf("required does not contain %q", field)
		}
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", schema["properties"])
	}
	for _, field := range []string{"file_path", "content"} {
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

// R-7VLS-NHCD: Write overwrites the existing file at file_path if one is
// present. The previous contents are discarded without backup or
// confirmation.
func TestR_7VLS_NHCD_WriteOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")

	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	newContent := "new content"
	block, err := write.Write("toolu_7vls", path, newContent)
	if err != nil {
		t.Fatalf("Write returned unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true on overwrite, want false")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != newContent {
		t.Errorf("file content after overwrite = %q, want %q", got, newContent)
	}
}

// R-K3XF-PB1Y: when file_path's parent directory does not exist, Write
// returns an error tool_result and does not auto-create intermediate dirs.
func TestR_K3XF_PB1Y_WriteMissingParentDirReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent-subdir", "file.txt")

	block, err := write.Write("toolu_k3xf", path, "content")
	if err != nil {
		t.Fatalf("Write returned unexpected go-error: %v", err)
	}
	if !block.IsError {
		t.Fatalf("is_error = false, want true when parent dir does not exist")
	}

	// Confirm the file was NOT created.
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("file should not exist after error, but os.Stat returned: %v", statErr)
	}
}

// R-CMWG-58TR: writes are atomic — Write uses a temp file in the same
// directory and renames it over the target. A failure does not leave a
// partially-written target. This test verifies the happy-path outcome of
// the strategy: the final file has the correct content and no temp files
// remain in the directory.
func TestR_CMWG_58TR_WriteIsAtomicNoleftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.txt")
	content := "atomic content"

	block, err := write.Write("toolu_cmwg", path, content)
	if err != nil {
		t.Fatalf("Write returned unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true, want false")
	}

	// No temp files should remain.
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
	if string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

// R-2DHN-A6VK: the materialized file (whether new or replacing an existing
// file) has mode 0644.
func TestR_2DHN_A6VK_WriteFileModeIs0644(t *testing.T) {
	dir := t.TempDir()

	t.Run("new_file", func(t *testing.T) {
		path := filepath.Join(dir, "new.txt")
		if _, err := write.Write("toolu_2dhn_new", path, "x"); err != nil {
			t.Fatalf("Write: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("mode = %o, want 0644", got)
		}
	})

	t.Run("replacing_existing", func(t *testing.T) {
		path := filepath.Join(dir, "existing.txt")
		// Create with a different mode to confirm Write normalizes it.
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if _, err := write.Write("toolu_2dhn_rep", path, "new"); err != nil {
			t.Fatalf("Write: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("mode after replace = %o, want 0644", got)
		}
	})
}

// R-PE9Q-1JFL: on success Write returns a tool_result with is_error=false
// and a short human-readable confirmation that distinguishes file creation
// from file replacement and includes the absolute path.
func TestR_PE9Q_1JFL_WriteSuccessMessageDistinguishesCreateFromReplace(t *testing.T) {
	dir := t.TempDir()

	t.Run("create", func(t *testing.T) {
		path := filepath.Join(dir, "create_me.txt")
		block, err := write.Write("toolu_pe9q_create", path, "content")
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false")
		}
		var msg string
		if err := json.Unmarshal(block.Content, &msg); err != nil {
			t.Fatalf("content not a JSON string: %v", err)
		}
		if !strings.Contains(strings.ToLower(msg), "created") {
			t.Errorf("success message for new file does not contain \"created\": %q", msg)
		}
		if !strings.Contains(msg, path) {
			t.Errorf("success message does not include the path %q: %q", path, msg)
		}
	})

	t.Run("replace", func(t *testing.T) {
		path := filepath.Join(dir, "replace_me.txt")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		block, err := write.Write("toolu_pe9q_replace", path, "new")
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error = true, want false")
		}
		var msg string
		if err := json.Unmarshal(block.Content, &msg); err != nil {
			t.Fatalf("content not a JSON string: %v", err)
		}
		lc := strings.ToLower(msg)
		if !strings.Contains(lc, "updated") && !strings.Contains(lc, "replaced") && !strings.Contains(lc, "overwrote") {
			t.Errorf("success message for replacement does not indicate replacement: %q", msg)
		}
		if !strings.Contains(msg, path) {
			t.Errorf("success message does not include the path %q: %q", path, msg)
		}
	})
}

// R-0GKA-MQ8B (cross-cutting): Write requires an absolute path; relative
// paths are rejected with an error tool_result.
func TestR_0GKA_MQ8B_WriteRejectsRelativePath(t *testing.T) {
	block, err := write.Write("toolu_0gka_w", "relative/path.txt", "content")
	if err != nil {
		t.Fatalf("Write returned unexpected go-error: %v", err)
	}
	if !block.IsError {
		t.Fatalf("is_error = false, want true for relative path")
	}
}
