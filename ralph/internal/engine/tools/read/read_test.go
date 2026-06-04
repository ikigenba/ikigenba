package read_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ralph/internal/engine/tools/read"
)

// R-516Q-9RC2: Read of an absolute path that does not exist returns
// an error tool_result. It does not auto-create or silently return
// empty text.
func TestR_516Q_9RC2_ReadNonexistentPathReturnsErrorToolResult(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "definitely-not-here.txt")

	block, err := read.Read("toolu_test", missing, 0, 0)
	if err != nil {
		t.Fatalf("Read returned unexpected go-error: %v", err)
	}

	if block.Type != "tool_result" {
		t.Errorf("type = %q, want %q", block.Type, "tool_result")
	}
	if block.ToolUseID != "toolu_test" {
		t.Errorf("tool_use_id = %q, want %q", block.ToolUseID, "toolu_test")
	}
	if !block.IsError {
		t.Errorf("is_error = false, want true (R-516Q-9RC2)")
	}

	var content string
	if err := json.Unmarshal(block.Content, &content); err != nil {
		t.Fatalf("content not a JSON string: %v (raw=%s)", err, block.Content)
	}
	if content == "" {
		t.Errorf("content is empty; want a human-readable failure message")
	}
	if !strings.Contains(content, missing) {
		t.Errorf("content %q does not mention the missing path %q", content, missing)
	}
}

// R-0GKA-MQ8B: every filesystem-touching tool requires absolute
// paths; relative paths are rejected with an error tool_result.
func TestR_0GKA_MQ8B_ReadRejectsRelativePath(t *testing.T) {
	rel := "some/relative/path.txt"

	block, err := read.Read("toolu_rel", rel, 0, 0)
	if err != nil {
		t.Fatalf("Read returned unexpected go-error: %v", err)
	}

	if block.Type != "tool_result" {
		t.Errorf("type = %q, want %q", block.Type, "tool_result")
	}
	if block.ToolUseID != "toolu_rel" {
		t.Errorf("tool_use_id = %q, want %q", block.ToolUseID, "toolu_rel")
	}
	if !block.IsError {
		t.Errorf("is_error = false, want true (R-0GKA-MQ8B)")
	}

	var content string
	if err := json.Unmarshal(block.Content, &content); err != nil {
		t.Fatalf("content not a JSON string: %v (raw=%s)", err, block.Content)
	}
	if !strings.Contains(content, rel) {
		t.Errorf("content %q does not mention the rejected relative path %q", content, rel)
	}
	lc := strings.ToLower(content)
	if !strings.Contains(lc, "absolute") && !strings.Contains(lc, "relative") {
		t.Errorf("content %q does not explain the absolute-path requirement", content)
	}
}

// R-21VK-LY2Y: Read returns the file's textual content as a successful
// tool_result given an absolute path.
func TestR_21VK_LY2Y_ReadReturnsFileContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	body := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := read.Read("toolu_ok", path, 0, 0)
	if err != nil {
		t.Fatalf("Read returned unexpected go-error: %v", err)
	}
	if block.Type != "tool_result" {
		t.Errorf("type = %q, want %q", block.Type, "tool_result")
	}
	if block.ToolUseID != "toolu_ok" {
		t.Errorf("tool_use_id = %q, want %q", block.ToolUseID, "toolu_ok")
	}
	if block.IsError {
		t.Errorf("is_error = true, want false on success path (R-21VK-LY2Y)")
	}
	var content string
	if err := json.Unmarshal(block.Content, &content); err != nil {
		t.Fatalf("content not a JSON string: %v (raw=%s)", err, block.Content)
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(content, want) {
			t.Errorf("content %q missing line %q", content, want)
		}
	}
}

// R-2XKY-JZD0: success content is cat -n form — each line prefixed
// with its 1-based line number, then a tab, then the line's content.
func TestR_2XKY_JZD0_ReadFormatsAsCatN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	body := "first\nsecond\nthird\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	block, err := read.Read("toolu_fmt", path, 0, 0)
	if err != nil {
		t.Fatalf("Read returned unexpected go-error: %v", err)
	}
	if block.IsError {
		t.Fatalf("is_error = true; expected success")
	}
	var content string
	if err := json.Unmarshal(block.Content, &content); err != nil {
		t.Fatalf("content not a JSON string: %v", err)
	}
	want := "1\tfirst\n2\tsecond\n3\tthird\n"
	if content != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

