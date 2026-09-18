package spaceinit

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const usageText = `Usage: devctl --account <name> space init <domain> [--opsctl <version>] [--acme-email <address>]

Set the host's nine derived keys again and run opsctl init. Keep its installed
opsctl and email unless an option names a replacement. Cloud resources and
secrets are unchanged.

Options:
  --opsctl <version>      install this release using the host's saved installer
  --acme-email <address>  replace the CA contact address
`

type invocation struct {
	domain    string
	opsctl    string
	acmeEmail *string
	help      bool
}

// Run executes a space init command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	invocation, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if invocation.help {
		_, err := io.WriteString(stdout, usageText)
		return err
	}

	acct, err := account.Open(ctx, deps, profile)
	if err != nil {
		return err
	}
	instance, err := acct.Space(ctx, invocation.domain)
	if err != nil {
		return err
	}
	if instance.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: invocation.domain, State: instance.State}
	}
	zone, err := acct.Zone(ctx, invocation.domain)
	if err != nil {
		return err
	}

	space.Step(stdout, "account", fmt.Sprintf("%s, %s", acct.Properties.Domain, acct.Properties.Region))
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
	configured, err := hostsetup.Configure(ctx, target, acct.Properties, zone, invocation.domain, invocation.acmeEmail)
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
			if !found || value == "" {
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
			result.acmeEmail = &value
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
		return invocation{}, usage("space init needs <domain>")
	}
	if len(operands) > 1 {
		return invocation{}, usage("space init takes only <domain>")
	}
	result.domain = operands[0]
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
	result.acmeEmail = &value
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: "devctl space --help"}
}
