package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const prerequisiteHead = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"

func TestBuildRejectsInvalidAppAfterOnlyCheckout(t *testing.T) {
	// R-FOE2-FLBQ
	// R-FPLY-TD2F
	fixture := newPrerequisiteFixture(t, " M host/main.go\n")
	root := fixture.root
	if err := os.Rename(filepath.Join(root, "crm"), filepath.Join(root, "host")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "host", "cmd", "crm"), filepath.Join(root, "host", "cmd", "host")); err != nil {
		t.Fatal(err)
	}
	seedAdversarialTree(t, filepath.Join(root, "crm", "dist"))
	before := snapshotTree(t, root)
	execCalls := 0
	cloudCalls := 0
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"host"}, "test-version", &stdout, seam.Deps{
		Dir: root,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, errors.New("unexpected Cloud call")
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			execCalls++
			if command.Path != "git" || !reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}) {
				t.Fatalf("unexpected command: %#v", command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
	})

	assertUsageError(t, err, "'host' is not a usable app name")
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if execCalls != 1 {
		t.Fatalf("Exec calls = %d, want 1", execCalls)
	}
	if cloudCalls != 0 {
		t.Fatalf("Cloud calls = %d, want 0", cloudCalls)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("filesystem changed on invalid-name refusal\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestBuildRefusesDirtyCheckoutBeforeHead(t *testing.T) {
	// R-6M69-Y36S
	// R-FPLY-TD2F
	fixture := newPrerequisiteFixture(t, " M crm/main.go\n")
	before := fixture.seedAndSnapshotDist(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), []string{"crm"}, "test-version", &stdout, fixture.deps())

	assertUsageError(t, err, "the working tree has uncommitted changes; commit them first")
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	wantCommands := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"status", "--porcelain"},
	}
	if !reflect.DeepEqual(fixture.gitArgs, wantCommands) {
		t.Fatalf("git arguments = %#v, want %#v", fixture.gitArgs, wantCommands)
	}
	if after := fixture.snapshotDist(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("dist changed on dirty-tree refusal\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestPrepareBuildResolvesHeadWithoutTags(t *testing.T) {
	// R-FPLY-TD2F
	fixture := newPrerequisiteFixture(t, "")
	opened, err := checkout.Open(context.Background(), fixture.deps())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareBuild(context.Background(), "crm", opened)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.app.Name != "crm" || prepared.sha != prerequisiteHead {
		t.Fatalf("prepared = %#v", prepared)
	}
	fixture.assertAllPrerequisiteCommands(t)
}

func assertUsageError(t *testing.T, err error, message string) {
	t.Helper()
	var usageError *UsageError
	if !errors.As(err, &usageError) {
		t.Fatalf("error = %T %v, want *UsageError", err, err)
	}
	if usageError.Message != message || usageError.Help != "" {
		t.Fatalf("error = %#v, want Message %q and empty Help", usageError, message)
	}
}

type prerequisiteFixture struct {
	root        string
	app         string
	status      string
	gitArgs     [][]string
	cloudCalled func()
}

func newPrerequisiteFixture(t *testing.T, status string) *prerequisiteFixture {
	t.Helper()
	const app = "crm"
	root := t.TempDir()
	appDir := filepath.Join(root, app)
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(appDir, "cmd", app), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "cmd", app, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "etc", "manifest.toml"), []byte("app = \""+app+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &prerequisiteFixture{root: root, app: app, status: status}
}

func (fixture *prerequisiteFixture) deps() seam.Deps {
	return seam.Deps{
		Dir: fixture.root,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			if fixture.cloudCalled != nil {
				fixture.cloudCalled()
			}
			return cloud.Clients{}, errors.New("unexpected Cloud call")
		},
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" {
				return seam.Result{}, errors.New("unexpected command: " + command.Path)
			}
			fixture.gitArgs = append(fixture.gitArgs, append([]string(nil), command.Args...))
			switch {
			case reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}):
				return seam.Result{Stdout: []byte(fixture.root + "\n")}, nil
			case reflect.DeepEqual(command.Args, []string{"status", "--porcelain"}):
				return seam.Result{Stdout: []byte(fixture.status)}, nil
			case reflect.DeepEqual(command.Args, []string{"rev-parse", "HEAD"}):
				return seam.Result{Stdout: []byte(prerequisiteHead + "\n")}, nil
			default:
				return seam.Result{}, errors.New("unexpected git arguments")
			}
		},
	}
}

func (fixture *prerequisiteFixture) assertAllPrerequisiteCommands(t *testing.T) {
	t.Helper()
	want := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"status", "--porcelain"},
		{"rev-parse", "HEAD"},
	}
	if !reflect.DeepEqual(fixture.gitArgs, want) {
		t.Fatalf("git arguments = %#v, want %#v", fixture.gitArgs, want)
	}
}

func (fixture *prerequisiteFixture) seedAndSnapshotDist(t *testing.T) map[string]treeEntry {
	t.Helper()
	dist := filepath.Join(fixture.root, fixture.app, "dist")
	seedAdversarialTree(t, dist)
	return snapshotTree(t, dist)
}

func (fixture *prerequisiteFixture) snapshotDist(t *testing.T) map[string]treeEntry {
	t.Helper()
	return snapshotTree(t, filepath.Join(fixture.root, fixture.app, "dist"))
}

type treeEntry struct {
	Mode    os.FileMode
	Content string
}

func seedAdversarialTree(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "nested", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"existing.tar.xz":          "existing artifact",
		"nested/partial-build.tmp": "partial bytes",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	opened, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := opened.Close(); err != nil {
			t.Errorf("close snapshot root: %v", err)
		}
	}()
	snapshot := make(map[string]treeEntry)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		captured := treeEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			contents, err := opened.ReadFile(relative)
			if err != nil {
				return err
			}
			captured.Content = string(contents)
		}
		snapshot[relative] = captured
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
