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
	assertGoTreeOmits(t, root, retired, false)
}

func TestServiceSourceDoesNotReadProviderKeys(t *testing.T) {
	// R-A3D7-NTLV
	root := filepath.Clean(filepath.Join("..", ".."))
	assertGoTreeOmits(t, root, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}, false)
}

func TestModuleSourceAndManifestDoNotDependOnAgentkit(t *testing.T) {
	// R-A4L4-1LCK
	root := filepath.Clean(filepath.Join("..", ".."))
	forbidden := "ikigenba/" + "agentkit"
	assertGoTreeOmits(t, root, []string{forbidden}, true)
	manifest, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if strings.Contains(string(manifest), forbidden) {
		t.Fatalf("go.mod contains retired module dependency %q", forbidden)
	}
}

func assertGoTreeOmits(t *testing.T, root string, forbidden []string, includeTests bool) {
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
			if rel == "project" {
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
