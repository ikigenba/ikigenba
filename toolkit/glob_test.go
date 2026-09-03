package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGlobConstructor(t *testing.T) {
	tool, err := Glob(t.TempDir())
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Glob() returned a nil tool")
	}
	if got, want := tool.Name(), "Glob"; got != want {
		t.Errorf("tool.Name() = %q, want %q", got, want)
	}
	if _, err := Glob(t.TempDir(), WithSkip("x")); err != nil {
		t.Fatalf("Glob() with option error = %v", err)
	}
	// R-C7QQ-WA2X
}

func TestGlobSchema(t *testing.T) {
	tool, err := Glob(t.TempDir())
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", schema["properties"])
	}
	if got, want := sortedKeys(properties), []string{"path", "pattern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("property names = %v, want %v", got, want)
	}
	pattern := properties["pattern"].(map[string]any)
	if got := pattern["type"]; got != "string" {
		t.Errorf("pattern type = %#v, want string", got)
	}
	if got := pattern["minLength"]; got != float64(1) {
		t.Errorf("pattern minLength = %#v, want 1", got)
	}
	path := properties["path"].(map[string]any)
	if got := path["type"]; got != "string" {
		t.Errorf("path type = %#v, want string", got)
	}
	if got, want := schema["required"], []any{"pattern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("required = %#v, want %#v", got, want)
	}
	// R-DGV1-1SIM
}

func TestGlobMatchesFilesWithDoublestar(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"top.go", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "sub.go"), 0o700); err != nil {
		t.Fatalf("mkdir matching directory: %v", err)
	}
	nestedDir := filepath.Join(root, "nested", "deep")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatalf("mkdir nested directory: %v", err)
	}
	nestedFile := filepath.Join(nestedDir, "nested.go")
	if err := os.WriteFile(nestedFile, []byte("nested"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	if want := filepath.Join(root, "top.go"); got != want {
		t.Errorf("Glob *.go = %q, want only regular file %q", got, want)
	}
	if strings.Contains(got, filepath.Join(root, "sub.go")) {
		t.Errorf("Glob returned matching directory: %q", got)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("Glob doublestar call error = %v", err)
	}
	lines := strings.Split(got, "\n")
	if !contains(lines, nestedFile) {
		t.Errorf("Glob doublestar result %q does not contain nested file %q", got, nestedFile)
	}
	// R-DI2X-FK9B
}

func TestGlobOrdersAbsolutePathsByModificationTime(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "newest.txt"),
		filepath.Join(root, "alpha.txt"),
		filepath.Join(root, "beta.txt"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	old := time.Unix(1_700_000_000, 0)
	newest := old.Add(time.Hour)
	for _, path := range paths[1:] {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("set times for %s: %v", path, err)
		}
	}
	if err := os.Chtimes(paths[0], newest, newest); err != nil {
		t.Fatalf("set times for %s: %v", paths[0], err)
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"*.txt"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	want := strings.Join(paths, "\n")
	if got != want {
		t.Errorf("Glob ordering = %q, want %q", got, want)
	}
	for _, path := range strings.Split(got, "\n") {
		if !filepath.IsAbs(path) {
			t.Errorf("Glob path %q is not absolute", path)
		}
	}
	// R-DJAT-TC00
}

func TestGlobNoFilesFound(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "present.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	if got != "No files found" {
		t.Errorf("Glob no-match result = %q, want %q", got, "No files found")
	}
	// R-DKIQ-73QP
}

func TestGlobRejectsInvalidPattern(t *testing.T) {
	tool, err := Glob(t.TempDir())
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	_, err = tool.Call(context.Background(), json.RawMessage(`{"pattern":"a[bc"}`))
	if err == nil {
		t.Fatal("Glob invalid pattern returned nil error")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("Glob error = %q, want it to name pattern", err)
	}
	// R-DLQM-KVHE
}

func TestGlobHonorsIgnoreFiles(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatalf("make nested directory: %v", err)
	}
	for path, contents := range map[string]string{
		filepath.Join(root, ".gitignore"):        "*.log\n",
		filepath.Join(root, ".ignore"):           "ignored.txt\n",
		filepath.Join(nested, ".gitignore"):      "*.tmp\n",
		filepath.Join(root, "keep.txt"):          "needle",
		filepath.Join(root, "top.log"):           "needle",
		filepath.Join(root, "ignored.txt"):       "needle",
		filepath.Join(nested, "keep.txt"):        "needle",
		filepath.Join(nested, "nested-drop.tmp"): "needle",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*.txt"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	// R-DARJ-4XT5
	for _, want := range []string{filepath.Join(root, "keep.txt"), filepath.Join(nested, "keep.txt")} {
		if !contains(strings.Split(got, "\n"), want) {
			t.Errorf("Glob result %q does not contain %q", got, want)
		}
	}
	for _, excluded := range []string{"top.log", "ignored.txt", "nested-drop.tmp"} {
		if strings.Contains(got, excluded) {
			t.Errorf("Glob result %q contains ignored file %q", got, excluded)
		}
	}
}

