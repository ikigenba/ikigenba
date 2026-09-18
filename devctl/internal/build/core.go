package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type preparedBuild struct {
	app     checkout.App
	version string
}

type stagedBuild struct {
	prepared preparedBuild
	binary   string
	manifest []byte
}

func run(ctx context.Context, name string, stdout io.Writer, deps seam.Deps) error {
	prepared, err := prepareBuild(ctx, name, deps)
	if err != nil {
		return err
	}
	return runPrepared(ctx, prepared, stdout, deps)
}

func prepareBuild(ctx context.Context, name string, deps seam.Deps) (preparedBuild, error) {
	if !appref.ValidName(name) {
		return preparedBuild{}, &UsageError{
			Message: fmt.Sprintf("'%s' is not a usable app name", name),
		}
	}

	opened, err := checkout.Open(ctx, deps)
	if err != nil {
		return preparedBuild{}, err
	}
	app, err := opened.App(name)
	if err != nil {
		return preparedBuild{}, err
	}
	clean, err := opened.Clean(ctx)
	if err != nil {
		return preparedBuild{}, err
	}
	if !clean {
		return preparedBuild{}, &UsageError{
			Message: "the working tree has uncommitted changes; commit them first",
		}
	}

	head, err := opened.Head(ctx)
	if err != nil {
		return preparedBuild{}, err
	}
	tags, err := opened.TagsAtHead(ctx)
	if err != nil {
		return preparedBuild{}, err
	}
	sort.Strings(tags)
	for _, tag := range tags {
		if version, ok := appref.VersionForTag(name, tag); ok {
			return preparedBuild{app: app, version: version}, nil
		}
	}

	return preparedBuild{}, &UsageError{
		Message: fmt.Sprintf("no tag %s/v<semver> points at HEAD (%s)", name, head),
	}
}

func runPrepared(ctx context.Context, prepared preparedBuild, stdout io.Writer, deps seam.Deps) error {
	staged, cleanup, err := stagePrepared(ctx, prepared, deps)
	if err != nil {
		return err
	}
	defer cleanup()
	return archivePrepared(ctx, staged, stdout, deps)
}

func stagePrepared(ctx context.Context, prepared preparedBuild, deps seam.Deps) (stagedBuild, func(), error) {
	stagedBinary, cleanup, err := compile(ctx, prepared.app, deps)
	emptyCleanup := func() {}
	if err != nil {
		return stagedBuild{}, emptyCleanup, err
	}

	version, err := executeStaged(ctx, deps, stagedBinary, prepared.app.Dir, prepared.app.Name+" --version", "--version")
	if err != nil {
		cleanup()
		return stagedBuild{}, emptyCleanup, err
	}
	reported := strings.TrimRight(string(version), "\n")
	if reported != prepared.version {
		cleanup()
		return stagedBuild{}, emptyCleanup, &UsageError{Message: fmt.Sprintf(
			"%s: tagged %s/%s but the binary reports %s",
			prepared.app.Name,
			prepared.app.Name,
			prepared.version,
			reported,
		)}
	}

	manifest, err := executeStaged(ctx, deps, stagedBinary, prepared.app.Dir, prepared.app.Name+" manifest", "manifest")
	if err != nil {
		cleanup()
		return stagedBuild{}, emptyCleanup, err
	}

	return stagedBuild{prepared: prepared, binary: stagedBinary, manifest: manifest}, cleanup, nil
}

func compile(ctx context.Context, app checkout.App, deps seam.Deps) (string, func(), error) {
	dist := filepath.Join(app.Dir, "dist")
	_, statErr := os.Stat(dist)
	distCreated := false
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			return "", func() {}, statErr
		}
		if err := os.MkdirAll(dist, 0o700); err != nil {
			return "", func() {}, err
		}
		distCreated = true
	}

	stageDir, err := os.MkdirTemp(dist, ".devctl-build-")
	if err != nil {
		if distCreated {
			_ = os.Remove(dist)
		}
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(stageDir)
		if distCreated {
			_ = os.Remove(dist)
		}
	}
	stagedBinary := filepath.Join(stageDir, app.Name)
	command := seam.Cmd{
		Path: "go",
		Args: []string{"build", "-o", stagedBinary, "./cmd/" + app.Name},
		Dir:  app.Dir,
		Env:  []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
	}
	if err := execute(ctx, deps, "build "+app.Name, command); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return stagedBinary, cleanup, nil
}

