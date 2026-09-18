package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestArchivePreparedPublishesExactValidatedPayload(t *testing.T) {
	// R-04SM-T2LN
	// R-EZIJ-K208
	// R-F0QF-XTQX
	// R-F5M1-GWPP
	// R-F6TX-UOGE
	staged := archiveFixture(t, "v1.2.3-rc.1+build.7")
	finalPath := filepath.Join(staged.prepared.app.Dir, "dist", "crm-v1.2.3-rc.1+build.7.tar.xz")
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	writeTestFile(t, filepath.Join(staged.prepared.app.Dir, "dist", "keep.txt"), []byte("keep"), 0o640)
	var commands []seam.Cmd
	var stdout bytes.Buffer

	err := archivePrepared(context.Background(), staged, &stdout, realTarDeps(&commands))
	if err != nil {
		t.Fatalf("archivePrepared error = %v", err)
	}
	if stdout.String() != "crm/dist/crm-v1.2.3-rc.1+build.7.tar.xz\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(commands) != 1 || commands[0].Path != "tar" || commands[0].Dir != staged.prepared.app.Dir {
		t.Fatalf("commands = %#v, want one tar command in app directory", commands)
	}

	listing := runTar(t, staged.prepared.app.Dir, "-tJf", finalPath)
	wantMembers := []string{
		"bin/crm",
		"etc/extra.conf",
		"etc/manifest.toml",
		"etc/nested/settings.ini",
		"share/assets/message.txt",
	}
	gotMembers := strings.Split(strings.TrimSuffix(string(listing), "\n"), "\n")
	if !reflect.DeepEqual(gotMembers, wantMembers) {
		t.Fatalf("archive members = %#v, want %#v", gotMembers, wantMembers)
	}
	for _, member := range gotMembers {
		if strings.Contains(member, staged.prepared.version) {
			t.Fatalf("archive member %q contains version directory", member)
		}
	}

	extracted := t.TempDir()
	runTar(t, staged.prepared.app.Dir, "-xJf", finalPath, "-C", extracted)
	assertTestFile(t, filepath.Join(extracted, "bin", "crm"), []byte("validated executable"), 0o711)
	assertTestFile(t, filepath.Join(extracted, filepath.FromSlash(checkout.ManifestFile)), staged.manifest, 0o644)
	assertTestFile(t, filepath.Join(extracted, "etc", "extra.conf"), []byte("extra\x00bytes"), 0o640)
	assertTestFile(t, filepath.Join(extracted, "etc", "nested", "settings.ini"), []byte("nested\n"), 0o604)
	assertTestFile(t, filepath.Join(extracted, "share", "assets", "message.txt"), []byte("share bytes\n"), 0o444)
	if _, err := os.Lstat(filepath.Join(extracted, "etc", "ignored-link")); !os.IsNotExist(err) {
		t.Fatalf("non-regular etc entry was archived: %v", err)
	}

	assertDistNames(t, staged.prepared.app.Dir,
		".validated-stage", "crm-v1.2.3-rc.1+build.7.tar.xz", "keep.txt")
}

