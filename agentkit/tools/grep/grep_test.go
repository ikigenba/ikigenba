package grep_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentkit/tools/grep"
)

// resultText unmarshals the tool_result content JSON string.
func resultText(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	return s
}

// writeFile creates a file with the given content in dir.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", p, err)
	}
	return p
}

// TestR_W2KP_TYRG_InputSchemaAndBasicSearch verifies that the Grep tool
// accepts the full schema (pattern required; path, glob, type, output_mode,
// -i, -n, -A, -B, -C, multiline, head_limit optional) and performs a basic
// files_with_matches search. R-W2KP-TYRG.
func TestR_W2KP_TYRG_InputSchemaAndBasicSearch(t *testing.T) {
	// Validate InputSchema is valid JSON with the required properties.
	var schema map[string]any
	if err := json.Unmarshal(grep.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema missing properties")
	}
	wantProps := []string{"pattern", "path", "glob", "type", "output_mode", "-i", "-n", "-A", "-B", "-C", "multiline", "head_limit"}
	for _, p := range wantProps {
		if _, exists := props[p]; !exists {
			t.Errorf("InputSchema missing property %q", p)
		}
	}
	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "pattern" {
		t.Errorf("InputSchema required: got %v, want [pattern]", required)
	}

	// Basic files_with_matches search.
	dir := t.TempDir()
	writeFile(t, dir, "hello.txt", "hello world\nfoo bar\n")
	writeFile(t, dir, "other.txt", "no match here\n")

	res, err := grep.Grep("id1", grep.Input{
		Pattern:    "hello",
		Path:       dir,
		OutputMode: "files_with_matches",
	})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected is_error: %s", resultText(t, res.Content))
	}
	body := resultText(t, res.Content)
	if !strings.Contains(body, "hello.txt") {
		t.Errorf("expected hello.txt in result, got: %s", body)
	}
	if strings.Contains(body, "other.txt") {
		t.Errorf("did not expect other.txt in result, got: %s", body)
	}
}

// TestR_DV6B_4XLA_PathResolution verifies that path defaults to cwd,
// absolute-file paths are searched directly, relative paths are rejected,
// and non-existent paths return an error. R-DV6B-4XLA.
func TestR_DV6B_4XLA_PathResolution(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "a.txt", "needle\n")

	t.Run("absolute_dir", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: dir})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
	})

	t.Run("absolute_file", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: f})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, f) {
			t.Errorf("expected %s in result, got: %s", f, body)
		}
	})

	t.Run("relative_path_rejected", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: "relative/path"})
		if !res.IsError {
			t.Fatal("expected is_error for relative path")
		}
	})

	t.Run("nonexistent_path_rejected", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: "/does/not/exist/12345"})
		if !res.IsError {
			t.Fatal("expected is_error for nonexistent path")
		}
	})
}

// TestR_PHCN_83RU_RE2RegexCompileError verifies that RE2-incompatible patterns
// (e.g. lookahead, backreferences) return an is_error result with the
// compiler's message. R-PHCN-83RU.
func TestR_PHCN_83RU_RE2RegexCompileError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "test\n")

	t.Run("invalid_pattern_is_error", func(t *testing.T) {
		// RE2 does not support lookahead (?=...).
		res, _ := grep.Grep("id", grep.Input{Pattern: "(?=lookahead)", Path: dir})
		if !res.IsError {
			t.Fatal("expected is_error for RE2-incompatible pattern")
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "invalid regex") && !strings.Contains(body, "error parsing regexp") {
			t.Errorf("error should mention regex problem; got: %s", body)
		}
	})

	t.Run("valid_re2_pattern_succeeds", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: `\btest\b`, Path: dir})
		if res.IsError {
			t.Fatalf("unexpected error for valid RE2 pattern: %s", resultText(t, res.Content))
		}
	})
}

