// Package seam contains the program's environmental dependencies.
package seam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

// QuoteOutput marks every line of external program output as quoted detail.
func QuoteOutput(text string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	return "> " + strings.ReplaceAll(text, "\n", "\n> ")
}

// Cmd describes a process invocation.
type Cmd struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// Result describes a completed process.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner runs a process and captures its output.
type Runner func(ctx context.Context, cmd Cmd) (Result, error)

// StreamRunner runs a process while streaming its standard output.
type StreamRunner func(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)

// Deps contains all environmental dependencies used by the application.
type Deps struct {
	Dir    string
	EUID   int
	Getenv func(key string) string
	Cloud  cloud.Opener
	Exec   Runner
	Stream StreamRunner
	Now    func() time.Time
	After  func(d time.Duration) <-chan time.Time
}

// Defaults replaces nil environmental functions with their real or inert
// implementations. It leaves explicitly supplied dependencies unchanged.
func (deps Deps) Defaults() Deps {
	if deps.Getenv == nil {
		deps.Getenv = func(string) string { return "" }
	}
	if deps.Exec == nil {
		deps.Exec = Exec
	}
	if deps.Stream == nil {
		deps.Stream = Stream
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.After == nil {
		deps.After = time.After
	}
	return deps
}

// Exec runs cmd and captures its standard output and standard error.
func Exec(ctx context.Context, command Cmd) (Result, error) {
	process, err := prepare(ctx, command)
	if err != nil {
		return Result{}, err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err = process.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ctxErr := ctx.Err(); ctxErr != nil && exitErr.ExitCode() < 0 {
			return result, ctxErr
		}
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	return result, err
}

// Stream runs cmd, writing standard output as it is produced and capturing
// standard error. Its returned Result never contains standard output.
func Stream(ctx context.Context, command Cmd, stdout io.Writer) (Result, error) {
	if stdout == nil {
		return Result{}, errors.New("stdout writer is nil")
	}
	process, err := prepare(ctx, command)
	if err != nil {
		return Result{}, err
	}

	pipe, err := process.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	process.Stderr = &stderr
	if err := process.Start(); err != nil {
		return Result{}, err
	}

	_, copyErr := io.Copy(stdout, pipe)
	if copyErr != nil {
		_ = process.Process.Kill()
	}
	waitErr := process.Wait()
	result := Result{Stderr: stderr.Bytes()}
	if copyErr != nil {
		return result, copyErr
	}
	if waitErr == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if ctxErr := ctx.Err(); ctxErr != nil && exitErr.ExitCode() < 0 {
			return result, ctxErr
		}
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	return result, waitErr
}

func prepare(ctx context.Context, command Cmd) (*exec.Cmd, error) {
	if command.Path == "" {
		return nil, errors.New("command path is empty")
	}
	if command.Dir == "" {
		return nil, errors.New("command directory is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := exec.LookPath(command.Path)
	if err != nil {
		return nil, fmt.Errorf("start %q: %w", command.Path, err)
	}
	// CommandContext supplies the cancellation machinery. Path and Args are
	// then replaced with the already resolved, caller-requested command.
	process := exec.CommandContext(ctx, "true")
	process.Path = path
	process.Args = append([]string{command.Path}, command.Args...)
	process.Dir = command.Dir
	process.Env = append(os.Environ(), command.Env...)
	return process, nil
}

var _ Runner = Exec
var _ StreamRunner = Stream
