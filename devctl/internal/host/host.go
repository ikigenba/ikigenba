package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	// User is the account used for host SSH connections.
	User = "ec2-user"
	// ProbeInterval is the wait between SSH probes.
	ProbeInterval = 5 * time.Second
	// ProbeAttempts is the maximum number of SSH probes.
	ProbeAttempts = 12
)

// Host is a remote deployment host.
type Host struct {
	Address string
	Deps    seam.Deps
}

// Output is the output captured from a remote command.
type Output struct {
	Stdout string
	Stderr string
}

// CommandError reports a remote command that exited unsuccessfully.
type CommandError struct {
	Step    string
	Command []string
	Status  int
	Stdout  string
	Stderr  string
}

func (e *CommandError) Error() string {
	prefix := ""
	if e.Step != "" {
		prefix = e.Step + ": "
	}
	return prefix + strings.Join(e.Command, " ") + ": exit status " + strconv.Itoa(e.Status)
}

// Detail returns the command's diagnostic output, preferring stderr.
func (e *CommandError) Detail() string {
	if strings.TrimSpace(e.Stderr) != "" {
		return seam.QuoteOutput(e.Stderr)
	}
	return seam.QuoteOutput(e.Stdout)
}

// ExitCode returns the devctl exit status for a failed remote command.
func (e *CommandError) ExitCode() int { return 1 }

// UnreachableError reports a host that did not accept an SSH probe in time.
type UnreachableError struct {
	Address string
}

func (e *UnreachableError) Error() string {
	return "ssh " + User + "@" + e.Address + ": connection timed out"
}

// Target returns the SSH target for the host.
func (h Host) Target() string { return User + "@" + h.Address }

// Run runs a command on the host.
func (h Host) Run(ctx context.Context, step string, args ...string) (Output, error) {
	return h.run(ctx, step, args)
}

// Sudo runs a command as root on the host.
func (h Host) Sudo(ctx context.Context, step string, args ...string) (Output, error) {
	return h.run(ctx, step, append([]string{"sudo"}, args...))
}

// StreamSudo runs a command as root and streams its standard output.
func (h Host) StreamSudo(ctx context.Context, stdout io.Writer, step string, args ...string) error {
	logical := append([]string{"sudo"}, args...)
	deps := h.Deps.Defaults()
	result, err := deps.Stream(ctx, h.command(logical), stdout)
	if err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	if result.ExitCode != 0 {
		return &CommandError{
			Step:    step,
			Command: h.logicalCommand(logical),
			Status:  result.ExitCode,
			Stderr:  string(result.Stderr),
		}
	}
	return nil
}

// Wait waits until the host accepts an SSH command.
func (h Host) Wait(ctx context.Context) error {
	deps := h.Deps.Defaults()
	h.Deps = deps
	for attempt := 0; attempt < ProbeAttempts; attempt++ {
		_, err := h.Run(ctx, "", "true")
		if err == nil {
			return nil
		}
		var commandErr *CommandError
		if !errors.As(err, &commandErr) {
			return err
		}
		if attempt+1 == ProbeAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deps.After(ProbeInterval):
		}
	}
	return &UnreachableError{Address: h.Address}
}

func (h Host) run(ctx context.Context, step string, logical []string) (Output, error) {
	deps := h.Deps.Defaults()
	result, err := deps.Exec(ctx, h.command(logical))
	if err != nil {
		return Output{}, fmt.Errorf("ssh: %w", err)
	}
	output := Output{Stdout: string(result.Stdout), Stderr: string(result.Stderr)}
	if result.ExitCode != 0 {
		return Output{}, &CommandError{
			Step:    step,
			Command: h.logicalCommand(logical),
			Status:  result.ExitCode,
			Stdout:  output.Stdout,
			Stderr:  output.Stderr,
		}
	}
	return output, nil
}

func (h Host) command(logical []string) seam.Cmd {
	return seam.Cmd{
		Path: "ssh",
		Args: []string{
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=accept-new",
			h.Target(),
			quoteCommand(logical),
		},
		Dir: h.Deps.Dir,
	}
}

func (h Host) logicalCommand(logical []string) []string {
	command := make([]string, 0, len(logical)+2)
	command = append(command, "ssh", h.Target())
	return append(command, logical...)
}

func quoteCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
