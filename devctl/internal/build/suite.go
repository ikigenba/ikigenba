package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func runSuite(ctx context.Context, opened *checkout.Checkout, operand, version string, stdout io.Writer, deps seam.Deps) error {
	sha, found, err := opened.ResolveCommit(ctx, operand)
	if err != nil {
		return err
	}
	if !found {
		return &UsageError{Message: fmt.Sprintf("'%s' is neither an app in the checkout nor a commit", operand)}
	}
	dist := opened.Path("dist")
	_, statErr := os.Stat(dist)
	created := os.IsNotExist(statErr)
	if statErr != nil && !created {
		return statErr
	}
	if err := os.MkdirAll(dist, 0o700); err != nil {
		return err
	}
	if created {
		defer func() { _ = os.Remove(dist) }()
	}
	worktree, err := os.MkdirTemp(dist, ".devctl-worktree-")
	if err != nil {
		return err
	}
	if err := os.Remove(worktree); err != nil {
		return err
	}
	if err := opened.AddWorktree(ctx, worktree, sha); err != nil {
		_ = os.RemoveAll(worktree)
		return err
	}
	stage, stageErr := os.MkdirTemp(dist, ".devctl-suite-")
	var archive string
	if stageErr == nil {
		defer func() { _ = os.RemoveAll(stage) }()
		archive, stageErr = stageSuite(ctx, worktree, stage, sha, version, deps)
	}
	removeErr := opened.RemoveWorktree(context.WithoutCancel(ctx), worktree)
	if removeErr == nil {
		_ = os.RemoveAll(worktree)
	}
	if stageErr != nil {
		return stageErr
	}
	if removeErr != nil {
		return removeErr
	}
	relative := ReleaseFile(sha)
	if err := os.Rename(archive, opened.Path(relative)); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, relative)
	return nil
}

func stageSuite(ctx context.Context, worktree, stage, sha, version string, deps seam.Deps) (string, error) {
	tree := checkout.Checkout{Root: worktree, Deps: deps}
	apps, err := tree.Apps()
	if err != nil {
		return "", err
	}
	for _, app := range apps {
		if !appref.ValidName(app.Name) || app.Name == "opsctl" {
			return "", &UsageError{Message: fmt.Sprintf("'%s' is not a usable app name", app.Name)}
		}
	}
	for _, app := range apps {
		for _, forbidden := range []string{"sbin", "include"} {
			info, err := os.Stat(filepath.Join(app.Dir, forbidden))
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
			if err == nil && info.IsDir() {
				return "", &UsageError{Message: app.Name + ": " + forbidden + "/ is not allowed in a release"}
			}
		}
	}
	archiveRoot := filepath.Join(stage, "members")
	releaseRoot := filepath.Join(archiveRoot, sha)
	for _, app := range apps {
		binary := filepath.Join(releaseRoot, app.Name, "bin", app.Name)
		if err := compileSuite(ctx, app.Name, app.Dir, binary, deps); err != nil {
			return "", err
		}
		manifest, err := executeStaged(ctx, deps, binary, app.Dir, app.Name+" manifest", "manifest")
		if err != nil {
			return "", err
		}
		committed, err := os.ReadFile(filepath.Join(app.Dir, checkout.ManifestFile))
		if err != nil {
			return "", err
		}
		if !bytes.Equal(manifest, committed) {
			return "", &StaleManifestError{App: app.Name}
		}
		destination := filepath.Join(releaseRoot, app.Name)
		if err := writeArchiveFile(filepath.Join(destination, checkout.ManifestFile), manifest, 0o644); err != nil {
			return "", err
		}
		for _, directory := range []string{"etc", "share", "libexec", "lib"} {
			if _, err := copyArchiveDirectory(app.Dir, destination, directory); err != nil {
				return "", err
			}
		}
	}
	if err := compileSuite(ctx, "opsctl", filepath.Join(worktree, "opsctl"), filepath.Join(releaseRoot, "opsctl", "bin", "opsctl"), deps); err != nil {
		return "", err
	}
	metadata, err := json.Marshal(struct {
		SHA    string `json:"sha"`
		Built  string `json:"built"`
		Devctl string `json:"devctl"`
	}{sha, deps.Now().UTC().Truncate(time.Second).Format(time.RFC3339), version})
	if err != nil {
		return "", err
	}
	if err := writeArchiveFile(filepath.Join(releaseRoot, "release.json"), metadata, 0o644); err != nil {
		return "", err
	}
	archive := filepath.Join(stage, "release.tar.xz")
	if err := execute(ctx, deps, "archive release", seam.Cmd{Path: "tar", Args: []string{"-cJf", archive, "-C", archiveRoot, "--", sha}, Dir: worktree}); err != nil {
		return "", err
	}
	return archive, nil
}

func compileSuite(ctx context.Context, name, dir, binary string, deps seam.Deps) error {
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		return err
	}
	if err := execute(ctx, deps, "build "+name, seam.Cmd{Path: "go", Args: []string{"build", "-buildvcs=false", "-o", binary, "./cmd/" + name}, Dir: dir, Env: []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOWORK=off"}}); err != nil {
		return err
	}
	info, err := os.Stat(binary)
	if err != nil {
		return err
	}
	return os.Chmod(binary, info.Mode().Perm()|0o111)
}
