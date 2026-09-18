package space

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	// PolicyName is the name of the inline policy attached to a space role.
	PolicyName = "space"
	// RecordTTL is the lifetime of space address records, in seconds.
	RecordTTL = 60
	// PollInterval is the delay between cloud state polls.
	PollInterval = 5 * time.Second
	// PollAttempts is the maximum number of cloud state polls.
	PollAttempts = 60
)

// Run executes a space command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	invocation, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if invocation.help != "" {
		_, _ = fmt.Fprint(stdout, invocation.help)
		return nil
	}
	if invocation.subcommand == "list" || invocation.subcommand == "status" || invocation.subcommand == "stop" || invocation.subcommand == "start" {
		acct, err := account.Open(ctx, deps, profile)
		if err != nil {
			return err
		}
		switch invocation.subcommand {
		case "list":
			return runList(ctx, stdout, acct)
		case "status":
			return runStatus(ctx, invocation.domain, stdout, deps, acct)
		case "stop":
			return runStop(ctx, stdout, deps, acct, invocation.domain)
		case "start":
			return runStart(ctx, stdout, deps, acct, invocation.domain)
		}
	}

	return dispatch(ctx, invocation, stdout, deps, profile)
}

// Step reports a completed command step.
func Step(w io.Writer, name, detail string) {
	if detail == "" {
		_, _ = fmt.Fprintf(w, "%s: ok\n", name)
		return
	}
	_, _ = fmt.Fprintf(w, "%s: ok (%s)\n", name, detail)
}

// RoleName returns the IAM role name for a space.
func RoleName(domain string) string { return "ikigenba-space-" + domain }

// BackupPrefix returns the object-key prefix for a space's backups.
func BackupPrefix(domain string) string { return domain + "/" }

// RecordNames returns the apex and wildcard DNS names for a space.
func RecordNames(domain string) []string { return []string{domain, "*." + domain} }

// UsageError reports invalid space command syntax.
type UsageError struct {
	Message string
	Help    string
}

// Error returns the syntax error message.
func (e *UsageError) Error() string { return e.Message }

// Detail returns the command-specific usage hint.
func (e *UsageError) Detail() string { return fmt.Sprintf("see '%s' for usage", e.Help) }

// ExitCode returns the command-line usage error status.
func (e *UsageError) ExitCode() int { return 2 }

// NotRunningError reports that a space is not running.
type NotRunningError struct {
	Domain string
	State  cloud.InstanceState
}

// Error returns the space state diagnostic.
func (e *NotRunningError) Error() string {
	return fmt.Sprintf("'%s' is %s", e.Domain, e.State)
}

// WaitError reports an exhausted polling loop.
type WaitError struct {
	Subject string
	Want    string
}

// Error returns the polling timeout diagnostic.
func (e *WaitError) Error() string {
	return fmt.Sprintf("timed out waiting for %s to %s", e.Subject, e.Want)
}

// RetireStateError reports an instance state that prevents retirement.
type RetireStateError struct {
	ID      string
	State   cloud.InstanceState
	Domain  string
	Profile string
}

// Error returns the retirement state diagnostic.
func (e *RetireStateError) Error() string {
	return fmt.Sprintf("retire: instance %s is %s", e.ID, e.State)
}

// ExitCode returns the general command failure status.
func (e *RetireStateError) ExitCode() int { return 1 }

// Detail returns instructions for making retirement safe.
func (e *RetireStateError) Detail() string {
	return fmt.Sprintf(
		"run 'devctl --account %s space start %s' first, or pass --no-backup",
		e.Profile,
		e.Domain,
	)
}
