// Package cli owns devctl's command-line grammar and application dispatch.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

var version = "v0.1.0"

const usage = `Usage: devctl [options] <command> [arguments]

Manage the ikigenba platform from the developer's machine. Never run as root.

Commands:
  version   print the version
  space     list, create, destroy, stop, start, initialise, and inspect spaces
  secrets   push and list an app's secrets for a space
  build     build one app into its deployable file
  deploy    put a built app file on a space
  remove    take an app off a space
  restore   put a space's app back from its backups

Options:
  --help              print this help
  --version           print the version
  --account <name>    AWS shared-config profile to act in

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
`

const versionUsage = `Usage: devctl version

Print the version.
`

var commandSet = map[string]struct{}{
	"version": {},
	"space":   {},
	"secrets": {},
	"build":   {},
	"deploy":  {},
	"restore": {},
	"remove":  {},
}

var accountRequired = map[string]struct{}{
	"space":   {},
	"secrets": {},
	"deploy":  {},
	"restore": {},
	"remove":  {},
}

type topLevel struct {
	account     string
	accountSet  bool
	command     string
	arguments   []string
	showHelp    bool
	showVersion bool
	err         string
}

// Run executes one devctl invocation and returns its process exit code.
func Run(ctx context.Context, args []string, _ io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	deps = deps.Defaults()
	if deps.EUID == 0 {
		_, _ = fmt.Fprintln(stderr, "devctl: must not run as root")
		return 3
	}

	invocation := parseTopLevel(args)
	if invocation.err != "" {
		return usageError(stderr, invocation.err, "devctl --help")
	}
	if invocation.showHelp {
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}
	if invocation.showVersion {
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	}
	if invocation.command == "" {
		return usageError(stderr, "no command given", "devctl --help")
	}
	if _, ok := commandSet[invocation.command]; !ok {
		return usageError(stderr, "unknown command '"+invocation.command+"'", "devctl --help")
	}
	if invocation.command == "version" {
		return runVersion(invocation.arguments, stdout, stderr)
	}
	if _, required := accountRequired[invocation.command]; required {
		if hasHelp(invocation.arguments) {
			// Command-specific phases replace this with the command's help.
			return 0
		}
		if !invocation.accountSet {
			return usageError(stderr, "--account is required", "devctl "+invocation.command+" --help")
		}
		_, _ = deps.Cloud(ctx, invocation.account, "")
	}

	// Command-specific phases replace this successful no-op with their dispatch.
	return 0
}

func hasHelp(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--help" || argument == "-h" {
			return true
		}
	}
	return false
}

func parseTopLevel(args []string) topLevel {
	var result topLevel
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "-h" || argument == "--help":
			result.showHelp = true
		case argument == "-V" || argument == "--version":
			result.showVersion = true
		case argument == "--account":
			if index+1 == len(args) || strings.HasPrefix(args[index+1], "-") || isCommand(args[index+1]) {
				result.err = "option '--account' requires a value"
				return result
			}
			index++
			result.account = args[index]
			result.accountSet = true
		case strings.HasPrefix(argument, "--account="):
			result.account = strings.TrimPrefix(argument, "--account=")
			if result.account == "" {
				result.err = "option '--account' requires a value"
				return result
			}
			result.accountSet = true
		case strings.HasPrefix(argument, "-"):
			result.err = "unknown option '" + argument + "'"
			return result
		default:
			result.command = argument
			result.arguments = args[index+1:]
			return result
		}
	}
	return result
}

func isCommand(argument string) bool {
	_, ok := commandSet[argument]
	return ok
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			_, _ = fmt.Fprint(stdout, versionUsage)
			return 0
		}
	}
	if len(args) != 0 {
		return usageError(stderr, "version takes no arguments", "devctl version --help")
	}
	_, _ = fmt.Fprintln(stdout, version)
	return 0
}

func usageError(stderr io.Writer, message, helpCommand string) int {
	_, _ = fmt.Fprintf(stderr, "devctl: %s\n\nsee '%s' for usage\n", message, helpCommand)
	return 2
}
