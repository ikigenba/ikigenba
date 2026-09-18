package secrets

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	helpCommand = "devctl secrets --help"
	usageText   = `Usage: devctl --account <name> secrets <subcommand> <domain> [<app>]

Push the values an app's manifest names from this machine's keyring to the
space's Parameter Store entry, or list which names a space holds. Values are
never printed.

Subcommands:
  push <domain> [<app>]   write /ikigenba/<domain>/<app> for one app, or every app
  list <domain> [<app>]   print the key names held for one app, or every app

Every subcommand needs --account. Run 'devctl secrets <subcommand> --help' for details.
`
	pushUsage = `Usage: devctl --account <name> secrets push <domain> [<app>]

Write the space's Parameter Store entry for an app from this machine's keyring:
a SecureString at /ikigenba/<domain>/<app> holding a JSON object whose keys are
the names the app's manifest declares. Each value comes from the environment
variable of that name, or from the login keyring. Values are never printed.

With <app> omitted, every app in the checkout is written, in name order. Every
value is gathered before anything is written, so one missing value leaves every
app's entry as it was.

Arguments:
  <domain>   the space to write the entry in
  <app>      one app of the checkout; omitted, every app
`
	listUsage = `Usage: devctl --account <name> secrets list <domain> [<app>]

Print the key names the space's Parameter Store entry holds for an app: the
app, then its names sorted and comma-separated, or - when the entry is empty.
Only names are printed; a value never is.

With <app> omitted, every entry under /ikigenba/<domain>/ is printed, in app
order, and a space that holds none prints nothing.

Arguments:
  <domain>   the space to read the entries of
  <app>      one app the space holds an entry for; omitted, every app
`
)

type invocation struct {
	subcommand string
	domain     string
	app        string
	help       string
}

// Run executes a secrets command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	invocation, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if invocation.help != "" {
		_, _ = fmt.Fprint(stdout, invocation.help)
		return nil
	}
	return dispatch(ctx, invocation, stdout, deps, profile)
}

func parseInvocation(args []string) (invocation, error) {
	if len(args) == 0 {
		return invocation{}, usage("secrets needs <subcommand>")
	}
	for _, argument := range args {
		if strings.HasPrefix(argument, "-") && argument != "--help" && argument != "-h" {
			return invocation{}, unknownOption(argument)
		}
	}
	if args[0] == "--help" || args[0] == "-h" {
		return invocation{help: usageText}, nil
	}

	subcommand := args[0]
	if subcommand != "push" && subcommand != "list" {
		return invocation{}, usage("unknown subcommand '" + subcommand + "'")
	}
	result := invocation{subcommand: subcommand}
	arguments := args[1:]
	for _, argument := range arguments {
		if argument == "--help" || argument == "-h" {
			if subcommand == "push" {
				result.help = pushUsage
			} else {
				result.help = listUsage
			}
			return result, nil
		}
	}

	operands := append([]string(nil), arguments...)
	if len(operands) == 0 {
		return invocation{}, usage("secrets " + subcommand + " needs <domain>")
	}
	if len(operands) > 2 {
		return invocation{}, usage("secrets " + subcommand + " takes at most <domain> and <app>")
	}
	result.domain = operands[0]
	if len(operands) == 2 {
		result.app = operands[1]
	}
	return result, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: helpCommand}
}

func unknownOption(option string) *UsageError {
	return usage("unknown option '" + option + "'")
}

func dispatch(ctx context.Context, invocation invocation, stdout io.Writer, deps seam.Deps, profile string) error {
	if invocation.subcommand == "push" {
		checkoutRoot, err := checkout.Open(ctx, deps)
		if err != nil {
			return err
		}
		var apps []checkout.App
		if invocation.app == "" {
			apps, err = checkoutRoot.Apps()
		} else {
			var app checkout.App
			app, err = checkoutRoot.App(invocation.app)
			apps = []checkout.App{app}
		}
		if err != nil {
			return err
		}

		acct, err := account.Open(ctx, deps, profile)
		if err != nil {
			return err
		}
		if _, err := acct.Space(ctx, invocation.domain); err != nil {
			return err
		}
		entries, err := Push(ctx, deps, acct, invocation.domain, apps)
		writeEntries(stdout, entries, true)
		return err
	}

	acct, err := account.Open(ctx, deps, profile)
	if err != nil {
		return err
	}
	if invocation.app != "" {
		keys, err := Names(ctx, acct, invocation.domain, invocation.app)
		if err != nil {
			return err
		}
		writeListEntry(stdout, Entry{App: invocation.app, Keys: keys})
		return nil
	}
	entries, err := List(ctx, acct, invocation.domain)
	if err != nil {
		return err
	}
	writeEntries(stdout, entries, false)
	return nil
}

func writeEntries(stdout io.Writer, entries []Entry, pushed bool) {
	for _, entry := range entries {
		if pushed {
			_, _ = fmt.Fprintf(stdout, "%s: ok (%d keys)\n", entry.App, len(entry.Keys))
		} else {
			writeListEntry(stdout, entry)
		}
	}
}

func writeListEntry(stdout io.Writer, entry Entry) {
	keys := strings.Join(entry.Keys, ",")
	if keys == "" {
		keys = "-"
	}
	_, _ = fmt.Fprintf(stdout, "%s %s\n", entry.App, keys)
}
