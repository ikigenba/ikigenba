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
	writeFile(t, filepath.Join(root, "symlink-source"), "package main\n")
	writeFile(t, filepath.Join(root, "non-regular-go", ManifestFile), "app = \"non-regular-go\"\n")
	if err := os.Symlink(filepath.Join(root, "symlink-source"), filepath.Join(root, "non-regular-go", "main.go")); err != nil {
		t.Fatal(err)
	}
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
		writeAppFixture(t, root, "crm", "package main\n", "app = \"crm\"\n")
		decodeErr := &identityError{message: "decode sentinel"}

		checkout := &Checkout{Root: root}
		_, err := checkout.apps(func(dir string) (Manifest, error) {
			if dir != filepath.Join(root, "crm") {
				t.Fatalf("read manifest dir = %q, want crm directory", dir)
			}
			return Manifest{}, decodeErr
		})
		assertManifestFailure(t, err, "crm", decodeErr)
	})

	t.Run("cannot read", func(t *testing.T) {
		root := t.TempDir()
		writeAppFixture(t, root, "crm", "package main\n", "app = \"crm\"\n")
		readErr := &identityError{message: "read sentinel"}

		checkout := &Checkout{Root: root}
		_, err := checkout.apps(func(string) (Manifest, error) {
			return Manifest{}, readErr
		})
		assertManifestFailure(t, err, "crm", readErr)
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

	appsErr := &identityError{message: "apps sentinel"}
	invocations := 0
	_, err = checkout.app("crm", func() ([]App, error) {
		invocations++
		return nil, appsErr
	})
	if invocations != 1 {
		t.Fatalf("Apps invocation count = %d, want 1", invocations)
	}
	if !sameError(err, appsErr) {
		t.Fatalf("App(crm) error = %#v, want exact Apps error %#v", err, appsErr)
	}
}

func assertManifestFailure(t *testing.T, err error, app string, wantErr *identityError) {
	t.Helper()
	var manifestErr *ManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("Apps() error = %T %v, want *ManifestError", err, err)
	}
	if manifestErr.App != app || !sameError(manifestErr.Err, wantErr) {
		t.Fatalf("Apps() error = %#v, want App %q and Err %#v", manifestErr, app, wantErr)
	}
}

func sameError(got error, want *identityError) bool {
	return reflect.TypeOf(got) == reflect.TypeOf(want) &&
		reflect.ValueOf(got).Pointer() == reflect.ValueOf(want).Pointer()
}

type identityError struct {
	message string
}

func (err *identityError) Error() string { return err.message }

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
