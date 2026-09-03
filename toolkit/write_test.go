package toolkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestWriteConstructor(t *testing.T) {
	tool, err := Write(t.TempDir())
	// R-C5AY-4QLJ: Write constructs the exported Write tool for a valid root.
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Write() returned a nil tool")
	}
	if got := tool.Name(); got != "Write" {
		t.Errorf("tool name = %q, want %q", got, "Write")
	}
}

func TestWriteSchema(t *testing.T) {
	tool, err := Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want an object", schema["properties"])
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required = %#v, want an array", schema["required"])
	}
	requiredNames := make([]string, len(required))
	for index, value := range required {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("required[%d] = %#v, want a string", index, value)
		}
		requiredNames[index] = name
	}
	sort.Strings(requiredNames)

	// R-CXCM-XGNI: the schema has exactly two required string properties.
	if got, want := sortedKeys(properties), []string{"content", "file_path"}; !reflect.DeepEqual(got, want) {
		t.Errorf("property names = %q, want %q", got, want)
	}
	for _, name := range []string{"content", "file_path"} {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Errorf("property %q = %#v, want an object", name, properties[name])
			continue
		}
		if got := property["type"]; got != "string" {
			t.Errorf("%s type = %#v, want string", name, got)
		}
	}
	if want := []string{"content", "file_path"}; !reflect.DeepEqual(requiredNames, want) {
		t.Errorf("required = %q, want %q", requiredNames, want)
	}
}

func TestWriteCreatesAndReplacesFiles(t *testing.T) {
	t.Run("new file and parents", func(t *testing.T) {
		root := t.TempDir()
		tool, err := Write(root)
		if err != nil {
			t.Fatal(err)
		}
		filePath := filepath.Join("a", "b", "c", "new.txt")
		content := "new content"
		if _, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"content":%q}`, filePath, content))); err != nil {
			t.Fatalf("Call() error = %v", err)
		}

		path := filepath.Clean(filepath.Join(root, filePath))
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		// R-CYKJ-B8E7: missing parents and a mode-0644 file are created,
		// with the supplied content replacing the whole file.
		if got := string(contents); got != content {
			t.Errorf("file content = %q, want %q", got, content)
		}
		if got := stat.Mode().Perm(); got != 0o644 {
			t.Errorf("file mode = %#o, want %#o", got, os.FileMode(0o644))
		}
	})

	t.Run("existing file preserves mode", func(t *testing.T) {
		root := t.TempDir()
		filePath := "existing.txt"
		path := filepath.Clean(filepath.Join(root, filePath))
		if err := os.WriteFile(path, []byte("old content that is longer"), 0o600); err != nil {
			t.Fatal(err)
		}
		tool, err := Write(root)
		if err != nil {
			t.Fatal(err)
		}
		content := "new"
		if _, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"content":%q}`, filePath, content))); err != nil {
			t.Fatalf("Call() error = %v", err)
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		// R-CYKJ-B8E7: replacement truncates old content and preserves mode.
		if got := string(contents); got != content {
			t.Errorf("file content = %q, want %q", got, content)
		}
		if got := stat.Mode().Perm(); got != 0o600 {
			t.Errorf("file mode = %#o, want %#o", got, os.FileMode(0o600))
		}
	})
}

func TestWriteReportsByteCount(t *testing.T) {
	tool, err := Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filePath := "unicode.txt"
	content := "café"
	got, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"content":%q}`, filePath, content)))
	if err != nil {
		t.Fatal(err)
	}

	// R-CZSF-P04W: success reports the byte length and supplied path exactly.
	if want := fmt.Sprintf("wrote %d bytes to %s", len(content), filePath); got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}

func TestWriteRejectsDirectoryPath(t *testing.T) {
	root := t.TempDir()
	filePath := "existing-directory"
	if err := os.Mkdir(filepath.Join(root, filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	tool, err := Write(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"content":"value"}`, filePath)))
	// R-D10C-2RVL: an existing directory is rejected and the error names file_path.
	if err == nil || !strings.Contains(err.Error(), filePath) {
		t.Errorf("Call() error = %v, want error containing %q", err, filePath)
	}
}
