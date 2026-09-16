// Package host defines the injected boundary for host processes and time.
package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Env supplies the environment of a domain operation.
type Env struct {
	Root    string
	Getenv  func(string) string
	Execute func(context.Context, Command) (Result, error)
	Now     func() time.Time
}

// Command describes one directly executed process.
type Command struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin io.Reader
}

// Result retains a completed process's output and status.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// CommandError preserves the label and output of a failed command.
type CommandError struct {
	Label  string
	Result Result
	Err    error
}

func (e *CommandError) Error() string {
	if e.Err != nil {
		return e.Label + ": " + e.Err.Error()
	}
	return e.Label + ": exit status " + strconv.Itoa(e.Result.ExitCode)
}

func (e *CommandError) Unwrap() error { return e.Err }

// Exec executes a process without interpreting its arguments as shell text.
func Exec(ctx context.Context, command Command) (Result, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = append(os.Environ(), command.Env...)
	cmd.Stdin = command.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.ExitCode = exit.ExitCode()
		return result, nil
	}
	return result, err
}
