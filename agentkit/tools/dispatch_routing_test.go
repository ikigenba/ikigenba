package tools_test

// Coverage-gap tests (Task 1.3) for Dispatch's name routing and its
// malformed-input / unknown-tool branches. dispatch_confine_test.go already
// covers Read/Write/Edit confinement and the in-root write→read round-trip;
// this file fills the remaining routing arms (Bash, Glob, Grep), the
// per-tool "invalid input" decode failures, and the unknown-tool default, so
// the dispatcher wiki depends on is exercised end to end (every tool_use the
// model can emit reaches a correlatable tool_result, never tears down the loop).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentkit/tools"
	"agentkit/wire"
)

// TestDispatch_RoutesBashGlobGrep exercises the non-path tool arms, which the
// confine tests do not touch, proving Dispatch routes by tool name to each
// implementation and returns a non-error result on the happy path.
func TestDispatch_RoutesBashGlobGrep(t *testing.T) {
	root := t.TempDir()
	// A fixture file so Glob and Grep have something to find under the root.
	if err := os.WriteFile(filepath.Join(root, "match.txt"), []byte("needle here\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Run("Bash", func(t *testing.T) {
		block := mustToolUse(t, "b1", "Bash", map[string]any{"command": "printf routed-bash"})
		tr, sidecar, err := tools.Dispatch(context.Background(), root, block)
		if err != nil {
			t.Fatalf("Dispatch Bash: %v", err)
		}
		if tr.IsError {
			t.Fatalf("Bash flagged is_error: %q", resultContent(t, tr))
		}
		// Bash is the one tool that returns a sidecar (R-DPI6-73NQ).
		if sidecar == nil {
			t.Fatalf("Bash dispatch returned nil sidecar, want a BashSidecar")
		}
		if !strings.Contains(resultContent(t, tr), "routed-bash") {
			t.Fatalf("Bash result = %q, want it to contain routed-bash", resultContent(t, tr))
		}
	})

	t.Run("Glob", func(t *testing.T) {
		// Empty path defaults to the sandbox root (effectiveSearchPath).
		block := mustToolUse(t, "g1", "Glob", map[string]any{"pattern": "*.txt"})
		tr, sidecar, err := tools.Dispatch(context.Background(), root, block)
		if err != nil {
			t.Fatalf("Dispatch Glob: %v", err)
		}
		if tr.IsError {
			t.Fatalf("Glob flagged is_error: %q", resultContent(t, tr))
		}
		if sidecar != nil {
			t.Fatalf("Glob returned a sidecar, want nil")
		}
		if !strings.Contains(resultContent(t, tr), "match.txt") {
			t.Fatalf("Glob result = %q, want it to list match.txt", resultContent(t, tr))
		}
	})

	t.Run("Grep", func(t *testing.T) {
		block := mustToolUse(t, "gr1", "Grep", map[string]any{"pattern": "needle"})
		tr, sidecar, err := tools.Dispatch(context.Background(), root, block)
		if err != nil {
			t.Fatalf("Dispatch Grep: %v", err)
		}
		if tr.IsError {
			t.Fatalf("Grep flagged is_error: %q", resultContent(t, tr))
		}
		if sidecar != nil {
			t.Fatalf("Grep returned a sidecar, want nil")
		}
		if !strings.Contains(resultContent(t, tr), "match.txt") {
			t.Fatalf("Grep result = %q, want it to reference match.txt", resultContent(t, tr))
		}
	})
}

// TestDispatch_GlobGrepConfineEscape covers the search-path confinement arms of
// Glob and Grep, which differ from confinePath (effectiveSearchPath): a search
// path escaping the root is an is_error result, not a Go error.
func TestDispatch_GlobGrepConfineEscape(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		tool  string
		input any
	}{
		{"Glob", map[string]any{"pattern": "*", "path": "../"}},
		{"Grep", map[string]any{"pattern": "x", "path": "../"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			block := mustToolUse(t, "esc", tc.tool, tc.input)
			tr, _, err := tools.Dispatch(context.Background(), root, block)
			if err != nil {
				t.Fatalf("Dispatch %s returned Go error, want is_error block: %v", tc.tool, err)
			}
			if !tr.IsError {
				t.Fatalf("%s escape not flagged is_error; content=%q", tc.tool, resultContent(t, tr))
			}
			if !strings.Contains(resultContent(t, tr), "escapes sandbox") {
				t.Fatalf("%s error content = %q, want 'escapes sandbox'", tc.tool, resultContent(t, tr))
			}
		})
	}
}

// TestDispatch_InvalidInput pins the per-tool JSON-decode failure arm: a
// tool_use whose Input is not the shape the tool expects must come back as an
// is_error tool_result (naming the tool), never a Go error.
func TestDispatch_InvalidInput(t *testing.T) {
	root := t.TempDir()
	// Input is a JSON array where each tool expects an object → unmarshal fails
	// in every arm, exercising each "invalid input" branch.
	badInput := json.RawMessage(`[1,2,3]`)

	for _, name := range []string{"Bash", "Read", "Write", "Glob", "Grep", "Edit"} {
		t.Run(name, func(t *testing.T) {
			block := wire.ToolUseBlock{Type: "tool_use", ID: "bad", Name: name, Input: badInput}
			tr, sidecar, err := tools.Dispatch(context.Background(), root, block)
			if err != nil {
				t.Fatalf("Dispatch %s with bad input returned Go error: %v", name, err)
			}
			if !tr.IsError {
				t.Fatalf("%s bad input not flagged is_error; content=%q", name, resultContent(t, tr))
			}
			if !strings.Contains(resultContent(t, tr), "invalid input") {
				t.Fatalf("%s error content = %q, want 'invalid input'", name, resultContent(t, tr))
			}
			if sidecar != nil {
				t.Fatalf("%s invalid input returned a sidecar, want nil", name)
			}
		})
	}
}

// TestDispatch_UnknownTool covers the default arm: a tool_use naming a tool the
// dispatcher does not implement comes back as an is_error result naming the
// unknown tool, so an off-menu call from the model is correlatable rather than
// fatal.
func TestDispatch_UnknownTool(t *testing.T) {
	block := mustToolUse(t, "u1", "NoSuchTool", map[string]any{"x": 1})
	tr, sidecar, err := tools.Dispatch(context.Background(), t.TempDir(), block)
	if err != nil {
		t.Fatalf("Dispatch unknown tool returned Go error: %v", err)
	}
	if !tr.IsError {
		t.Fatalf("unknown tool not flagged is_error; content=%q", resultContent(t, tr))
	}
	if !strings.Contains(resultContent(t, tr), "unknown tool") {
		t.Fatalf("unknown-tool result = %q, want it to mention 'unknown tool'", resultContent(t, tr))
	}
	if sidecar != nil {
		t.Fatalf("unknown tool returned a sidecar, want nil")
	}
}
