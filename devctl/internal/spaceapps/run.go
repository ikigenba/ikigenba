package spaceapps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

const helpCommand = "devctl space --help"

const restartHelp = `Usage: devctl space restart <space> <app>

Have opsctl restart one app's service. Deploy the existing file to apply pushed
secrets; a restart uses the environment already installed on the host.
`

const disableHelp = `Usage: devctl space disable <space> <app>

Have opsctl take one app offline: stop its socket and service and keep both
from starting until 'devctl space enable'. Its release, data and units stay
on the host, and deploy, restore, init and restart leave it disabled.
`

const enableHelp = `Usage: devctl space enable <space> <app>

Have opsctl bring a disabled app back: enable and start its socket and
service. Enabling an app that is already enabled changes nothing.
`

const logsHelp = `Usage: devctl space logs <space> <app> [--follow] [--since <when>]

Print the last 100 journal lines of one app's service on the space's host. With
--since, print every line from that moment instead; with --follow, keep printing
until interrupted. The two combine.

Options:
  --follow         keep printing as the app writes, until interrupted
  --since <when>   start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00
`

// NoAppError reports that an app has no installed service on a space.
type NoAppError struct {
	App    string
	Domain string
}

// Error returns the missing-app diagnostic.
func (e *NoAppError) Error() string {
	return fmt.Sprintf("no app '%s' on '%s'", e.App, e.Domain)
}

type invocation struct {
	subcommand string
	space      string
	app        string
	follow     bool
	since      string
	help       bool
}

// Run executes a space app operation.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	parsed, err := parse(args)
	if err != nil {
		return err
	}
	if parsed.help {
		var help string
		switch parsed.subcommand {
		case "restart":
			help = restartHelp
		case "disable":
			help = disableHelp
		case "enable":
			help = enableHelp
		case "logs":
			help = logsHelp
		}
		_, err = io.WriteString(stdout, help)
		return err
	}
	if parsed.subcommand == "logs" && !appref.ValidName(parsed.app) {
		return usage(fmt.Sprintf("'%s' is not a usable app name", parsed.app))
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

	remote := host.Host{Address: target.Address, Deps: deps}
	if parsed.subcommand != "logs" {
		output, err := remote.Sudo(ctx, parsed.subcommand, "opsctl", parsed.subcommand, parsed.app)
		if err != nil {
			return err
		}
		_, err = io.WriteString(stdout, output.Stdout)
		return err
	}

	unit := "ikigenba-" + parsed.app + ".service"
	state, err := remote.Sudo(ctx, "logs", "systemctl", "show", "--property=LoadState", "--value", unit)
	if err != nil {
		return err
	}
	if strings.TrimSpace(state.Stdout) == "not-found" {
		return &NoAppError{App: parsed.app, Domain: targetRef.Domain}
	}

	journalArgs := []string{"journalctl", "-u", unit}
	if parsed.since == "" {
		journalArgs = append(journalArgs, "-n", "100")
	} else {
		journalArgs = append(journalArgs, "--since", parsed.since)
	}
	if parsed.follow {
		journalArgs = append(journalArgs, "-f")
	}
	journalArgs = append(journalArgs, "--no-pager")
	if err := remote.StreamSudo(ctx, stdout, "logs", journalArgs...); err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return err
	}
	return nil
}

func parse(args []string) (invocation, error) {
	if len(args) == 0 {
		return invocation{}, usage("space needs <subcommand>")
	}
	if args[0] != "restart" && args[0] != "disable" && args[0] != "enable" && args[0] != "logs" {
		if strings.HasPrefix(args[0], "-") {
			return invocation{}, unknownOption(args[0])
		}
		return invocation{}, usage("unknown subcommand '" + args[0] + "'")
	}

	result := invocation{subcommand: args[0]}
	operands := make([]string, 0, 2)
	for i := 1; i < len(args); i++ {
		argument := args[i]
		if argument == "--help" || argument == "-h" {
			result.help = true
			continue
		}
		if result.subcommand == "logs" {
			switch {
			case argument == "--follow":
				result.follow = true
				continue
			case argument == "--since":
				if i+1 == len(args) || args[i+1] == "" || recognizedLogsOption(args[i+1]) {
					return invocation{}, usage("option '--since' requires a value")
				}
				i++
				result.since = args[i]
				continue
			case strings.HasPrefix(argument, "--since="):
				value := strings.TrimPrefix(argument, "--since=")
				if value == "" {
					return invocation{}, usage("option '--since' requires a value")
				}
				result.since = value
				continue
			}
		}
		if strings.HasPrefix(argument, "-") {
			return invocation{}, unknownOption(argument)
		}
		operands = append(operands, argument)
	}
	if result.help {
		return result, nil
	}
	if len(operands) < 2 {
		return invocation{}, usage("space " + result.subcommand + " needs <space> and <app>")
	}
	if len(operands) > 2 {
		return invocation{}, usage("space " + result.subcommand + " takes only <space> and <app>")
	}
	result.space, result.app = operands[0], operands[1]
	return result, nil
}

func recognizedLogsOption(argument string) bool {
	return argument == "--follow" || argument == "--since" ||
		strings.HasPrefix(argument, "--since=") || argument == "--help" || argument == "-h"
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: helpCommand}
}

func unknownOption(option string) *space.UsageError {
	return usage("unknown option '" + option + "'")
}