func TestGlobSkipPatternsAndHiddenEntries(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"generated", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("make fixture directory %s: %v", dir, err)
		}
	}
	for _, name := range []string{"keep.txt", "skip.log", "generated/result.txt", ".env", ".hidden/inside.txt"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("needle"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	tool, err := Glob(root, WithSkip("*.log"), WithSkip("generated/"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	// R-DBZF-IPJU
	if strings.Contains(got, "skip.log") || strings.Contains(got, "generated") {
		t.Errorf("Glob result contains WithSkip match: %q", got)
	}
	// R-DD7B-WHAJ
	for _, want := range []string{filepath.Join(root, ".env"), filepath.Join(root, ".hidden", "inside.txt")} {
		if !contains(strings.Split(got, "\n"), want) {
			t.Errorf("Glob result %q does not contain hidden path %q", got, want)
		}
	}

	hiddenSkippingTool, err := Glob(root, WithSkip(".*"))
	if err != nil {
		t.Fatalf("Glob() with hidden skip error = %v", err)
	}
	got, err = hiddenSkippingTool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*"}`))
	if err != nil {
		t.Fatalf("hidden-skipping Glob call error = %v", err)
	}
	if strings.Contains(got, ".env") || strings.Contains(got, ".hidden") {
		t.Errorf("Glob result contains explicitly skipped hidden path: %q", got)
	}
}

func TestGlobSymlinkWalkPolicy(t *testing.T) {
	temp := t.TempDir()
	root := filepath.Join(temp, "root")
	outsideDir := filepath.Join(temp, "outside")
	for _, dir := range []string{root, outsideDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("make fixture directory %s: %v", dir, err)
		}
	}
	inside := filepath.Join(root, "inside.txt")
	outside := filepath.Join(outsideDir, "outside.txt")
	for _, path := range []string{inside, outside} {
		if err := os.WriteFile(path, []byte("needle"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}
	insideLink := filepath.Join(root, "inside-link.txt")
	outsideLink := filepath.Join(root, "outside-link.txt")
	directoryLink := filepath.Join(root, "directory-link")
	for link, target := range map[string]string{
		insideLink:    inside,
		outsideLink:   outside,
		directoryLink: outsideDir,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("symlink %s to %s: %v", link, target, err)
		}
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*.txt"}`))
	if err != nil {
		t.Fatalf("Glob call error = %v", err)
	}
	// R-DEF8-A918
	if !contains(strings.Split(got, "\n"), insideLink) {
		t.Errorf("Glob result %q omits in-root file symlink %q", got, insideLink)
	}
	if strings.Contains(got, outsideLink) || strings.Contains(got, directoryLink) {
		t.Errorf("Glob result contains escaping symlink: %q", got)
	}
}

func TestGlobPathResolution(t *testing.T) {
	temp := t.TempDir()
	root := filepath.Join(temp, "root")
	subdir := filepath.Join(root, "subdir")
	outsideDir := filepath.Join(temp, "outside")
	for _, dir := range []string{subdir, outsideDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("make fixture directory %s: %v", dir, err)
		}
	}
	nested := filepath.Join(subdir, "nested.txt")
	sibling := filepath.Join(root, "sibling.txt")
	for _, path := range []string{nested, sibling, filepath.Join(outsideDir, "outside.txt")} {
		if err := os.WriteFile(path, []byte("needle"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	tool, err := Glob(root)
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	for name, path := range map[string]string{"relative": "subdir", "absolute": subdir} {
		t.Run(name, func(t *testing.T) {
			input, err := json.Marshal(map[string]string{"pattern": "*.txt", "path": path})
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}
			got, err := tool.Call(context.Background(), input)
			if err != nil {
				t.Fatalf("Glob call error = %v", err)
			}
			if got != nested {
				t.Errorf("Glob result = %q, want only %q", got, nested)
			}
		})
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"pattern":"**/*.txt"}`))
	if err != nil {
		t.Fatalf("default-path Glob call error = %v", err)
	}
	// R-DFN4-O0RX
	for _, want := range []string{nested, sibling} {
		if !contains(strings.Split(got, "\n"), want) {
			t.Errorf("default-path result %q does not contain %q", got, want)
		}
	}

	for _, path := range []string{"sibling.txt", "../outside"} {
		input, err := json.Marshal(map[string]string{"pattern": "*.txt", "path": path})
		if err != nil {
			t.Fatalf("marshal invalid path input: %v", err)
		}
		if _, err := tool.Call(context.Background(), input); err == nil || !strings.Contains(err.Error(), "path") {
			t.Errorf("Glob path %q error = %v, want an error naming path", path, err)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
