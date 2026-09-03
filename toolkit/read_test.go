package toolkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestReadConstructorAndSchema(t *testing.T) {
	tool, err := Read(t.TempDir())
	// R-C431-QYUU: Read constructs the exported Read tool for a valid root.
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Read() returned a nil tool")
	}
	if got := tool.Name(); got != "Read" {
		t.Errorf("tool name = %q, want %q", got, "Read")
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

	// R-CMDJ-HIZ9: the schema has exactly the required path and two optional,
	// minimum-one integer paging properties.
	if got, want := sortedKeys(properties), []string{"file_path", "limit", "offset"}; !reflect.DeepEqual(got, want) {
		t.Errorf("property names = %q, want %q", got, want)
	}
	if got := properties["file_path"].(map[string]any)["type"]; got != "string" {
		t.Errorf("file_path type = %#v, want string", got)
	}
	if !reflect.DeepEqual(required, []any{"file_path"}) {
		t.Errorf("required = %#v, want only file_path", required)
	}
	for _, name := range []string{"offset", "limit"} {
		property := properties[name].(map[string]any)
		if got := property["type"]; got != "integer" {
			t.Errorf("%s type = %#v, want integer", name, got)
		}
		if got := property["minimum"]; got != float64(1) {
			t.Errorf("%s minimum = %#v, want 1", name, got)
		}
	}
}

func TestReadRendersNumberedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"file_path":"short.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	// R-CNLF-VAPY: every source line has its one-based number in a six-wide
	// field, followed by a literal tab and content without the terminator.
	want := "     1\talpha\n     2\tbeta\n     3\tgamma"
	if got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}

	manyLines := strings.Repeat("value\n", 1000)
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(manyLines), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = tool.Call(context.Background(), json.RawMessage(`{"file_path":"many.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	rendered := strings.Split(got, "\n")
	for index, wantLine := range map[int]string{
		0:   "     1\tvalue",
		99:  "   100\tvalue",
		999: "  1000\tvalue",
	} {
		if gotLine := rendered[index]; gotLine != wantLine {
			t.Errorf("rendered line %d = %q, want %q", index+1, gotLine, wantLine)
		}
	}
}

func TestReadSlicesByOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	for line := 1; line <= 10; line++ {
		fmt.Fprintf(&content, "line-%d\n", line)
	}
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args string
		want string
	}{
		{
			name: "defaults",
			args: `{"file_path":"lines.txt"}`,
			want: numberedRange(1, 10),
		},
		{
			name: "explicit offset",
			args: `{"file_path":"lines.txt","offset":3}`,
			want: numberedRange(3, 10),
		},
		{
			name: "explicit offset and limit",
			args: `{"file_path":"lines.txt","offset":3,"limit":2}`,
			want: numberedRange(3, 4) + "\n[showing lines 3-4 of 10]",
		},
		{
			name: "limit extends past EOF",
			args: `{"file_path":"lines.txt","offset":8,"limit":5}`,
			want: numberedRange(8, 10),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tool.Call(context.Background(), json.RawMessage(test.args))
			if err != nil {
				t.Fatal(err)
			}
			// R-COTC-92GN: absent paging values use their defaults, explicit values
			// select the inclusive numbered range, and the result clamps at EOF.
			if got != test.want {
				t.Errorf("Call() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReadReportsPartialRange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "five.txt"), []byte("one\ntwo\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"file_path":"five.txt","limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	// R-CQ18-MU7C: partial reads report the actual range and total line count.
	if !strings.HasSuffix(got, "\n[showing lines 1-3 of 5]") {
		t.Errorf("partial Call() = %q, want range trailer", got)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"file_path":"five.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "[showing lines") {
		t.Errorf("complete Call() = %q, want no range trailer", got)
	}
}

func TestReadTruncatesLongLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(strings.Repeat("x", 2500)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"file_path":"long.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	// R-CR95-0LY1: only the first 2000 bytes of a long source line are rendered.
	want := "     1\t" + strings.Repeat("x", 2000) + " [line truncated]"
	if got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}

func TestReadReportsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"file_path":"empty.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	// R-CSH1-EDOQ: a zero-length file has a dedicated exact response.
	if want := "empty.txt is an empty file"; got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}

func TestReadRejectsMissingAndDirectoryPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, filePath := range []string{"missing.txt", "folder"} {
		t.Run(filePath, func(t *testing.T) {
			_, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q}`, filePath)))
			// R-CTOX-S5FF: missing and directory errors name the supplied file_path.
			if err == nil || !strings.Contains(err.Error(), filePath) {
				t.Errorf("Call() error = %v, want error containing %q", err, filePath)
			}
		})
	}
}

func TestReadRejectsOffsetPastEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "three.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Call(context.Background(), json.RawMessage(`{"file_path":"three.txt","offset":10}`))
	// R-CUWU-5X64: an offset beyond a non-empty file errors and names file_path.
	if err == nil || !strings.Contains(err.Error(), "three.txt") {
		t.Errorf("Call() error = %v, want error containing %q", err, "three.txt")
	}
}

func TestReadRejectsNonTextContent(t *testing.T) {
	dir := t.TempDir()
	tool, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		contents []byte
	}{
		{name: "nul", contents: []byte("abc\x00def")},
		{name: "invalid UTF-8", contents: []byte{0x61, 0xc3, 0x28}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filePath := test.name + ".txt"
			if err := os.WriteFile(filepath.Join(dir, filePath), test.contents, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q}`, filePath)))
			// R-CW4Q-JOWT: non-text input returns no content and an error naming file_path.
			if err == nil || !strings.Contains(err.Error(), filePath) {
				t.Errorf("Call() error = %v, want error containing %q", err, filePath)
			}
			if got != "" {
				t.Errorf("Call() result = %q, want empty string", got)
			}
		})
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func numberedRange(first, last int) string {
	lines := make([]string, 0, last-first+1)
	for line := first; line <= last; line++ {
		lines = append(lines, fmt.Sprintf("%6d\tline-%d", line, line))
	}
	return strings.Join(lines, "\n")
}