// R-3JJ5-FUPI: Read accepts optional offset (1-based) and limit
// (max lines). Defaults: offset 1, limit 2000. Files larger than
// the limit are silently truncated; offset skips earlier lines.
func TestR_3JJ5_FUPI_ReadOffsetLimit(t *testing.T) {
	dir := t.TempDir()

	// Build a file with 2500 lines: "lineN".
	var big strings.Builder
	for i := 1; i <= 2500; i++ {
		fmt.Fprintf(&big, "line%d\n", i)
	}
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte(big.String()), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	read1 := func(off, lim int) string {
		t.Helper()
		block, err := read.Read("toolu_rng", path, off, lim)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if block.IsError {
			t.Fatalf("is_error true; expected success (off=%d lim=%d)", off, lim)
		}
		var s string
		if err := json.Unmarshal(block.Content, &s); err != nil {
			t.Fatalf("content not a JSON string: %v", err)
		}
		return s
	}

	// Defaults (0/0): start at 1, return up to 2000. Should include
	// lines 1..2000, exclude lines 2001+.
	def := read1(0, 0)
	if !strings.HasPrefix(def, "1\tline1\n") {
		t.Errorf("default content does not start with line 1: %q", def[:min(40, len(def))])
	}
	if !strings.Contains(def, "2000\tline2000\n") {
		t.Errorf("default content missing line 2000")
	}
	if strings.Contains(def, "2001\tline2001") {
		t.Errorf("default content unexpectedly includes line 2001 (limit=2000 not enforced)")
	}
	if got := strings.Count(def, "\n"); got != 2000 {
		t.Errorf("default emitted %d lines, want 2000", got)
	}

	// Explicit offset/limit: start at 100, return 5 lines.
	got := read1(100, 5)
	want := "100\tline100\n101\tline101\n102\tline102\n103\tline103\n104\tline104\n"
	if got != want {
		t.Errorf("offset=100 limit=5:\n got = %q\nwant = %q", got, want)
	}

	// Offset past EOF returns empty success content.
	empty := read1(9999, 10)
	if empty != "" {
		t.Errorf("offset past EOF: got %q, want empty", empty)
	}
}

// R-ZUM3-QUVT: tool failure surfaces to the model as a tool_result
// block with is_error=true and a human-readable text body describing
// the failure. ikigai-cli does not crash on tool errors. This test
// drives a representative failure (nonexistent path) and asserts the
// contract end-to-end.
func TestR_ZUM3_QUVT_ToolFailureSurfacesAsErrorToolResult(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.txt")
	relative := "still/relative.txt"

	cases := []struct {
		name      string
		toolUseID string
		path      string
	}{
		{"nonexistent_absolute", "toolu_zum3_a", missing},
		{"relative_path", "toolu_zum3_b", relative},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, err := read.Read(tc.toolUseID, tc.path, 0, 0)
			if err != nil {
				t.Fatalf("Read returned a go-error instead of an error tool_result: %v", err)
			}
			if block.Type != "tool_result" {
				t.Errorf("type = %q, want %q", block.Type, "tool_result")
			}
			if block.ToolUseID != tc.toolUseID {
				t.Errorf("tool_use_id = %q, want %q", block.ToolUseID, tc.toolUseID)
			}
			if !block.IsError {
				t.Errorf("is_error = false, want true (R-ZUM3-QUVT)")
			}
			var content string
			if err := json.Unmarshal(block.Content, &content); err != nil {
				t.Fatalf("content not a JSON string: %v (raw=%s)", err, block.Content)
			}
			if strings.TrimSpace(content) == "" {
				t.Errorf("content is empty; want a human-readable failure message")
			}
			if !strings.Contains(content, tc.path) {
				t.Errorf("content %q does not mention the offending path %q", content, tc.path)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