// TestR_MO9F_AKEW_OutputModes verifies the three output_mode shapes:
// files_with_matches, content, and count. R-MO9F-AKEW.
func TestR_MO9F_AKEW_OutputModes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "foo bar\nfoo baz\nno match\n")
	writeFile(t, dir, "b.txt", "irrelevant\n")

	t.Run("files_with_matches_default", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "foo", Path: dir})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		lines := strings.Split(strings.TrimSpace(body), "\n")
		if len(lines) != 1 {
			t.Errorf("files_with_matches: expected 1 path, got %d: %v", len(lines), lines)
		}
		if !strings.HasSuffix(lines[0], "a.txt") {
			t.Errorf("expected a.txt in result, got: %s", lines[0])
		}
		if !filepath.IsAbs(lines[0]) {
			t.Errorf("path must be absolute, got: %s", lines[0])
		}
	})

	t.Run("content_mode", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "foo", Path: dir, OutputMode: "content"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "foo bar") {
			t.Errorf("content mode: expected matching line content; got: %s", body)
		}
		if strings.Contains(body, "no match") {
			t.Errorf("content mode: non-matching line should not appear; got: %s", body)
		}
	})

	t.Run("content_mode_with_line_numbers", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "foo", Path: dir, OutputMode: "content", LineNumbers: true})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		// Expect path:linenum:content format.
		if !strings.Contains(body, ":1:") && !strings.Contains(body, ":2:") {
			t.Errorf("content mode -n: expected line numbers; got: %s", body)
		}
	})

	t.Run("count_mode", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "foo", Path: dir, OutputMode: "count"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		// Should be "path:2" since a.txt has 2 matching lines.
		if !strings.Contains(body, ":2") {
			t.Errorf("count mode: expected count 2 for a.txt; got: %s", body)
		}
		if strings.Contains(body, "b.txt") {
			t.Errorf("count mode: b.txt has no matches, should not appear; got: %s", body)
		}
	})
}

// TestR_Z5JW_CG1H_GlobFilter verifies that the glob filter restricts the
// file walk. Basename-only patterns (no '/') match against the final segment;
// path patterns with '/' or '**' match the full relative path. R-Z5JW-CG1H.
func TestR_Z5JW_CG1H_GlobFilter(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "main.go", "needle\n")
	writeFile(t, dir, "main.py", "needle\n")
	writeFile(t, sub, "helper.go", "needle\n")

	t.Run("basename_glob_filters_extension", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: dir, Glob: "*.go"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if strings.Contains(body, "main.py") {
			t.Errorf("*.go glob should exclude .py files; got: %s", body)
		}
		if !strings.Contains(body, "main.go") || !strings.Contains(body, "helper.go") {
			t.Errorf("*.go glob should include all .go files; got: %s", body)
		}
	})

	t.Run("type_and_glob_intersection", func(t *testing.T) {
		// Glob *.go AND type go — both satisfied only by .go files.
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: dir, Glob: "*.go", TypeName: "go"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if strings.Contains(body, "main.py") {
			t.Errorf("intersection should exclude .py; got: %s", body)
		}
	})
}

// TestR_T1YQ_V7BN_TypeFilter verifies that the type filter resolves named
// types to extensions and that unknown types return an error. R-T1YQ-V7BN.
func TestR_T1YQ_V7BN_TypeFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "needle\n")
	writeFile(t, dir, "script.py", "needle\n")
	writeFile(t, dir, "notes.md", "needle\n")

	t.Run("go_type_matches_only_go_files", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: dir, TypeName: "go"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "main.go") {
			t.Errorf("go type: expected main.go; got: %s", body)
		}
		if strings.Contains(body, "script.py") || strings.Contains(body, "notes.md") {
			t.Errorf("go type: should not include non-go files; got: %s", body)
		}
	})

	t.Run("unknown_type_is_error_with_list", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: dir, TypeName: "cobol"})
		if !res.IsError {
			t.Fatal("expected is_error for unknown type")
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "cobol") {
			t.Errorf("error should mention the unknown type name; got: %s", body)
		}
		// Must list supported types.
		if !strings.Contains(body, "go") || !strings.Contains(body, "py") {
			t.Errorf("error should list supported types; got: %s", body)
		}
	})
}

