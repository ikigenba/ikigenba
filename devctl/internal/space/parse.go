package space

import (
	"context"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const listUsage = `Usage: devctl space list

Print one line per space: the domain, the instance state, the public address
or - when it has none, and apex on the one space that holds the root domain or
- on every other. The lines are sorted by domain, and no spaces prints nothing.

The cloud's tags are the only registry: a space is an instance tagged with the
root domain and a Space tag naming its own. The holder of the root domain is
the space whose Elastic IP the root's A record points at; no host is asked.
What a space is running is 'devctl space status'.
`

const usageText = `Usage: devctl space <subcommand> [arguments]

List, create, destroy, stop, start, initialise, and inspect spaces, and
restart or read the journal of one app on one. A space is one label under the
root domain; <space> is that label or the full domain. The cloud's tags are
the only registry.

Subcommands:
  list                       one line per space
  create <space> [options]   create the space
  destroy <space> [options]  remove the space and everything it owned
  stop <space>               stop the instance; state is kept
  start <space>              start the instance; its address is unchanged
  init <space> [options]     set the host's keys again and run opsctl init
  status <space>             one line per app: version, service state, database journal mode
  restart <space> <app>      restart one app's service on the host
  logs <space> <app>         print one app's journal from the host

Options (create):
  --acme-email <address>  where the CA sends the space's expiry warnings; required

Options (destroy):
  --no-backup             skip the final backup the host takes before it goes
  --delete-secrets        delete the space's secrets; they are kept otherwise
  --delete-backups        delete the space's backups; they are kept otherwise

Options (init):
  --opsctl <version>      move the host to this opsctl release first
  --acme-email <address>  change where the CA sends the space's expiry warnings

Options (logs):
  --follow                keep printing as the app writes, until interrupted
  --since <when>          start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00

Run 'devctl space <subcommand> --help' for details.
`

const destroyUsage = `Usage: devctl space destroy <space> [--no-backup] [--delete-secrets] [--delete-backups]

Delete the root domain's record first when this space holds it, have the host
take its final backup with opsctl retire, then remove the instance, Elastic IP,
records and role. Secrets and backups are kept unless an option says otherwise.
Run again to finish a partial destroy.

Options:
  --no-backup        skip the final backup the host takes before it goes
  --delete-secrets   delete the space's secrets; they are kept otherwise
  --delete-backups   delete the space's backups; they are kept otherwise
`

const stopUsage = `Usage: devctl space stop <space>

Stop the instance and keep its disk, Elastic IP, records, secrets and backups.
If the space holds the root domain, the root keeps pointing at it.
`

const startUsage = `Usage: devctl space start <space>

Start the instance at its existing Elastic IP, wait for status checks and SSH,
then run certbot renew. Records are unchanged; the last line is domain and address.
`

const statusUsage = `Usage: devctl space status <space>

Relay opsctl status from the running host: app, version, service state and
database journal mode. A host with no apps prints nothing.
`

const spaceHelp = "devctl space --help"

type invocation struct {
	subcommand    string
	domain        string
	noBackup      bool
	deleteSecrets bool
	deleteBackups bool
	help          string
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
		return invocation{help: usageText}, nil
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
		if subcommand == "destroy" && argument == "--delete-secrets" {
			result.deleteSecrets = true
			continue
		}
		if subcommand == "destroy" && argument == "--delete-backups" {
			result.deleteBackups = true
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
		return invocation{}, usage("space " + subcommand + " takes only <space>")
	}
	if help {
		switch subcommand {
		case "destroy":
			result.help = destroyUsage
		case "stop":
			result.help = stopUsage
		case "start":
			result.help = startUsage
		case "status":
			result.help = statusUsage
		}
		return result, nil
	}
	if len(operands) == 0 {
		return invocation{}, usage("space " + subcommand + " needs <space>")
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
func dispatch(context.Context, invocation, io.Writer, seam.Deps) error {
	return nil
}
