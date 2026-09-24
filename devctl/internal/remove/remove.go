package remove

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const helpText = `Usage: devctl remove <space> <app>

Have opsctl on the space take <app> off it: stop and remove its socket and
service, remove its binary and configuration, and stop routing its name. Its
state/ is kept on the host and its secrets are kept in the account, so a later
deploy of <app> lands over its data. What remove does on the host is opsctl's.
`

// UsageError reports invalid remove command syntax.
type UsageError struct {
	Message string
	Help    string
}

// Error returns the syntax error message.
func (e *UsageError) Error() string { return e.Message }

// ExitCode returns the command-line usage error status.
func (e *UsageError) ExitCode() int { return 2 }

// Detail returns the command-specific usage hint.
func (e *UsageError) Detail() string { return fmt.Sprintf("see '%s' for usage", e.Help) }

type invocation struct {
	space string
	app   string
	help  bool
}

// Run removes one app from a running space through opsctl.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	parsed, err := parse(args)
	if err != nil {
		return err
	}
	if parsed.help {
		_, err := io.WriteString(stdout, helpText)
		return err
	}

	root, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		return err
	}
	targetRef, err := spaceref.Parse(parsed.space, root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	target, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, targetRef.Domain)
	if err != nil {
		return err
	}
	if target.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: targetRef.Domain, State: target.State}
	}

	if _, err := (host.Host{Address: target.Address, Deps: deps}).Sudo(ctx, "remove", "opsctl", "uninstall", parsed.app); err != nil {
		return err
	}
	space.Step(stdout, "remove", "opsctl uninstalled "+parsed.app)
	return nil
}

func parse(args []string) (invocation, error) {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return invocation{help: true}, nil
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return invocation{}, usage("unknown option '" + arg + "'")
		}
	}
	if len(args) < 2 {
		return invocation{}, usage("remove needs <space> and <app>")
	}
	if len(args) > 2 {
		return invocation{}, usage("remove takes only <space> and <app>")
	}
	return invocation{space: args[0], app: args[1]}, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: "devctl remove --help"}
}
