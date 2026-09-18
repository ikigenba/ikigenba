package build

import (
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
		Args: []string{"build", "-o", stagedBinary, "."},
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

// archivePrepared is the handoff to archive construction and publication.
func archivePrepared(_ context.Context, _ stagedBuild, _ io.Writer, _ seam.Deps) error {
	return nil
}
