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
	// R-KFX6-MNEW
	root := filepath.Clean(filepath.Join("..", ".."))
	if strings.TrimSpace(extract.DefaultPromptInstructions) == "" {
		t.Fatal("extract.DefaultPromptInstructions is not exported with production content")
	}
	retired := []string{"Recorder", "Call" + "Record", "LLM" + "CallStore", "NewRecording" + "Embedder", "WithJob" + "ID"}
	assertGoTreeOmits(t, root, retired, false, nil)
}

func TestServiceSourceDoesNotReadProviderKeys(t *testing.T) {
	// R-KEPA-8VO7
	root := filepath.Clean(filepath.Join("..", ".."))
	assertGoTreeOmits(t, root, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}, false, []string{"cmd/eval-extract", "cmd/eval-analysis", "internal/eval"})
}

func TestAgentkitImportsAreConfinedToEvaluationWorkbench(t *testing.T) {
	// R-KH53-0F5L
	root := filepath.Clean(filepath.Join("..", ".."))
	assertGoTreeOmits(t, root, []string{"ikigenba/" + "agentkit"}, true, []string{"cmd/eval-extract", "cmd/eval-analysis", "internal/eval"})
}

func assertGoTreeOmits(t *testing.T, root string, forbidden []string, includeTests bool, allowedDirs []string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == "project" || pathWithinAny(rel, allowedDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || (!includeTests && strings.HasSuffix(path, "_test.go")) {
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

func pathWithinAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
