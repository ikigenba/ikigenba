// Package rollback relays a host's release rollback.
package rollback

import (
	"context"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const usageText = `Usage: devctl rollback <space>

Have opsctl on the space activate the release it ran before the current one
again: current points at previous, previous is removed, and every app is
restarted. With no previous release opsctl refuses, so a second rollback is
refused until the next deploy. What rollback does on the host is opsctl's.
`

// UsageError reports an invalid invocation.
type UsageError struct {
	Message string
	Help    string
}

func (e *UsageError) Error() string { return e.Message }

// Detail returns the usage advice for the invocation.
func (e *UsageError) Detail() string {
	if e.Help == "" {
		return ""
	}
	return "see '" + e.Help + "' for usage"
}

// ExitCode returns the usage error status.
func (*UsageError) ExitCode() int { return 2 }

// Run finds a running space and streams its rollback unchanged.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			_, err := io.WriteString(stdout, usageText)
			return err
		}
	}
	fail := func(message string) error { return &UsageError{Message: message, Help: "devctl rollback --help"} }
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return fail("unknown option '" + arg + "'")
		}
	}
	if len(args) == 0 {
		return fail("rollback needs <space>")
	}
	if len(args) > 1 {
		return fail("rollback takes only <space>")
	}
	root, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		return err
	}
	sp, err := spaceref.Parse(args[0], root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	target, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, sp.Domain)
	if err != nil {
		return err
	}
	if target.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: sp.Domain, State: target.State}
	}
	h := host.Host{Address: target.Address, Deps: deps}
	return h.StreamSudo(ctx, stdout, "rollback", "opsctl", "rollback")
}
