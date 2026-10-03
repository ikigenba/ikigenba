// Package git runs the host's git with the environment supplied by its caller.
package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Fixed buffer limits keep the HTTP bridge independent of the pack's size.
const (
	CopyBufferSize = 32768
	MaxHeaderBytes = 65536
)

// ErrNotFound means no executable git was found on the supplied search path.
var ErrNotFound = errors.New("git not found")

// Git holds an executable and the environment source for its commands.
type Git struct {
	path    string
	environ func() []string
}

// Find searches absolute directories in path without starting a process.
func Find(path string, environ func() []string) (*Git, error) {
	for _, dir := range strings.Split(path, ":") {
		if !filepath.IsAbs(dir) {
			continue
		}
		name := filepath.Join(dir, "git")
		info, err := os.Stat(name)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return &Git{path: name, environ: environ}, nil
		}
	}
	return nil, ErrNotFound
}

// Command builds an unstarted git whose process group ends with ctx.
func (g *Git) Command(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, g.path, args...)
	cmd.Dir = dir
	cmd.Env = make([]string, 0)
	if g.environ != nil {
		cmd.Env = append(cmd.Env, g.environ()...)
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd
}

// Output runs git and returns its standard output and execution error.
func (g *Git) Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return g.Command(ctx, dir, nil, args...).Output()
}
