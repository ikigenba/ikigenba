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

func runSuite(ctx context.Context, opened *checkout.Checkout, operand, version string, stdout io.Writer) error {
	sha, found, err := opened.ResolveCommit(ctx, operand)
	if err != nil {
		return err
	}
	if !found {
		return &UsageError{Message: fmt.Sprintf("'%s' is not a commit", operand)}
	}
	release, err := Suite(ctx, opened, sha, version)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, release.File)
	return nil
}

// Release is the suite artifact and the manifests it contains.
type Release struct {
	SHA       string
	File      string
	Manifests []checkout.Manifest
}

// Suite builds a resolved commit without writing command output.
func Suite(ctx context.Context, opened *checkout.Checkout, sha, version string) (Release, error) {
	deps := opened.Deps.Defaults()
	dist := opened.Path("dist")
	_, statErr := os.Stat(dist)
	created := os.IsNotExist(statErr)
	if statErr != nil && !created {
		return Release{}, statErr
	}
	if err := os.MkdirAll(dist, 0o700); err != nil {
		return Release{}, err
	}
	if created {
		defer func() { _ = os.Remove(dist) }()
	}
	worktree, err := os.MkdirTemp(dist, ".devctl-worktree-")
	if err != nil {
		return Release{}, err
	}
	if err := os.Remove(worktree); err != nil {
		return Release{}, err
	}
	if err := opened.AddWorktree(ctx, worktree, sha); err != nil {
		_ = os.RemoveAll(worktree)
		return Release{}, err
	}
	stage, stageErr := os.MkdirTemp(dist, ".devctl-suite-")
	var archive string
	var manifests []checkout.Manifest
	if stageErr == nil {
		defer func() { _ = os.RemoveAll(stage) }()
		archive, manifests, stageErr = stageSuite(ctx, worktree, stage, sha, version, deps)
	}
	removeErr := opened.RemoveWorktree(context.WithoutCancel(ctx), worktree)
	if removeErr == nil {
		_ = os.RemoveAll(worktree)
	}
	if stageErr != nil {
		return Release{}, stageErr
	}
	if removeErr != nil {
		return Release{}, removeErr
	}
	relative := ReleaseFile(sha)
	if err := os.Rename(archive, opened.Path(relative)); err != nil {
		return Release{}, err
	}
	return Release{SHA: sha, File: relative, Manifests: manifests}, nil
}

func stageSuite(ctx context.Context, worktree, stage, sha, version string, deps seam.Deps) (string, []checkout.Manifest, error) {
	tree := checkout.Checkout{Root: worktree, Deps: deps}
	apps, err := tree.Apps()
	if err != nil {
		return "", nil, err
	}
	for _, app := range apps {
		if !appref.ValidName(app.Name) || app.Name == "opsctl" {
			return "", nil, &UsageError{Message: fmt.Sprintf("'%s' is not a usable app name", app.Name)}
		}
	}
	for _, app := range apps {
		for _, forbidden := range []string{"sbin", "include"} {
			info, err := os.Stat(filepath.Join(app.Dir, forbidden))
			if err != nil && !os.IsNotExist(err) {
				return "", nil, err
			}
			if err == nil && info.IsDir() {
				return "", nil, &UsageError{Message: app.Name + ": " + forbidden + "/ is not allowed in a release"}
			}
		}
	}
	archiveRoot := filepath.Join(stage, "members")
	releaseRoot := filepath.Join(archiveRoot, sha)
	var manifests []checkout.Manifest
	for _, app := range apps {
		binary := filepath.Join(releaseRoot, app.Name, "bin", app.Name)
		if err := compileSuite(ctx, app.Name, app.Dir, binary, deps); err != nil {
			return "", nil, err
		}
		manifest, err := executeStaged(ctx, deps, binary, app.Dir, app.Name+" manifest", "manifest")
		if err != nil {
			return "", nil, err
		}
		committed, err := os.ReadFile(filepath.Join(app.Dir, checkout.ManifestFile))
		if err != nil {
			return "", nil, err
		}
		if !bytes.Equal(manifest, committed) {
			return "", nil, &StaleManifestError{App: app.Name}
		}
		decoded, err := checkout.DecodeManifest(bytes.NewReader(manifest))
		if err != nil {
			return "", nil, err
		}
		manifests = append(manifests, decoded)
		destination := filepath.Join(releaseRoot, app.Name)
		if err := writeArchiveFile(filepath.Join(destination, checkout.ManifestFile), manifest, 0o644); err != nil {
			return "", nil, err
		}
		for _, directory := range []string{"etc", "share", "libexec", "lib"} {
			if _, err := copyArchiveDirectory(app.Dir, destination, directory); err != nil {
				return "", nil, err
			}
		}
	}
	if err := compileSuite(ctx, "opsctl", filepath.Join(worktree, "opsctl"), filepath.Join(releaseRoot, "opsctl", "bin", "opsctl"), deps); err != nil {
		return "", nil, err
	}
	metadata, err := json.Marshal(struct {
		SHA    string `json:"sha"`
		Built  string `json:"built"`
		Devctl string `json:"devctl"`
	}{sha, deps.Now().UTC().Truncate(time.Second).Format(time.RFC3339), version})
	if err != nil {
		return "", nil, err
	}
	if err := writeArchiveFile(filepath.Join(releaseRoot, "release.json"), metadata, 0o644); err != nil {
		return "", nil, err
	}
	archive := filepath.Join(stage, "release.tar.xz")
	if err := execute(ctx, deps, "archive release", seam.Cmd{Path: "tar", Args: []string{"-cJf", archive, "-C", archiveRoot, "--", sha}, Dir: worktree}); err != nil {
		return "", nil, err
	}
	return archive, manifests, nil
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