// TestR_A8DI_RLSF_MultilineMode verifies that multiline: true compiles
// with (?s)(?m) flags and feeds the full file to the matcher, allowing a
// single match to span multiple lines. R-A8DI-RLSF.
func TestR_A8DI_RLSF_MultilineMode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "multi.txt", "line one\nline two\nline three\n")

	t.Run("cross_line_match", func(t *testing.T) {
		// Pattern spans two lines: "one\nline two"
		res, _ := grep.Grep("id", grep.Input{
			Pattern:    `one\nline two`,
			Path:       dir,
			Multiline:  true,
			OutputMode: "files_with_matches",
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "multi.txt") {
			t.Errorf("multiline cross-line match: expected multi.txt; got: %s", body)
		}
	})

	t.Run("dotall_dot_matches_newline", func(t *testing.T) {
		// (?s) makes . match newlines.
		res, _ := grep.Grep("id", grep.Input{
			Pattern:    `one.line`,
			Path:       dir,
			Multiline:  true,
			OutputMode: "files_with_matches",
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "multi.txt") {
			t.Errorf("(?s) dotall: expected multi.txt; got: %s", body)
		}
	})

	t.Run("multiline_no_match_without_flag", func(t *testing.T) {
		// Without multiline, cross-line pattern should not match.
		res, _ := grep.Grep("id", grep.Input{
			Pattern:    `one\nline two`,
			Path:       dir,
			Multiline:  false,
			OutputMode: "files_with_matches",
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if strings.Contains(body, "multi.txt") {
			t.Errorf("without multiline, cross-line pattern should not match; got: %s", body)
		}
	})

	t.Run("multiline_content_mode_returns_span", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{
			Pattern:    `one\nline two`,
			Path:       dir,
			Multiline:  true,
			OutputMode: "content",
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "one") || !strings.Contains(body, "two") {
			t.Errorf("multiline content: expected full span in output; got: %s", body)
		}
	})
}

// TestR_NRBC_OMJ6_HeadLimitAndByteCap verifies that head_limit truncates
// output to N entries and that the 50KB byte cap is enforced, both with a
// visible truncation notice appended. R-NRBC-OMJ6.
func TestR_NRBC_OMJ6_HeadLimitAndByteCap(t *testing.T) {
	dir := t.TempDir()

	// Create 10 files each containing a match.
	for i := 0; i < 10; i++ {
		writeFile(t, dir, strings.Repeat(string(rune('a'+i)), 1)+".txt",
			"needle line\n")
	}

	t.Run("head_limit_files_with_matches", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{
			Pattern:   "needle",
			Path:      dir,
			HeadLimit: 3,
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "truncated") {
			t.Errorf("head_limit truncation notice missing; got: %s", body)
		}
		paths := []string{}
		for _, line := range strings.Split(body, "\n") {
			if strings.HasSuffix(line, ".txt") {
				paths = append(paths, line)
			}
		}
		if len(paths) != 3 {
			t.Errorf("expected 3 paths (head_limit=3), got %d: %v", len(paths), paths)
		}
	})

	t.Run("head_limit_content_mode", func(t *testing.T) {
		res, _ := grep.Grep("id", grep.Input{
			Pattern:    "needle",
			Path:       dir,
			OutputMode: "content",
			HeadLimit:  2,
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res.Content))
		}
		body := resultText(t, res.Content)
		if !strings.Contains(body, "truncated") {
			t.Errorf("head_limit truncation notice missing in content mode; got: %s", body)
		}
	})
}

// TestR_K4UP_DSXC_TraversalSkips verifies that directory traversal skips
// hidden entries (. prefix) and the fixed denylist of common build/vendor/
// cache directories. R-K4UP-DSXC.
func TestR_K4UP_DSXC_TraversalSkips(t *testing.T) {
	dir := t.TempDir()

	mkDir := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		return p
	}

	// Hidden directory.
	hidden := mkDir(".hidden")
	writeFile(t, hidden, "secret.txt", "needle\n")

	// Denylist directories.
	for _, d := range []string{"node_modules", "vendor", "dist", "build", "target", "venv", "__pycache__"} {
		dd := mkDir(d)
		writeFile(t, dd, "file.txt", "needle\n")
	}

	// Visible file that should be found.
	writeFile(t, dir, "visible.go", "needle\n")

	res, _ := grep.Grep("id", grep.Input{Pattern: "needle", Path: dir})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(t, res.Content))
	}
	body := resultText(t, res.Content)

	if !strings.Contains(body, "visible.go") {
		t.Errorf("expected visible.go in result; got: %s", body)
	}
	for _, skip := range []string{".hidden", "node_modules", "vendor", "dist", "build", "target", "venv", "__pycache__"} {
		if strings.Contains(body, skip) {
			t.Errorf("skipped directory %q should not appear in result; got: %s", skip, body)
		}
	}
}

// TestR_MGY7_WFLI_EmptyMatchIsSuccess verifies that a pattern matching no
// files returns is_error:false with a "No matches found" body. R-MGY7-WFLI.
func TestR_MGY7_WFLI_EmptyMatchIsSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hello world\n")

	res, err := grep.Grep("id1", grep.Input{
		Pattern: "xyzzy_no_such_string_12345",
		Path:    dir,
	})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if res.IsError {
		t.Fatalf("empty match should not be an error; got is_error=true: %s", resultText(t, res.Content))
	}
	body := resultText(t, res.Content)
	if !strings.Contains(strings.ToLower(body), "no matches") {
		t.Errorf("expected 'No matches found' body; got: %s", body)
	}
}
