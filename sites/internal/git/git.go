// Package git runs the host's git with an explicitly supplied environment.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrNotFound means the selected git executable could not be found or started.
var ErrNotFound = errors.New("git not found")

// Git holds an executable selection and its environment source.
type Git struct {
	path    string
	environ func() []string
}

// Find selects the first executable git in an absolute PATH directory.
func Find(path string, environ func() []string) (*Git, error) {
	for _, dir := range strings.Split(path, ":") {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "git")
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return &Git{path: candidate, environ: environ}, nil
		}
	}
	return nil, ErrNotFound
}

// Error reports the exit status and unmodified standard error of a failed git.
type Error struct {
	Status int
	Stderr string
}

func (e *Error) Error() string { return "git exited with status " + strconv.Itoa(e.Status) }

// Command prepares git without attaching any standard streams.
func (g *Git) Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, g.path, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{}, g.environ()...)
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

// Output runs git and returns its standard output, or its contextual failure.
func (g *Git) Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	cmd := g.Command(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if cause := context.Cause(ctx); cause != nil {
		return output, cause
	}
	if err == nil {
		return output, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return output, &Error{Status: exit.ExitCode(), Stderr: stderr.String()}
	}
	return output, fmt.Errorf("%w: %w", ErrNotFound, err)
}
