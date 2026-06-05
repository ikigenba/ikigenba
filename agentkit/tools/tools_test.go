package tools_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"agentkit/tools"
)

// R-B9P4-41S7: every implemented tool must be advertised on every
// request. The registry is the single source the driver iterates,
// so this test pins it to contain exactly the set of tool
// subpackages that exist under internal/tools/. If a new tool
// package lands without being registered here, this test fails;
// likewise if a tool is registered without backing implementation.
func TestR_B9P4_41S7_RegistryOffersAllImplementedTools(t *testing.T) {
	got := tools.All()
	if len(got) == 0 {
		t.Fatalf("tools.All() returned empty registry; R-B9P4-41S7 requires every implemented tool to be offered")
	}

	gotNames := make([]string, 0, len(got))
	seen := make(map[string]bool)
	for _, d := range got {
		if d.Name == "" {
			t.Fatalf("registry entry has empty Name")
		}
		if seen[d.Name] {
			t.Fatalf("duplicate tool in registry: %s", d.Name)
		}
		seen[d.Name] = true
		gotNames = append(gotNames, d.Name)

		if len(d.InputSchema) == 0 {
			t.Fatalf("tool %q has empty InputSchema", d.Name)
		}
		var probe any
		if err := json.Unmarshal(d.InputSchema, &probe); err != nil {
			t.Fatalf("tool %q InputSchema is not valid JSON: %v", d.Name, err)
		}
	}

	wantNames := implementedToolNames(t)
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	if len(gotNames) != len(wantNames) {
		t.Fatalf("registry mismatch: got %v, want %v (every internal/tools/<pkg> must be advertised; R-B9P4-41S7)", gotNames, wantNames)
	}
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Fatalf("registry mismatch: got %v, want %v (R-B9P4-41S7)", gotNames, wantNames)
		}
	}
}

// implementedToolNames returns the canonical Name for every tool
// subpackage under internal/tools/. The mapping subpackage->Name is
// hard-coded because Go has no compile-time directory iteration; if
// a new tool subpackage is added (which R-AQ6C-0C5B forbids in v1
// but the layout test enforces separately), this list must grow
// alongside the registry, and the dir scan below catches the
// directory side of that change.
func implementedToolNames(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	toolsDir := filepath.Join(root, "tools")
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		t.Fatalf("read tools: %v", err)
	}
	expected := map[string]string{
		"read":  "Read",
		"bash":  "Bash",
		"write": "Write",
		"edit":  "Edit",
		"glob":  "Glob",
		"grep":  "Grep",
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name, ok := expected[e.Name()]
		if !ok {
			t.Fatalf("internal/tools/%s present but no canonical Name mapped in this test; update implementedToolNames and tools.All() together (R-B9P4-41S7)", e.Name())
		}
		names = append(names, name)
	}
	return names
}

// R-YFCR-J9IL: tools.Select filters All() to the comma-separated names in
// the --tools flag value. Empty = all tools; whitespace and empty elements
// are tolerated; an unknown name is a fatal error listing registered names.
func TestR_YFCR_J9IL_Select(t *testing.T) {
	all := tools.All()
	allNames := make(map[string]bool, len(all))
	for _, d := range all {
		allNames[d.Name] = true
	}

	t.Run("empty_flag_returns_all", func(t *testing.T) {
		got, err := tools.Select("")
		if err != nil {
			t.Fatalf("Select(%q): unexpected error: %v", "", err)
		}
		if len(got) != len(all) {
			t.Errorf("got %d tools, want %d", len(got), len(all))
		}
	})

	t.Run("whitespace_only_returns_all", func(t *testing.T) {
		got, err := tools.Select("   ")
		if err != nil {
			t.Fatalf("Select(spaces): unexpected error: %v", err)
		}
		if len(got) != len(all) {
			t.Errorf("got %d tools, want %d", len(got), len(all))
		}
	})

	t.Run("single_known_tool", func(t *testing.T) {
		got, err := tools.Select("Bash")
		if err != nil {
			t.Fatalf("Select(Bash): %v", err)
		}
		if len(got) != 1 || got[0].Name != "Bash" {
			t.Errorf("got %v, want [{Bash ...}]", got)
		}
	})

	t.Run("subset_selection", func(t *testing.T) {
		got, err := tools.Select("Read,Bash")
		if err != nil {
			t.Fatalf("Select(Read,Bash): %v", err)
		}
		if len(got) != 2 {
			t.Errorf("got %d tools, want 2", len(got))
		}
	})

	t.Run("whitespace_around_commas_tolerated", func(t *testing.T) {
		got, err := tools.Select(" Read , Bash ")
		if err != nil {
			t.Fatalf("Select with spaces: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("got %d tools, want 2", len(got))
		}
	})

	t.Run("trailing_comma_tolerated", func(t *testing.T) {
		got, err := tools.Select("Bash,")
		if err != nil {
			t.Fatalf("Select(Bash,): %v", err)
		}
		if len(got) != 1 || got[0].Name != "Bash" {
			t.Errorf("got %v, want [{Bash ...}]", got)
		}
	})

	t.Run("leading_comma_tolerated", func(t *testing.T) {
		got, err := tools.Select(",Bash")
		if err != nil {
			t.Fatalf("Select(,Bash): %v", err)
		}
		if len(got) != 1 || got[0].Name != "Bash" {
			t.Errorf("got %v, want [{Bash ...}]", got)
		}
	})

	t.Run("duplicate_name_deduped", func(t *testing.T) {
		got, err := tools.Select("Bash,Bash")
		if err != nil {
			t.Fatalf("Select(Bash,Bash): %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d tools, want 1 (duplicates deduped)", len(got))
		}
	})

	t.Run("unknown_name_is_error", func(t *testing.T) {
		_, err := tools.Select("Bash,NoSuchTool")
		if err == nil {
			t.Fatalf("expected error for unknown tool, got nil")
		}
		if !strings.Contains(err.Error(), "NoSuchTool") {
			t.Errorf("error should mention the unknown name; got: %v", err)
		}
		// Error message must list the registered tool names.
		for _, d := range all {
			if !strings.Contains(err.Error(), d.Name) {
				t.Errorf("error should list registered tool %q; got: %v", d.Name, err)
			}
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}
