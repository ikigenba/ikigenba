package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const prerequisiteHead = "0123456789abcdef"

func TestBuildRejectsInvalidAppBeforeCheckout(t *testing.T) {
	// R-EUMY-0Z1G
	// R-EOJG-44BZ
	root := t.TempDir()
	seedAdversarialTree(t, filepath.Join(root, "crm", "dist"))
	before := snapshotTree(t, root)
	execCalls := 0
	cloudCalls := 0
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"Bad/App"}, &stdout, seam.Deps{
		Dir: root,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, errors.New("unexpected Cloud call")
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			execCalls++
			return seam.Result{}, errors.New("unexpected execution")
		},
	})

	assertUsageError(t, err, "'Bad/App' is not a usable app name")
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if execCalls != 0 {
		t.Fatalf("Exec calls = %d, want 0", execCalls)
	}
	if cloudCalls != 0 {
		t.Fatalf("Cloud calls = %d, want 0", cloudCalls)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("filesystem changed on invalid-name refusal\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestBuildRefusesDirtyCheckoutBeforeHeadOrTags(t *testing.T) {
	// R-6M69-Y36S
	// R-EOJG-44BZ
	fixture := newPrerequisiteFixture(t, " M crm/main.go\n", "")
	before := fixture.seedAndSnapshotDist(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), []string{"crm"}, &stdout, fixture.deps())

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

func TestBuildRequiresMatchingAppTagAtHead(t *testing.T) {
	// R-EPRC-HW2O
	// R-EOJG-44BZ
	fixture := newPrerequisiteFixture(t, "", "other/v1.2.3\ncrm/not-semver\n")
	before := fixture.seedAndSnapshotDist(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), []string{"crm"}, &stdout, fixture.deps())

	assertUsageError(t, err, "no tag crm/v<semver> points at HEAD ("+prerequisiteHead+")")
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	fixture.assertAllPrerequisiteCommands(t)
	if after := fixture.snapshotDist(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("dist changed on no-tag refusal\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestPrepareBuildSelectsLexicographicallyFirstCompleteAppTag(t *testing.T) {
	fixture := newPrerequisiteFixture(t, "", "zebra/v9.9.9\ncrm/v2.0.0\ncrm/v1.9.0+z\ncrm/v1.9.0+a\n")

	prepared, err := prepareBuild(context.Background(), "crm", fixture.deps())
	if err != nil {
		t.Fatalf("prepareBuild error = %v", err)
	}
	if prepared.app.Name != "crm" {
		t.Fatalf("prepared app = %q, want crm", prepared.app.Name)
	}
	if prepared.version != "v1.9.0+a" {
		t.Fatalf("prepared version = %q, want v1.9.0+a", prepared.version)
	}
	fixture.assertAllPrerequisiteCommands(t)
}

func TestPrepareBuildAcceptsFullSemverWithoutBranchPolicy(t *testing.T) {
	// R-ETF1-N7AR
	for _, version := range []string{"v1.2.3", "v1.2.3-rc.1", "v1.2.3+build.7", "v1.2.3-rc.1+build.7"} {
		t.Run(version, func(t *testing.T) {
			fixture := newPrerequisiteFixture(t, "", "crm/"+version+"\n")
			fixture.cloudCalled = func() { t.Fatal("Cloud called") }

			prepared, err := prepareBuild(context.Background(), "crm", fixture.deps())
			if err != nil {
				t.Fatalf("prepareBuild error = %v", err)
			}
			if prepared.version != version {
				t.Fatalf("prepared version = %q, want %q", prepared.version, version)
			}
			fixture.assertAllPrerequisiteCommands(t)
		})
	}
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
	tags        string
	gitArgs     [][]string
	cloudCalled func()
}

func newPrerequisiteFixture(t *testing.T, status, tags string) *prerequisiteFixture {
	t.Helper()
	const app = "crm"
	root := t.TempDir()
	appDir := filepath.Join(root, app)
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "etc", "manifest.toml"), []byte("app = \""+app+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &prerequisiteFixture{root: root, app: app, status: status, tags: tags}
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
			case reflect.DeepEqual(command.Args, []string{"tag", "--points-at", "HEAD"}):
				return seam.Result{Stdout: []byte(fixture.tags)}, nil
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
		{"tag", "--points-at", "HEAD"},
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
