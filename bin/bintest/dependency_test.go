package bintest

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type modulePathVersion struct {
	Path    string
	Version string
}

type moduleReplacement struct {
	Old modulePathVersion
	New modulePathVersion
}

type moduleFile struct {
	Path    string
	Module  modulePathVersion
	Require []modulePathVersion
	Replace []moduleReplacement
}

func committedModuleFiles(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "go.mod" || pathHasSegment(path, "testdata") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("discover go.mod files: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("found no committed go.mod files")
	}
	sort.Strings(paths)
	return paths
}

func pathHasSegment(path, segment string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == segment {
			return true
		}
	}
	return false
}

func readModuleFile(t *testing.T, path string) moduleFile {
	t.Helper()
	cmd := exec.Command("go", "mod", "edit", "-json", path)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, out)
	}
	var mod moduleFile
	if err := json.Unmarshal(out, &mod); err != nil {
		t.Fatalf("decode parsed module %s: %v", path, err)
	}
	mod.Path = path
	return mod
}

func readAllModuleFiles(t *testing.T) []moduleFile {
	t.Helper()
	paths := committedModuleFiles(t)
	modules := make([]moduleFile, 0, len(paths))
	for _, path := range paths {
		modules = append(modules, readModuleFile(t, path))
	}
	return modules
}

func relativeToRepo(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot(t), path)
	if err != nil {
		t.Fatalf("make %s relative to repo: %v", path, err)
	}
	return filepath.ToSlash(rel)
}

// R-3R5W-79JK
func TestInRepoLibrariesUseUnversionedRelativeReplaces(t *testing.T) {
	root := repoRoot(t)
	libraries := make(map[string]string)
	for _, name := range []string{"appkit", "eventplane", "registry"} {
		mod := readModuleFile(t, filepath.Join(root, name, "go.mod"))
		if mod.Module.Path == "" {
			t.Fatalf("%s/go.mod has no module path", name)
		}
		libraries[mod.Module.Path] = "../" + name
	}

	for _, mod := range readAllModuleFiles(t) {
		for _, requirement := range mod.Require {
			wantReplace, isLibrary := libraries[requirement.Path]
			if !isLibrary {
				continue
			}
			if requirement.Version != "v0.0.0" {
				t.Errorf("%s requires in-repo library %s at %s, want v0.0.0", relativeToRepo(t, mod.Path), requirement.Path, requirement.Version)
			}
			matched := false
			for _, replacement := range mod.Replace {
				if replacement.Old.Path == requirement.Path && replacement.Old.Version == "" {
					matched = replacement.New.Path == wantReplace && replacement.New.Version == ""
				}
			}
			if !matched {
				t.Errorf("%s requires in-repo library %s without replace to %s", relativeToRepo(t, mod.Path), requirement.Path, wantReplace)
			}
		}
	}
}

// R-3SDS-L1A9
func TestModulePathsAreWrittenAsPlainLiterals(t *testing.T) {
	for _, mod := range readAllModuleFiles(t) {
		raw, err := os.ReadFile(mod.Path)
		if err != nil {
			t.Fatalf("read %s: %v", mod.Path, err)
		}
		paths := []string{mod.Module.Path}
		for _, requirement := range mod.Require {
			paths = append(paths, requirement.Path)
		}
		for _, replacement := range mod.Replace {
			paths = append(paths, replacement.Old.Path, replacement.New.Path)
		}
		for _, path := range paths {
			if path != "" && !bytes.Contains(raw, []byte(path)) {
				t.Errorf("%s does not write parsed module path %q as a plain literal", relativeToRepo(t, mod.Path), path)
			}
		}
	}
}

// R-3TLO-YT0Y
func TestAgentkitRequiresUseOneRepoWideVersion(t *testing.T) {
	const agentkit = "github.com/ikigenba/agentkit"
	versions := make(map[string][]string)
	for _, mod := range readAllModuleFiles(t) {
		for _, requirement := range mod.Require {
			if requirement.Path == agentkit {
				versions[requirement.Version] = append(versions[requirement.Version], relativeToRepo(t, mod.Path))
			}
		}
	}
	if len(versions) > 1 {
		t.Errorf("agentkit requires use divergent versions: %v", versions)
	}
}

// R-3UTL-CKRN
func TestCommittedWorkspaceHasNoReplaceDirectives(t *testing.T) {
	workPath := filepath.Join(repoRoot(t), "go.work")
	cmd := exec.Command("go", "work", "edit", "-json", workPath)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parse go.work: %v\n%s", err, out)
	}
	var work struct {
		Replace []moduleReplacement
	}
	if err := json.Unmarshal(out, &work); err != nil {
		t.Fatalf("decode parsed go.work: %v", err)
	}
	if len(work.Replace) != 0 {
		t.Errorf("go.work has replace directives: %+v", work.Replace)
	}
}
