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

func TestEditConstructor(t *testing.T) {
	tool, err := Edit(t.TempDir())
	// R-C6IU-IIC8: Edit constructs the exported Edit tool for a valid root.
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Edit() returned a nil tool")
	}
	if got := tool.Name(); got != "Edit" {
		t.Errorf("tool name = %q, want %q", got, "Edit")
	}
}

func TestEditSchema(t *testing.T) {
	tool, err := Edit(t.TempDir())
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

	// R-D288-GJMA: the schema declares exactly three required strings and one optional boolean.
	if got, want := sortedKeys(properties), []string{"file_path", "new_string", "old_string", "replace_all"}; !reflect.DeepEqual(got, want) {
		t.Errorf("property names = %q, want %q", got, want)
	}
	for _, name := range []string{"file_path", "new_string", "old_string"} {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Errorf("property %q = %#v, want an object", name, properties[name])
			continue
		}
		if got := property["type"]; got != "string" {
			t.Errorf("%s type = %#v, want string", name, got)
		}
	}
	if got := properties["old_string"].(map[string]any)["minLength"]; got != float64(1) {
		t.Errorf("old_string minLength = %#v, want 1", got)
	}
	if got := properties["replace_all"].(map[string]any)["type"]; got != "boolean" {
		t.Errorf("replace_all type = %#v, want boolean", got)
	}
	if want := []string{"file_path", "new_string", "old_string"}; !reflect.DeepEqual(requiredNames, want) {
		t.Errorf("required = %q, want %q", requiredNames, want)
	}
}

func TestEditRejectsIdenticalStrings(t *testing.T) {
	root := t.TempDir()
	filePath := "same.txt"
	path := filepath.Clean(filepath.Join(root, filePath))
	want := "leave this unchanged"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Edit(root)
	if err != nil {
		t.Fatal(err)
	}

	_, callErr := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"old_string":"this","new_string":"this"}`, filePath)))
	contents, readErr := os.ReadFile(path)
	// R-D4O1-833O: identical old and new strings fail before changing the file.
	if callErr == nil {
		t.Error("Call() error = nil, want an error")
	}
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if got := string(contents); got != want {
		t.Errorf("file content = %q, want unchanged %q", got, want)
	}
}

func TestEditRejectsInvalidFilePath(t *testing.T) {
	root := t.TempDir()
	tool, err := Edit(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		filePath string
		prepare  func(string) error
	}{
		{name: "missing", filePath: "missing.txt", prepare: func(string) error { return nil }},
		{name: "directory", filePath: "a-directory", prepare: func(path string) error { return os.Mkdir(path, 0o700) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.prepare(filepath.Join(root, test.filePath)); err != nil {
				t.Fatal(err)
			}
			_, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"old_string":"old","new_string":"new"}`, test.filePath)))
			// R-D5VX-LUUD: missing paths and directories fail with the supplied file_path.
			if err == nil || !strings.Contains(err.Error(), test.filePath) {
				t.Errorf("Call() error = %v, want error containing %q", err, test.filePath)
			}
		})
	}
}

func TestEditRequiresExactMatch(t *testing.T) {
	root := t.TempDir()
	filePath := "exact.txt"
	path := filepath.Clean(filepath.Join(root, filePath))
	want := "needle\n"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Edit(root)
	if err != nil {
		t.Fatal(err)
	}

	_, callErr := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"old_string":"needle ","new_string":"replacement"}`, filePath)))
	contents, readErr := os.ReadFile(path)
	// R-D73T-ZML2: matching is exact, and no match reports the specified error without writing.
	if callErr == nil || !strings.Contains(callErr.Error(), "old_string not found") {
		t.Errorf("Call() error = %v, want error containing old_string not found", callErr)
	}
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if got := string(contents); got != want {
		t.Errorf("file content = %q, want unchanged %q", got, want)
	}
}

func TestEditRejectsAmbiguousMatch(t *testing.T) {
	root := t.TempDir()
	filePath := "ambiguous.txt"
	path := filepath.Clean(filepath.Join(root, filePath))
	want := "old middle old"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Edit(root)
	if err != nil {
		t.Fatal(err)
	}

	_, callErr := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"old_string":"old","new_string":"new"}`, filePath)))
	contents, readErr := os.ReadFile(path)
	// R-D8BQ-DEBR: an ambiguous default edit reports its count without writing.
	if callErr == nil || !strings.Contains(callErr.Error(), "2") {
		t.Errorf("Call() error = %v, want error containing occurrence count 2", callErr)
	}
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if got := string(contents); got != want {
		t.Errorf("file content = %q, want unchanged %q", got, want)
	}
}

func TestEditReplacesAndPreservesMode(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		oldString  string
		newString  string
		replaceAll bool
		want       string
		wantCount  int
	}{
		{name: "single default", content: "before old after", oldString: "old", newString: "new", want: "before new after", wantCount: 1},
		{name: "all occurrences", content: "old middle old", oldString: "old", newString: "new", replaceAll: true, want: "new middle new", wantCount: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			filePath := "editable.txt"
			path := filepath.Clean(filepath.Join(root, filePath))
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			tool, err := Edit(root)
			if err != nil {
				t.Fatal(err)
			}
			args := fmt.Sprintf(`{"file_path":%q,"old_string":%q,"new_string":%q,"replace_all":%t}`, filePath, test.oldString, test.newString, test.replaceAll)
			got, err := tool.Call(context.Background(), json.RawMessage(args))
			if err != nil {
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

			// R-D9JM-R62G: one or all matches are replaced, mode is preserved, and the result reports the count and supplied path.
			if gotContent := string(contents); gotContent != test.want {
				t.Errorf("file content = %q, want %q", gotContent, test.want)
			}
			if gotMode := stat.Mode().Perm(); gotMode != 0o600 {
				t.Errorf("file mode = %#o, want %#o", gotMode, os.FileMode(0o600))
			}
			if wantResult := fmt.Sprintf("replaced %d occurrence(s) of old_string in %s", test.wantCount, filePath); got != wantResult {
				t.Errorf("Call() = %q, want %q", got, wantResult)
			}
		})
	}
}
