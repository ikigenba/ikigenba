package checkout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// R-W1C4-X1GX
func TestAppsDiscoversOnlyRunnableRootDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAppFixture(t, root, "zeta", "package main\n", "app = \"zeta\"\nsecrets = [\"TOKEN\"]\n")
	writeAppFixture(t, root, "alpha", "package main\n", "app = \"alpha\"\n")
	writeAppFixture(t, root, "library", "package library\n", "app = \"library\"\n")
	writeAppFixture(t, root, "test-only", "package main\n", "app = \"test-only\"\n")
	if err := os.Rename(filepath.Join(root, "test-only", "main.go"), filepath.Join(root, "test-only", "main_test.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "nested-only", ManifestFile), "app = \"nested-only\"\n")
	writeFile(t, filepath.Join(root, "nested-only", "nested", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "no-manifest", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "plain-file"), "package main\n")
	writeFile(t, filepath.Join(root, "manifest-dir", "main.go"), "package main\n")
	if err := os.MkdirAll(filepath.Join(root, "manifest-dir", ManifestFile), 0o700); err != nil {
		t.Fatal(err)
	}

	checkout := &Checkout{
		Root: root,
		Deps: seam.Deps{
			Dir: filepath.Join(root, "some", "subdirectory"),
			Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				t.Fatal("Apps passed a command to Exec")
				return seam.Result{}, nil
			},
		},
	}
	got, err := checkout.Apps()
	if err != nil {
		t.Fatalf("Apps() error = %v", err)
	}
	want := []App{
		{Name: "alpha", Dir: filepath.Join(root, "alpha"), Manifest: Manifest{App: "alpha", Secrets: []string{}}},
		{Name: "zeta", Dir: filepath.Join(root, "zeta"), Manifest: Manifest{App: "zeta", Secrets: []string{"TOKEN"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Apps() = %#v, want %#v", got, want)
	}

	empty := &Checkout{Root: t.TempDir(), Deps: checkout.Deps}
	got, err = empty.Apps()
	if err != nil {
		t.Fatalf("empty Apps() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty Apps() = %#v, want empty result", got)
	}
}

// R-W2K1-AT7M
func TestAppsReportsManifestFailures(t *testing.T) {
	t.Parallel()

	t.Run("cannot decode", func(t *testing.T) {
		root := t.TempDir()
		writeAppFixture(t, root, "crm", "package main\n", "app = [\n")

		_, err := (&Checkout{Root: root}).Apps()
		_ = assertManifestFailure(t, err, "crm")
	})

	t.Run("cannot read", func(t *testing.T) {
		root := t.TempDir()
		writeAppFixture(t, root, "crm", "package main\n", "app = \"crm\"\n")
		manifest := filepath.Join(root, "crm", ManifestFile)
		if err := os.Chmod(manifest, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(manifest, 0o600) })

		_, err := (&Checkout{Root: root}).Apps()
		manifestErr := assertManifestFailure(t, err, "crm")
		if !errors.Is(manifestErr.Err, os.ErrPermission) {
			t.Fatalf("Apps() cause = %v, want permission failure", manifestErr.Err)
		}
	})

	t.Run("app differs from directory", func(t *testing.T) {
		root := t.TempDir()
		writeAppFixture(t, root, "crm", "package main\n", "app = \"billing\"\n")

		_, err := (&Checkout{Root: root}).Apps()
		var manifestErr *ManifestError
		if !errors.As(err, &manifestErr) {
			t.Fatalf("Apps() error = %T %v, want *ManifestError", err, err)
		}
		if manifestErr.App != "crm" || manifestErr.Detail != "app is 'billing', not 'crm'" {
			t.Fatalf("Apps() error = %#v", manifestErr)
		}
	})
}

// R-W3RX-OKYB
func TestAppReturnsMatchMissingAndAppsFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAppFixture(t, root, "crm", "package main\n", "app = \"crm\"\nsecrets = [\"CRM_KEY\"]\n")
	checkout := &Checkout{Root: root}

	got, err := checkout.App("crm")
	if err != nil {
		t.Fatalf("App(crm) error = %v", err)
	}
	want := App{Name: "crm", Dir: filepath.Join(root, "crm"), Manifest: Manifest{App: "crm", Secrets: []string{"CRM_KEY"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("App(crm) = %#v, want %#v", got, want)
	}

	_, err = checkout.App("bogus")
	var noApp *NoAppError
	if !errors.As(err, &noApp) || noApp.Name != "bogus" {
		t.Fatalf("App(bogus) error = %T %#v, want *NoAppError for bogus", err, err)
	}

	writeAppFixture(t, root, "broken", "package main\n", "not valid toml =")
	_, err = checkout.App("crm")
	var manifestErr *ManifestError
	if !errors.As(err, &manifestErr) || manifestErr.App != "broken" {
		t.Fatalf("App(crm) with broken Apps result error = %T %#v", err, err)
	}
}

func assertManifestFailure(t *testing.T, err error, app string) *ManifestError {
	t.Helper()
	var manifestErr *ManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("Apps() error = %T %v, want *ManifestError", err, err)
	}
	if manifestErr.App != app || manifestErr.Err == nil {
		t.Fatalf("Apps() error = %#v, want App %q and non-nil Err", manifestErr, app)
	}
	return manifestErr
}

func writeAppFixture(t *testing.T, root, name, source, manifest string) {
	t.Helper()
	writeFile(t, filepath.Join(root, name, "main.go"), source)
	writeFile(t, filepath.Join(root, name, ManifestFile), manifest)
}

func writeFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
