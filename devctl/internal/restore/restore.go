package restore

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const usage = `Usage: devctl restore <space> <app> [--at <timestamp>]

Have opsctl on the space put <app> back from the space's own backups. The
app's etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s unit is stopped for the restore and
started again after it.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

// UsageError reports invalid restore command syntax.
type UsageError struct {
	Message string
	Help    string
}

// Error returns the syntax error message.
func (e *UsageError) Error() string { return e.Message }

// ExitCode returns the command-line usage error status.
func (e *UsageError) ExitCode() int { return 2 }

// Detail returns the command-specific usage hint, if any.
func (e *UsageError) Detail() string {
	if e.Help == "" {
		return ""
	}
	return fmt.Sprintf("see '%s' for usage", e.Help)
}

type invocation struct {
	space string
	app   string
	at    string
	help  bool
}

type sudoer interface {
	Sudo(context.Context, string, ...string) (host.Output, error)
}

// Run restores one app on a running space.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	parsed, err := parse(args)
	if err != nil {
		return err
	}
	if parsed.help {
		_, err := io.WriteString(stdout, usage)
		return err
	}

	root, err := checkout.ReadRootFile(ctx, deps.Defaults())
	if err != nil {
		return err
	}
	spaceRef, err := spaceref.Parse(parsed.space, root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	item, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, spaceRef.Domain)
	if err != nil {
		return err
	}
	if item.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: spaceRef.Domain, State: item.State}
	}

	command := []string{"opsctl", "restore", parsed.app}
	if parsed.at != "" {
		command = append(command, "--at", parsed.at)
	}
	return restoreOnHost(ctx, stdout, host.Host{Address: item.Address, Deps: deps}, command)
}

func restoreOnHost(ctx context.Context, stdout io.Writer, remote sudoer, command []string) error {
	if _, err := remote.Sudo(ctx, "restore", command...); err != nil {
		return err
	}
	space.Step(stdout, "restore", strings.Join(command, " "))
	return nil
}

func parse(args []string) (invocation, error) {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return invocation{help: true}, nil
		}
	}

	var result invocation
	operands := make([]string, 0, 2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--at":
			value, ok := followingArgument(args, i)
			if !ok {
				return invocation{}, usageError("option '--at' requires a value", "devctl restore --help")
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return invocation{}, usageError("option '--at' requires a value", "devctl restore --help")
			}
			i++
			result.at = value
		case strings.HasPrefix(arg, "--at="):
			result.at = strings.TrimPrefix(arg, "--at=")
			if result.at == "" {
				return invocation{}, usageError("option '--at' requires a value", "devctl restore --help")
			}
		case strings.HasPrefix(arg, "-"):
			return invocation{}, usageError("unknown option '"+arg+"'", "devctl restore --help")
		default:
			operands = append(operands, arg)
		}
	}

	if len(operands) < 2 {
		return invocation{}, usageError("restore needs <space> and <app>", "devctl restore --help")
	}
	if len(operands) > 2 {
		return invocation{}, usageError("restore takes only <space> and <app>", "devctl restore --help")
	}
	if result.at != "" {
		if _, err := time.Parse(time.RFC3339, result.at); err != nil {
			return invocation{}, usageError("--at takes an RFC 3339 timestamp", "")
		}
	}
	result.space = operands[0]
	result.app = operands[1]
	return result, nil
}

func followingArgument(args []string, index int) (string, bool) {
	next := index + 1
	if next >= len(args) {
		return "", false
	}
	return args[next], true
}

func usageError(message, help string) *UsageError {
	return &UsageError{Message: message, Help: help}
}
