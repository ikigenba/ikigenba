// Package seam provides turns' dependencies on the running process.
package seam

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Cmd describes an external process.
type Cmd struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// Result records the streams and exit status of a completed process.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner executes one external process.
type Runner func(ctx context.Context, cmd Cmd) (Result, error)

// Deps holds every dependency supplied by the invoking process.
type Deps struct {
	EUID     int
	Getenv   func(key string) string
	Dir      string
	Arg0     string
	Etc      string
	Now      func() time.Time
	Hostname func() (string, error)
	HTTP     *http.Client
	Exec     Runner
}

// Process captures the invoking process, leaving optional dependencies at their defaults.
func Process() Deps {
	dir, _ := os.Getwd()
	return Deps{EUID: os.Geteuid(), Getenv: os.Getenv, Dir: dir, Arg0: os.Args[0], Etc: "/etc"}
}

// Exec runs a command with empty input, returning completed exit statuses as results.
func Exec(ctx context.Context, cmd Cmd) (Result, error) {
	if cmd.Path == "" {
		return Result{}, errors.New("empty command path")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	path := cmd.Path
	if !strings.Contains(path, "/") {
		var err error
		path, err = exec.LookPath(path)
		if err != nil {
			return Result{}, err
		}
	}
	var stdout, stderr bytes.Buffer
	process := exec.Cmd{
		Path: path, Args: append([]string{cmd.Path}, cmd.Args...), Dir: cmd.Dir,
		Env: append(os.Environ(), cmd.Env...), Stdout: &stdout, Stderr: &stderr,
		WaitDelay: 100 * time.Millisecond,
	}
	if err := process.Start(); err != nil {
		return Result{}, err
	}
	completed := make(chan error, 1)
	go func() { completed <- process.Wait() }()
	var err, canceled error
	select {
	case err = <-completed:
	case <-ctx.Done():
		select {
		case err = <-completed:
		default:
			if killErr := process.Process.Kill(); killErr == nil {
				canceled = ctx.Err()
			}
			err = <-completed
		}
	}
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if process.ProcessState != nil {
		result.ExitCode = process.ProcessState.ExitCode()
	}
	if canceled != nil {
		return result, canceled
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) || errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	return result, err
}

// QuoteOutput prefixes each remaining line after removing trailing LF bytes.
func QuoteOutput(text string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	return "> " + strings.ReplaceAll(text, "\n", "\n> ")
}
