// Package host defines the injected boundary for host processes and time.
package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// Apex returns the parent domain of a host name with at least three labels.
func Apex(hostName string) (string, error) {
	labels := strings.Split(hostName, ".")
	if len(labels) >= 3 {
		for _, label := range labels {
			if label == "" {
				return "", fmt.Errorf("host.apex is set but host.name '%s' has no parent domain", hostName)
			}
		}
		return strings.Join(labels[1:], "."), nil
	}
	return "", fmt.Errorf("host.apex is set but host.name '%s' has no parent domain", hostName)
}

// NormalizeName applies the stored host-name spelling convention.
func NormalizeName(name string) string {
	normalized := []byte(name)
	for i, char := range normalized {
		if char >= 'A' && char <= 'Z' {
			normalized[i] = char + ('a' - 'A')
		}
	}
	if len(normalized) > 0 && normalized[len(normalized)-1] == '.' {
		normalized = normalized[:len(normalized)-1]
	}
	return string(normalized)
}

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
