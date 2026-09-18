package spaceapps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const helpCommand = "devctl space --help"

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
	domain     string
	app        string
	follow     bool
	since      string
	help       bool
}

// Run executes a space restart or logs command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	parsed, err := parse(args)
	if err != nil {
		return err
	}
	if parsed.help {
		return nil
	}
	if parsed.subcommand == "logs" && !appref.ValidName(parsed.app) {
		return usage(fmt.Sprintf("'%s' is not a usable app name", parsed.app))
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

	remote := host.Host{Address: target.Address, Deps: deps}
	if parsed.subcommand == "restart" {
		if _, err := remote.Sudo(ctx, "restart", "opsctl", "restart", parsed.app); err != nil {
			return err
		}
		space.Step(stdout, "restart", "opsctl restarted "+parsed.app)
		return nil
	}

	unit := "ikigenba-" + parsed.app + ".service"
	state, err := remote.Sudo(ctx, "logs", "systemctl", "show", "--property=LoadState", "--value", unit)
	if err != nil {
		return err
	}
	if strings.TrimSpace(state.Stdout) == "not-found" {
		return &NoAppError{App: parsed.app, Domain: parsed.domain}
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
	if args[0] != "restart" && args[0] != "logs" {
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
		return invocation{}, usage("space " + result.subcommand + " needs <domain> and <app>")
	}
	if len(operands) > 2 {
		return invocation{}, usage("space " + result.subcommand + " takes only <domain> and <app>")
	}
	result.domain, result.app = operands[0], operands[1]
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