func TestArchivePreparedRejectsStaleManifestBeforeTar(t *testing.T) {
	// R-6UPK-MHDN
	// R-04SM-T2LN
	// R-F0QF-XTQX
	// R-F6TX-UOGE
	staged := archiveFixture(t, "v1.2.3")
	writeTestFile(t, filepath.Join(staged.prepared.app.Dir, checkout.ManifestFile), []byte("app = \"crm\"\n# committed\n"), 0o644)
	finalPath := filepath.Join(staged.prepared.app.Dir, "dist", "crm-v1.2.3.tar.xz")
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	var stdout bytes.Buffer
	tarCalls := 0

	err := archivePrepared(context.Background(), staged, &stdout, seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		tarCalls++
		return seam.Result{}, errors.New("tar must not run")
	}})
	var stale *StaleManifestError
	if !errors.As(err, &stale) || stale.App != "crm" {
		t.Fatalf("error = %#v, want StaleManifestError for crm", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if tarCalls != 0 {
		t.Fatalf("tar calls = %d, want 0", tarCalls)
	}
	assertTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	assertDistNames(t, staged.prepared.app.Dir, ".validated-stage", "crm-v1.2.3.tar.xz")
}

func TestArchivePreparedTarFailurePreservesPriorArtifact(t *testing.T) {
	// R-04SM-T2LN
	// R-EZIJ-K208
	// R-F0QF-XTQX
	// R-F6TX-UOGE
	staged := archiveFixture(t, "v1.2.3")
	finalPath := filepath.Join(staged.prepared.app.Dir, "dist", "crm-v1.2.3.tar.xz")
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	var stdout bytes.Buffer

	err := archivePrepared(context.Background(), staged, &stdout, seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		if command.Path != "tar" {
			t.Fatalf("command = %#v, want tar", command)
		}
		return seam.Result{ExitCode: 23, Stderr: []byte("xz failed\n")}, nil
	}})
	var processError *ProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("error = %T %v, want *ProcessError", err, err)
	}
	if *processError != (ProcessError{Label: "archive crm", Status: 23, Stderr: "xz failed\n"}) {
		t.Fatalf("ProcessError = %#v", processError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	assertTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	assertDistNames(t, staged.prepared.app.Dir, ".validated-stage", "crm-v1.2.3.tar.xz")
}

func TestSelectedTagSuffixNamesArtifactAndOutput(t *testing.T) {
	// R-EQZ8-VNTD
	fixture := newPrerequisiteFixture(t, "", "zebra/v9.9.9\ncrm/v2.0.0\ncrm/v1.9.0+z\ncrm/v1.9.0+a\n")
	prepared, err := prepareBuild(context.Background(), "crm", fixture.deps())
	if err != nil {
		t.Fatalf("prepareBuild error = %v", err)
	}
	fixture.assertAllPrerequisiteCommands(t)
	stageDir := filepath.Join(prepared.app.Dir, "dist", ".selected-stage")
	binary := filepath.Join(stageDir, "crm")
	writeTestFile(t, binary, []byte("selected executable"), 0o700)
	manifest, err := os.ReadFile(filepath.Join(prepared.app.Dir, checkout.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = archivePrepared(context.Background(), stagedBuild{prepared: prepared, binary: binary, manifest: manifest}, &stdout, realTarDeps(nil))
	if err != nil {
		t.Fatalf("archivePrepared error = %v", err)
	}
	if stdout.String() != "crm/dist/crm-v1.9.0+a.tar.xz\n" {
		t.Fatalf("stdout = %q, want selected tag suffix", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(prepared.app.Dir, "dist", "crm-v1.9.0+a.tar.xz")); err != nil {
		t.Fatalf("selected artifact missing: %v", err)
	}
}

func archiveFixture(t *testing.T, version string) stagedBuild {
	t.Helper()
	appDir := filepath.Join(t.TempDir(), "crm")
	manifest := []byte("app = \"crm\"\n")
	writeTestFile(t, filepath.Join(appDir, checkout.ManifestFile), manifest, 0o600)
	writeTestFile(t, filepath.Join(appDir, "etc", "extra.conf"), []byte("extra\x00bytes"), 0o640)
	writeTestFile(t, filepath.Join(appDir, "etc", "nested", "settings.ini"), []byte("nested\n"), 0o604)
	writeTestFile(t, filepath.Join(appDir, "share", "assets", "message.txt"), []byte("share bytes\n"), 0o444)
	if err := os.Symlink("extra.conf", filepath.Join(appDir, "etc", "ignored-link")); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(appDir, "dist", ".validated-stage", "crm")
	writeTestFile(t, binary, []byte("validated executable"), 0o600)
	return stagedBuild{
		prepared: preparedBuild{app: checkout.App{Name: "crm", Dir: appDir}, version: version},
		binary:   binary,
		manifest: manifest,
	}
}

func realTarDeps(commands *[]seam.Cmd) seam.Deps {
	return seam.Deps{Exec: func(ctx context.Context, command seam.Cmd) (seam.Result, error) {
		if commands != nil {
			*commands = append(*commands, cloneCommand(command))
		}
		return seam.Exec(ctx, command)
	}}
}

func runTar(t *testing.T, directory string, arguments ...string) []byte {
	t.Helper()
	result, err := seam.Exec(context.Background(), seam.Cmd{Path: "tar", Args: arguments, Dir: directory})
	if err != nil {
		t.Fatalf("start tar: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("tar exit %d: %s", result.ExitCode, result.Stderr)
	}
	return result.Stdout
}

func writeTestFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertTestFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Errorf("close test root: %v", closeErr)
		}
	}()
	got, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, contents) {
		t.Fatalf("%s contents = %q, want %q", path, got, contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %04o, want %04o", path, info.Mode().Perm(), mode)
	}
}

func assertDistNames(t *testing.T, appDir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(appDir, "dist"))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dist entries = %#v, want %#v", got, want)
	}
}
