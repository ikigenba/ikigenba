package wiki_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wiki/internal/extract"
)

func TestRetiredRecorderAndEvaluationSurfaceIsAbsent(t *testing.T) {
	// R-1E79-6TB1
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, path := range []string{"internal/eval", "cmd/eval-extract", "testdata/eval"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("retired path %s still exists (err=%v)", path, err)
		}
	}
	if strings.TrimSpace(extract.DefaultPromptInstructions) == "" {
		t.Fatal("extract.DefaultPromptInstructions is not exported with production content")
	}
	retired := []string{"Recorder", "Call" + "Record", "LLM" + "CallStore", "NewRecording" + "Embedder", "WithJob" + "ID"}
	assertGoTreeOmits(t, root, retired, false)
}

func TestGoSourceHasNoRetiredProviderDependency(t *testing.T) {
	// R-1GN1-YCSF
	root := filepath.Clean(filepath.Join("..", ".."))
	assertGoTreeOmits(t, root, []string{"ikigenba/" + "agentkit"}, true)
}

func assertGoTreeOmits(t *testing.T, root string, forbidden []string, includeTests bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "project" {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || (!includeTests && strings.HasSuffix(path, "_test.go")) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, value := range forbidden {
			if strings.Contains(string(raw), value) {
				t.Errorf("%s contains retired source vocabulary %q", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go source: %v", err)
	}
}
