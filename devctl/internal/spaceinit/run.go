package spaceinit

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const usageText = `Usage: devctl space init <space> [--opsctl <version>] [--acme-email <address>]

Set the host's five derived keys again and run opsctl init. Keep its installed
opsctl, its CA address, and its backup periods unless an option names a
replacement. Cloud resources, records, and secrets are unchanged.

Options:
  --opsctl <version>      move the host to this opsctl release first
  --acme-email <address>  change where the CA sends the space's expiry warnings
`

type invocation struct {
	space     string
	opsctl    string
	acmeEmail string
	help      bool
}

// Run executes a space init command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	invocation, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if invocation.help {
		_, err := io.WriteString(stdout, usageText)
		return err
	}

	root, err := checkout.ReadRootFile(ctx, deps)
	if err != nil {
		return err
	}
	sp, err := spaceref.Parse(invocation.space, root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	instance, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, sp.Domain)
	if err != nil {
		return err
	}
	if instance.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: instance.Domain, State: instance.State}
	}
	zone, err := session.Clients.Route53.Zone(ctx, root.Domain)
	if err != nil {
		return err
	}

	space.Step(stdout, "account", fmt.Sprintf("%s, %s, %s", root.Domain, root.Region, session.AccountID))
	space.Step(stdout, "domain", fmt.Sprintf("zone %s %s", zone.Name, zone.ID))
	space.Step(stdout, "instance", fmt.Sprintf("%s running, %s", instance.ID, instance.Address))

	target := host.Host{Address: instance.Address, Deps: deps}
	version := invocation.opsctl
	action := "installed"
	if version != "" {
		if err := hostsetup.Upgrade(ctx, target, version); err != nil {
			return err
		}
	} else {
		action = "kept"
		version, err = hostsetup.Version(ctx, target)
		if err != nil {
			return err
		}
	}
	configured, err := hostsetup.Configure(ctx, target, "opsctl", hostsetup.Config{
		Root:    root.Domain,
		Region:  root.Region,
		ZoneID:  zone.ID,
		Space:   sp,
		Email:   invocation.acmeEmail,
		Periods: nil,
	})
	if err != nil {
		return err
	}
	space.Step(stdout, "opsctl", fmt.Sprintf("%s %s, %d keys set", version, action, configured))

	if _, err := target.Sudo(ctx, "init", "opsctl", "init"); err != nil {
		return err
	}
	space.Step(stdout, "init", "")
	return nil
}

func parseInvocation(args []string) (invocation, error) {
	result := invocation{}
	operands := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--help" || argument == "-h":
			result.help = true
		case argument == "--opsctl" || argument == "--acme-email":
			index++
			value, found := argumentAt(args, index)
			if !found || value == "" || strings.HasPrefix(value, "-") {
				return invocation{}, usage("option '" + argument + "' requires a value")
			}
			setOption(&result, argument, value)
		case strings.HasPrefix(argument, "--opsctl="):
			value := strings.TrimPrefix(argument, "--opsctl=")
			if value == "" {
				return invocation{}, usage("option '--opsctl' requires a value")
			}
			result.opsctl = value
		case strings.HasPrefix(argument, "--acme-email="):
			value := strings.TrimPrefix(argument, "--acme-email=")
			if value == "" {
				return invocation{}, usage("option '--acme-email' requires a value")
			}
			result.acmeEmail = value
		case strings.HasPrefix(argument, "-"):
			return invocation{}, usage("unknown option '" + argument + "'")
		default:
			operands = append(operands, argument)
		}
	}
	if result.help {
		return result, nil
	}
	if len(operands) == 0 {
		return invocation{}, usage("space init needs <space>")
	}
	if len(operands) > 1 {
		return invocation{}, usage("space init takes only <space>")
	}
	result.space = operands[0]
	return result, nil
}

func argumentAt(args []string, wanted int) (string, bool) {
	for index, argument := range args {
		if index == wanted {
			return argument, true
		}
	}
	return "", false
}

func setOption(result *invocation, option, value string) {
	if option == "--opsctl" {
		result.opsctl = value
		return
	}
	result.acmeEmail = value
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: "devctl space --help"}
}
