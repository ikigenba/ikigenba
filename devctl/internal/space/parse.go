package space

import (
	"context"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const listUsage = `Usage: devctl --account <name> space list

Print one line per space in the account: the domain, the instance state, and the
public address, or - when it has none. The lines are sorted by domain, and an
account with no spaces prints nothing.

The cloud's tags are the only registry: a space is an instance tagged
Project=ikigenba with a Space tag naming its domain. What a space is running is
'devctl space status'.
`

const spaceHelp = "devctl space --help"

type invocation struct {
	subcommand string
	domain     string
	noBackup   bool
	help       string
}

var subcommands = map[string]struct{}{
	"list":    {},
	"create":  {},
	"destroy": {},
	"stop":    {},
	"start":   {},
	"init":    {},
	"status":  {},
	"restart": {},
	"logs":    {},
}

func parseInvocation(args []string) (invocation, error) {
	if len(args) == 0 {
		return invocation{}, usage("space needs <subcommand>")
	}
	if args[0] == "--help" || args[0] == "-h" {
		for _, argument := range args[1:] {
			if strings.HasPrefix(argument, "-") && argument != "--help" && argument != "-h" {
				return invocation{}, unknownOption(argument)
			}
		}
		// The top-level space help is supplied with its own requirement.
		return invocation{}, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return invocation{}, unknownOption(args[0])
	}

	subcommand := args[0]
	if _, ok := subcommands[subcommand]; !ok {
		return invocation{}, usage("unknown subcommand '" + subcommand + "'")
	}
	result := invocation{subcommand: subcommand}
	arguments := args[1:]

	// These subcommands own their grammars in their own packages. The CLI
	// dispatcher normally routes them there without calling this package.
	if subcommand == "create" || subcommand == "init" || subcommand == "restart" || subcommand == "logs" {
		return result, nil
	}

	operands := make([]string, 0, len(arguments))
	help := false
	for _, argument := range arguments {
		if argument == "--help" || argument == "-h" {
			help = true
			continue
		}
		if subcommand == "destroy" && argument == "--no-backup" {
			result.noBackup = true
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return invocation{}, unknownOption(argument)
		}
		operands = append(operands, argument)
	}

	if subcommand == "list" {
		if len(operands) != 0 {
			return invocation{}, usage("space list takes no arguments")
		}
		if help {
			result.help = listUsage
		}
		return result, nil
	}
	if len(operands) > 1 {
		return invocation{}, usage("space " + subcommand + " takes only <domain>")
	}
	if help {
		return result, nil
	}
	if len(operands) == 0 {
		return invocation{}, usage("space " + subcommand + " needs <domain>")
	}
	result.domain = operands[0]
	return result, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: spaceHelp}
}

func unknownOption(option string) *UsageError {
	return usage("unknown option '" + option + "'")
}

// dispatch is the stable handoff from grammar to command behavior. Command
// implementations fill these cases in later phases without changing parsing.
func dispatch(context.Context, invocation, io.Writer, seam.Deps, string) error {
	return nil
}
