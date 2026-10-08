package checkout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestHasAppUsesOnlyNamedEntriesFiles(t *testing.T) {
	// R-EYBP-QLFD
	signature := checkoutMethod[func(*Checkout, string) bool]((*Checkout).HasApp)
	// R-EZJM-4D62
	t.Parallel()
	root := t.TempDir()
	writeAppFixture(t, root, "crm", "package main\n", "invalid = [")
	writeAppFixture(t, root, "broken", "package main\n", "invalid = [")
	writeAppFixture(t, root, "Crm", "package main\n", "app = 'other'\n")
	writeAppFixture(t, root, "library", "package library\n", "app = 'library'\n")
	writeFile(t, filepath.Join(root, "no-manifest", "cmd", "no-manifest", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "root-only", ManifestFile), "app = 'root-only'\n")
	writeFile(t, filepath.Join(root, "root-only", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "nested-only", ManifestFile), "")
	writeFile(t, filepath.Join(root, "nested-only", "cmd", "nested-only", "nested", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "other-only", ManifestFile), "")
	writeFile(t, filepath.Join(root, "other-only", "cmd", "other", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "test-only", ManifestFile), "")
	writeFile(t, filepath.Join(root, "test-only", "cmd", "test-only", "main_test.go"), "package main\n")
	writeFile(t, filepath.Join(root, "wrong-extension", ManifestFile), "")
	writeFile(t, filepath.Join(root, "wrong-extension", "cmd", "wrong-extension", "main.txt"), "package main\n")
	writeFile(t, filepath.Join(root, "manifest-directory", "cmd", "manifest-directory", "main.go"), "package main\n")
	if err := os.MkdirAll(filepath.Join(root, "manifest-directory", ManifestFile), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "plain-file"), "")
	opened := &Checkout{Root: root, Deps: seam.Deps{Dir: filepath.Join(root, "different"), Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("HasApp invoked Exec")
		return seam.Result{}, nil
	}}}
	for _, name := range []string{"crm", "Crm"} {
		if !signature(opened, name) {
			t.Errorf("HasApp(%q) = false", name)
		}
	}
	writeFile(t, filepath.Join(root, "crm", ManifestFile), "app = 'crm'\nport = 3100\n")
	if !opened.HasApp("crm") {
		t.Error("HasApp(crm) = false with port in its manifest and broken other manifest")
	}
	for _, name := range []string{"", ".", "..", "bogus", "no-manifest", "root-only", "nested-only", "other-only", "test-only", "wrong-extension", "manifest-directory", "library", "plain-file", "crm/cmd", "devctl/v1.2.0"} {
		if opened.HasApp(name) {
			t.Errorf("HasApp(%q) = true", name)
		}
	}
}
