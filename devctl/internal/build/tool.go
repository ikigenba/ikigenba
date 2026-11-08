package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// Tool compiles one command at a resolved commit for the requested architecture.
func Tool(ctx context.Context, c *checkout.Checkout, sha, name, goarch, out string) error {
	dist := c.Path("dist")
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
	dir, err := os.MkdirTemp(dist, ".devctl-tool-")
	if err != nil {
		return err
	}
	if err := os.Remove(dir); err != nil {
		return err
	}
	if err := c.AddWorktree(ctx, dir, sha); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	result, execErr := c.Deps.Defaults().Exec(ctx, seam.Cmd{
		Path: "go", Dir: filepath.Join(dir, name),
		Args: []string{"build", "-buildvcs=false", "-o", out, "./cmd/" + name},
		Env:  []string{"GOOS=linux", "GOARCH=" + goarch, "CGO_ENABLED=0", "GOWORK=off"},
	})
	var compileErr error
	if execErr != nil {
		compileErr = errors.New("go: " + execErr.Error())
	} else if result.ExitCode != 0 {
		compileErr = &ProcessError{Label: "build " + name, Status: result.ExitCode, Stderr: string(result.Stderr)}
	}
	removeErr := c.RemoveWorktree(context.WithoutCancel(ctx), dir)
	if removeErr == nil {
		_ = os.RemoveAll(dir)
	}
	if compileErr != nil {
		return compileErr
	}
	return removeErr
}