func executeStaged(ctx context.Context, deps seam.Deps, path, dir, label, argument string) ([]byte, error) {
	command := seam.Cmd{Path: path, Args: []string{argument}, Dir: dir}
	result, err := executeResult(ctx, deps, label, command)
	return result.Stdout, err
}

func execute(ctx context.Context, deps seam.Deps, label string, command seam.Cmd) error {
	_, err := executeResult(ctx, deps, label, command)
	return err
}

func executeResult(ctx context.Context, deps seam.Deps, label string, command seam.Cmd) (seam.Result, error) {
	result, err := deps.Exec(ctx, command)
	if err != nil {
		return seam.Result{}, fmt.Errorf("%s: %w", command.Path, err)
	}
	if result.ExitCode != 0 {
		return seam.Result{}, &ProcessError{
			Label:  label,
			Status: result.ExitCode,
			Stderr: string(result.Stderr),
		}
	}
	return result, nil
}

// archivePrepared constructs and publishes the archive for a validated build.
func archivePrepared(ctx context.Context, staged stagedBuild, stdout io.Writer, deps seam.Deps) error {
	app := staged.prepared.app
	committedManifest, err := os.ReadFile(filepath.Join(app.Dir, checkout.ManifestFile))
	if err != nil {
		return err
	}
	if !bytes.Equal(staged.manifest, committedManifest) {
		return &StaleManifestError{App: app.Name}
	}

	dist := filepath.Join(app.Dir, "dist")
	archiveRoot, err := os.MkdirTemp(dist, ".devctl-archive-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(archiveRoot) }()

	members := make([]string, 0)
	binaryMember := filepath.Join("bin", app.Name)
	if err := copyArchiveFile(staged.binary, filepath.Join(archiveRoot, binaryMember), true); err != nil {
		return err
	}
	members = append(members, filepath.ToSlash(binaryMember))

	manifestMember := filepath.FromSlash(checkout.ManifestFile)
	if err := writeArchiveFile(filepath.Join(archiveRoot, manifestMember), staged.manifest, 0o644); err != nil {
		return err
	}
	members = append(members, filepath.ToSlash(manifestMember))

	for _, directory := range []string{"etc", "share"} {
		directoryMembers, walkErr := copyArchiveDirectory(app.Dir, archiveRoot, directory)
		if walkErr != nil {
			return walkErr
		}
		members = append(members, directoryMembers...)
	}
	sort.Strings(members)

	temporary, err := os.CreateTemp(dist, ".devctl-artifact-*.tar.xz")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if closeErr := temporary.Close(); closeErr != nil {
		_ = os.Remove(temporaryPath)
		return closeErr
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporaryPath) }()

	arguments := []string{"-cJf", temporaryPath, "-C", archiveRoot, "--"}
	arguments = append(arguments, members...)
	if err := execute(ctx, deps, "archive "+app.Name, seam.Cmd{
		Path: "tar",
		Args: arguments,
		Dir:  app.Dir,
	}); err != nil {
		return err
	}

	finalRelative := File(app.Name, staged.prepared.version)
	finalPath := filepath.Join(app.Dir, "dist", filepath.Base(finalRelative))
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return err
	}
	// Publishing is the commit point. A writer failure cannot be reported after
	// the artifact has replaced its predecessor, and writing before the rename
	// could expose success output for a failed publication.
	_, _ = fmt.Fprintln(stdout, finalRelative)
	return nil
}

func copyArchiveDirectory(appDir, archiveRoot, directory string) ([]string, error) {
	sourceRoot := filepath.Join(appDir, directory)
	if _, err := os.Stat(sourceRoot); err != nil {
		if os.IsNotExist(err) && directory == "share" {
			return nil, nil
		}
		return nil, err
	}

	var members []string
	err := filepath.WalkDir(sourceRoot, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(appDir, source)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == checkout.ManifestFile {
			return nil
		}
		if err := copyArchiveFile(source, filepath.Join(archiveRoot, relative), false); err != nil {
			return err
		}
		members = append(members, filepath.ToSlash(relative))
		return nil
	})
	return members, err
}

func copyArchiveFile(source, destination string, executable bool) error {
	sourceRoot, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	contents, err := sourceRoot.ReadFile(filepath.Base(source))
	closeErr := sourceRoot.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if executable {
		mode |= 0o111
	}
	return writeArchiveFile(destination, contents, mode)
}

func writeArchiveFile(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
