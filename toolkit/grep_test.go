package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

func TestGrepCount(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"zzz.txt": "hit\nmiss\nhit",
		"aaa.txt": "miss\nhit",
		"mmm.txt": "miss only",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"count"}`))
	if err != nil {
		t.Fatalf("Grep count call error = %v", err)
	}
	want := filepath.Join(root, "aaa.txt") + ":1\n" + filepath.Join(root, "zzz.txt") + ":2"
	if got != want {
		t.Errorf("Grep count result = %q, want %q", got, want)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"absent","output_mode":"count"}`))
	if err != nil {
		t.Fatalf("Grep empty count call error = %v", err)
	}
	if got != "No matches found" {
		t.Errorf("Grep empty count result = %q, want no matches", got)
	}
	// R-DVHT-N1EY
}

func TestGrepContent(t *testing.T) {
	root := t.TempDir()
	aaaPath := filepath.Join(root, "aaa.txt")
	zzzPath := filepath.Join(root, "zzz.txt")
	if err := os.WriteFile(aaaPath, []byte("hit first\nplain\nhit third"), 0o600); err != nil {
		t.Fatalf("write aaa fixture: %v", err)
	}
	if err := os.WriteFile(zzzPath, []byte("plain\nhit second"), 0o600); err != nil {
		t.Fatalf("write zzz fixture: %v", err)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"content"}`))
	if err != nil {
		t.Fatalf("Grep content call error = %v", err)
	}
	want := strings.Join([]string{
		aaaPath + ":1:hit first", "--", aaaPath + ":3:hit third", "--", zzzPath + ":2:hit second",
	}, "\n")
	if got != want {
		t.Errorf("Grep numbered content = %q, want %q", got, want)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"content","-n":false}`))
	if err != nil {
		t.Fatalf("Grep unnumbered content call error = %v", err)
	}
	want = strings.Join([]string{
		aaaPath + ":hit first", "--", aaaPath + ":hit third", "--", zzzPath + ":hit second",
	}, "\n")
	if got != want {
		t.Errorf("Grep unnumbered content = %q, want %q", got, want)
	}
	// R-DWPQ-0T5N
}

func TestGrepContentContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lines.txt")
	contents := "zero\none\nhit two\nthree\nfour\nfive\nhit six\nseven\neight"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	tests := []struct {
		name  string
		flags string
		want  string
	}{
		{"after", `,"-A":1`, strings.Join([]string{path + ":3:hit two", path + "-4-three", "--", path + ":7:hit six", path + "-8-seven"}, "\n")},
		{"before", `,"-B":1`, strings.Join([]string{path + "-2-one", path + ":3:hit two", "--", path + "-6-five", path + ":7:hit six"}, "\n")},
		{"common", `,"-C":1`, strings.Join([]string{path + "-2-one", path + ":3:hit two", path + "-4-three", "--", path + "-6-five", path + ":7:hit six", path + "-8-seven"}, "\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := `{"pattern":"hit","output_mode":"content"` + test.flags + `}`
			got, err := tool.Call(context.Background(), json.RawMessage(input))
			if err != nil {
				t.Fatalf("Grep context call error = %v", err)
			}
			if got != test.want {
				t.Errorf("Grep context result = %q, want %q", got, test.want)
			}
			if strings.HasPrefix(got, "--\n") || strings.HasSuffix(got, "\n--") {
				t.Errorf("Grep context has edge separator: %q", got)
			}
		})
	}
	// R-DXXM-EKWC
}

func TestGrepIgnoresContentFlagsInOtherModes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lines.txt")
	if err := os.WriteFile(path, []byte("before\nhit\nafter"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	plain, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"count"}`))
	if err != nil {
		t.Fatalf("plain Grep count call error = %v", err)
	}
	flagged, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"count","-n":false,"-A":4,"-B":3,"-C":2}`))
	if err != nil {
		t.Fatalf("flagged Grep count call error = %v", err)
	}
	if flagged != plain || flagged != path+":1" {
		t.Errorf("flagged count = %q, plain count = %q, want %q", flagged, plain, path+":1")
	}
	// R-DZ5I-SCN1
}

func TestGrepGlobFilter(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatalf("make nested fixture directory: %v", err)
	}
	goPath := filepath.Join(root, "a.go")
	txtPath := filepath.Join(nested, "b.txt")
	for path, contents := range map[string]string{goPath: "needle", txtPath: "needle"} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle","glob":"*.go"}`))
	if err != nil {
		t.Fatalf("glob-filtered Grep call error = %v", err)
	}
	// R-DQM8-3YG6
	if got != goPath {
		t.Errorf("glob-filtered result = %q, want only %q", got, goPath)
	}
	if strings.Contains(got, txtPath) {
		t.Errorf("glob-filtered result contains excluded path %q", txtPath)
	}

	_, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle","glob":"["}`))
	if err == nil {
		t.Fatal("Grep with invalid glob returned nil error")
	}
	if !strings.Contains(err.Error(), "glob") {
		t.Errorf("invalid glob error = %q, want it to name glob", err)
	}
}

func TestGrepPathFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatalf("make subdirectory: %v", err)
	}
	targetPath := filepath.Join(subdir, "target.txt")
	plainPath := filepath.Join(subdir, "plain.txt")
	siblingPath := filepath.Join(root, "sibling.txt")
	for path, contents := range map[string]string{
		targetPath:  "needle in target",
		plainPath:   "unrelated",
		siblingPath: "needle in sibling",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle","path":"subdir/target.txt"}`))
	if err != nil {
		t.Fatalf("file-path Grep call error = %v", err)
	}
	// R-DRU4-HQ6V
	if got != targetPath {
		t.Errorf("file-path result = %q, want only %q", got, targetPath)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle","path":"subdir"}`))
	if err != nil {
		t.Fatalf("directory-path Grep call error = %v", err)
	}
	if got != targetPath {
		t.Errorf("directory-path result = %q, want only %q", got, targetPath)
	}
	if strings.Contains(got, siblingPath) {
		t.Errorf("directory-path result contains outside sibling %q", siblingPath)
	}

	_, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle","path":"missing"}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("missing path error = %v, want an error naming path", err)
	}
}

func TestGrepSkipsBinaryFiles(t *testing.T) {
	root := t.TempDir()
	textPath := filepath.Join(root, "text.txt")
	binaryPath := filepath.Join(root, "binary.dat")
	if err := os.WriteFile(textPath, []byte("needle in text"), 0o600); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("needle before\x00needle after"), 0o600); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatalf("Grep binary-skip call error = %v", err)
	}
	// R-DT20-VHXK
	if got != textPath {
		t.Errorf("binary-skip result = %q, want only %q", got, textPath)
	}
	if strings.Contains(got, binaryPath) {
		t.Errorf("binary-skip result contains binary path %q", binaryPath)
	}
}

func TestGrepMultiline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lines.txt")
	if err := os.WriteFile(path, []byte("zero\none\ntwo\nthree"), 0o600); err != nil {
		t.Fatalf("write multiline fixture: %v", err)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"one.*two"}`))
	if err != nil {
		t.Fatalf("single-line Grep call error = %v", err)
	}
	if got != "No matches found" {
		t.Errorf("single-line result = %q, want no matches", got)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"one.*two","multiline":true}`))
	if err != nil {
		t.Fatalf("multiline files Grep call error = %v", err)
	}
	// R-E0DF-64DQ
	if got != path {
		t.Errorf("multiline files result = %q, want %q", got, path)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"one.*two","multiline":true,"output_mode":"count"}`))
	if err != nil {
		t.Fatalf("multiline count Grep call error = %v", err)
	}
	if want := path + ":1"; got != want {
		t.Errorf("multiline count result = %q, want %q", got, want)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"one.*two","multiline":true,"output_mode":"content"}`))
	if err != nil {
		t.Fatalf("multiline content Grep call error = %v", err)
	}
	want := path + ":2:one\n" + path + ":3:two"
	if got != want {
		t.Errorf("multiline content result = %q, want %q", got, want)
	}
}

func TestGrepHeadLimit(t *testing.T) {
	root := t.TempDir()
	var paths []string
	for _, name := range []string{"e.txt", "a.txt", "d.txt", "b.txt", "c.txt"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("hit"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
		paths = append(paths, path)
	}
	tool, err := Grep(root)
	if err != nil {
		t.Fatalf("Grep() error = %v", err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","head_limit":3}`))
	if err != nil {
		t.Fatalf("limited Grep call error = %v", err)
	}
	sort.Strings(paths)
	want := strings.Join(append(append([]string(nil), paths[:3]...), "[truncated to first 3 entries]"), "\n")
	// R-EJVT-AG8U
	if got != want {
		t.Errorf("limited result = %q, want %q", got, want)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","glob":"a.txt","head_limit":3}`))
	if err != nil {
		t.Fatalf("under-limit Grep call error = %v", err)
	}
	if got != paths[0] || strings.Contains(got, "[truncated") {
		t.Errorf("under-limit result = %q, want %q without truncation", got, paths[0])
	}

	contentRoot := t.TempDir()
	aPath := filepath.Join(contentRoot, "a.txt")
	bPath := filepath.Join(contentRoot, "b.txt")
	for _, path := range []string{aPath, bPath} {
		if err := os.WriteFile(path, []byte("hit\ncontext"), 0o600); err != nil {
			t.Fatalf("write content fixture %s: %v", path, err)
		}
	}
	contentTool, err := Grep(contentRoot)
	if err != nil {
		t.Fatalf("Grep() for content root error = %v", err)
	}
	got, err = contentTool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"content","-A":1,"head_limit":3}`))
	if err != nil {
		t.Fatalf("limited content Grep call error = %v", err)
	}
	want = strings.Join([]string{
		aPath + ":1:hit", aPath + "-2-context", "--", bPath + ":1:hit", "[truncated to first 3 entries]",
	}, "\n")
	if got != want {
		t.Errorf("limited content result = %q, want %q", got, want)
	}
	if strings.Count(got, "[truncated") != 1 || !strings.HasSuffix(got, "[truncated to first 3 entries]") {
		t.Errorf("limited content truncation placement = %q", got)
	}

	got, err = contentTool.Call(context.Background(), json.RawMessage(`{"pattern":"hit","output_mode":"content","-A":1,"head_limit":2}`))
	if err != nil {
		t.Fatalf("group-boundary content Grep call error = %v", err)
	}
	if strings.Contains(got, "--\n[truncated") {
		t.Errorf("group-boundary truncation has a trailing separator: %q", got)
	}

	// The default head limit is 250 entries.
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
