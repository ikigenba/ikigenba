package remove

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const helpText = `Usage: devctl --account <name> remove <domain> <app>

Have opsctl on <domain> take <app> off the space: stop and remove its service,
remove its binary and configuration, and stop routing its name. Its state/ is
kept on the host and its secrets are kept in the account, so a later deploy of
<app> lands over its data. What remove does on the host is opsctl's.
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
	domain string
	app    string
	help   bool
}

// Run removes one app from a running space through opsctl.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	parsed, err := parse(args)
	if err != nil {
		return err
	}
	if parsed.help {
		_, err := io.WriteString(stdout, helpText)
		return err
	}

	acct, err := account.Open(ctx, deps, profile)
	if err != nil {
		return err
	}
	target, err := acct.Space(ctx, parsed.domain)
	if err != nil {
		return err
	}
	if target.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: parsed.domain, State: target.State}
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
		return invocation{}, usage("remove needs <domain> and <app>")
	}
	if len(args) > 2 {
		return invocation{}, usage("remove takes only <domain> and <app>")
	}
	return invocation{domain: args[0], app: args[1]}, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: "devctl remove --help"}
}
