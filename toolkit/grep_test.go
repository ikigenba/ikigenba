package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGrepConstructor(t *testing.T) {
	tool, err := Grep(t.TempDir())
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Grep() returned a nil tool")
	}
	if got, want := tool.Name(), "Grep"; got != want {
		t.Errorf("tool.Name() = %q, want %q", got, want)
	}
	if _, err := Grep(t.TempDir(), WithSkip("x")); err != nil {
		t.Fatalf("Grep() with option error = %v", err)
	}
	// R-C8YN-A1TM
}

func TestGrepSchema(t *testing.T) {
	tool, err := Grep(t.TempDir())
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", schema["properties"])
	}
	wantNames := []string{"-A", "-B", "-C", "-i", "-n", "glob", "head_limit", "multiline", "output_mode", "path", "pattern"}
	if got := sortedKeys(properties); !reflect.DeepEqual(got, wantNames) {
		t.Errorf("property names = %v, want %v", got, wantNames)
	}

	assertSchemaProperty(t, properties, "pattern", "string", "minLength", float64(1))
	assertSchemaProperty(t, properties, "path", "string", "", nil)
	assertSchemaProperty(t, properties, "glob", "string", "", nil)
	assertSchemaProperty(t, properties, "-i", "boolean", "", nil)
	assertSchemaProperty(t, properties, "-n", "boolean", "", nil)
	for _, name := range []string{"-A", "-B", "-C"} {
		assertSchemaProperty(t, properties, name, "integer", "minimum", float64(0))
	}
	assertSchemaProperty(t, properties, "multiline", "boolean", "", nil)
	assertSchemaProperty(t, properties, "head_limit", "integer", "minimum", float64(1))

	outputMode := properties["output_mode"].(map[string]any)
	if got := outputMode["type"]; got != "string" {
		t.Errorf("output_mode type = %#v, want string", got)
	}
	if got, want := outputMode["enum"], []any{"files_with_matches", "content", "count"}; !reflect.DeepEqual(got, want) {
		t.Errorf("output_mode enum = %#v, want %#v", got, want)
	}
	if got, want := schema["required"], []any{"pattern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("required = %#v, want %#v", got, want)
	}
	// R-DMYI-YN83
}

func TestGrepRegularExpression(t *testing.T) {
	t.Run("ignore case", func(t *testing.T) {
		root := t.TempDir()
		matchPath := filepath.Join(root, "mixed.txt")
		if err := os.WriteFile(matchPath, []byte("Hello, World!"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		tool, err := Grep(root)
		if err != nil {
			t.Fatalf("Grep() error = %v", err)
		}

		got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"hello"}`))
		if err != nil {
			t.Fatalf("case-sensitive Grep call error = %v", err)
		}
		if got != "No matches found" {
			t.Errorf("case-sensitive result = %q, want no matches", got)
		}

		got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"hello","-i":true}`))
		if err != nil {
			t.Fatalf("case-insensitive Grep call error = %v", err)
		}
		if got != matchPath {
			t.Errorf("case-insensitive result = %q, want %q", got, matchPath)
		}
	})

	t.Run("invalid pattern", func(t *testing.T) {
		tool, err := Grep(t.TempDir())
		if err != nil {
			t.Fatalf("Grep() error = %v", err)
		}
		_, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"a(b"}`))
		if err == nil {
			t.Fatal("Grep invalid pattern returned nil error")
		}
		if !strings.Contains(err.Error(), "pattern") {
			t.Errorf("Grep error = %q, want it to name pattern", err)
		}
	})
	// R-DPEB-Q6PH
}

func TestGrepFilesWithMatches(t *testing.T) {
	root := t.TempDir()
	fixtures := map[string]string{
		"zzz.txt": "needle at the end",
		"aaa.txt": "needle at the start",
		"mmm.txt": "unrelated contents",
	}
	for name, contents := range fixtures {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}
	want := strings.Join([]string{filepath.Join(root, "aaa.txt"), filepath.Join(root, "zzz.txt")}, "\n")
	for name, input := range map[string]string{
		"default":            `{"pattern":"needle"}`,
		"files_with_matches": `{"pattern":"needle","output_mode":"files_with_matches"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := tool.Call(context.Background(), json.RawMessage(input))
			if err != nil {
				t.Fatalf("Grep call error = %v", err)
			}
			if got != want {
				t.Errorf("Grep result = %q, want %q", got, want)
			}
			if strings.Contains(got, filepath.Join(root, "mmm.txt")) {
				t.Errorf("Grep result contains non-matching file: %q", got)
			}
			for _, path := range strings.Split(got, "\n") {
				if !filepath.IsAbs(path) {
					t.Errorf("Grep path %q is not absolute", path)
				}
			}
		})
	}
	// R-DU9X-99O9
}

func TestGrepNoMatchesFound(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "present.txt"), []byte("haystack"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatalf("Grep call error = %v", err)
	}
	if got != "No matches found" {
		t.Errorf("Grep no-match result = %q, want %q", got, "No matches found")
	}
	// R-EL3P-O7ZJ
}

func assertSchemaProperty(t *testing.T, properties map[string]any, name, wantType, constraint string, wantConstraint any) {
	t.Helper()
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("%s property = %#v", name, properties[name])
	}
	if got := property["type"]; got != wantType {
		t.Errorf("%s type = %#v, want %s", name, got, wantType)
	}
	if constraint != "" {
		if got := property[constraint]; got != wantConstraint {
			t.Errorf("%s %s = %#v, want %#v", name, constraint, got, wantConstraint)
		}
	}
}
