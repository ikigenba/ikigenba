// Package cli owns devctl's command-line grammar and application dispatch.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/deploy"
	"github.com/ikigenba/ikigenba/devctl/internal/keyring"
	"github.com/ikigenba/ikigenba/devctl/internal/remove"
	"github.com/ikigenba/ikigenba/devctl/internal/restore"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceapps"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceinit"
)

var version = "v0.1.1"

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

const spaceRestartUsage = `Usage: devctl --account <name> space restart <domain> <app>

Have opsctl restart one app's service. Deploy the existing file to apply pushed
secrets; a restart uses the environment already installed on the host.
`

const spaceLogsUsage = `Usage: devctl --account <name> space logs <domain> <app> [--since <when>] [--follow]

Print the last 100 journal lines for an installed app. With --since, print all
lines from that moment using journalctl's time syntax.

Options:
  --since <when>   read from this moment; passed unchanged to journalctl
  --follow         stream new lines until interrupted
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
		writeDiagnostic(stderr, "must not run as root", "", "", false)
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
	if hasHelp(invocation.arguments) {
		invocation.arguments = helpArguments(invocation.command, invocation.arguments)
	} else if message, helpCommand := missingCommandOptionValue(invocation.command, invocation.arguments); message != "" {
		return usageError(stderr, message, helpCommand)
	}
	if invocation.command == "build" {
		return operationError(stderr, build.Run(ctx, invocation.arguments, stdout, deps))
	}
	if invocation.command == "deploy" {
		if !invocation.accountSet && !hasHelp(invocation.arguments) {
			return usageError(stderr, "--account is required", "devctl deploy --help")
		}
		return operationError(stderr, deploy.Run(ctx, invocation.arguments, stdout, deps, invocation.account))
	}
	if invocation.command == "restore" {
		if !invocation.accountSet && !hasHelp(invocation.arguments) {
			return usageError(stderr, "--account is required", "devctl restore --help")
		}
		return operationError(stderr, restore.Run(ctx, invocation.arguments, stdout, deps, invocation.account))
	}
	if invocation.command == "remove" {
		if !invocation.accountSet && !hasHelp(invocation.arguments) {
			return usageError(stderr, "--account is required", "devctl remove --help")
		}
		return operationError(stderr, remove.Run(ctx, invocation.arguments, stdout, deps, invocation.account))
	}
	if invocation.command == "secrets" {
		if !invocation.accountSet && !hasHelp(invocation.arguments) {
			return usageError(stderr, "--account is required", "devctl secrets --help")
		}
		return operationError(stderr, secrets.Run(ctx, invocation.arguments, stdout, deps, invocation.account))
	}
	if invocation.command == "space" {
		if !invocation.accountSet && !hasHelp(invocation.arguments) {
			return usageError(stderr, "--account is required", "devctl space --help")
		}
		return operationError(stderr, runSpace(ctx, invocation.arguments, stdout, deps, invocation.account))
	}
	if _, required := accountRequired[invocation.command]; required {
		if hasHelp(invocation.arguments) {
			// Command-specific phases replace this with the command's help.
			return 0
		}
		if !invocation.accountSet {
			return usageError(stderr, "--account is required", "devctl "+invocation.command+" --help")
		}
		acct, err := account.Open(ctx, deps, invocation.account)
		if err != nil {
			return operationError(stderr, err)
		}
		if domain, ok := spaceDomain(invocation.command, invocation.arguments); ok {
			if _, err := acct.Space(ctx, domain); err != nil {
				return operationError(stderr, err)
			}
		}
	}

	// Command-specific phases replace this successful no-op with their dispatch.
	return 0
}

func helpArguments(command string, arguments []string) []string {
	if len(arguments) != 0 {
		switch command {
		case "space":
			if _, ok := map[string]struct{}{
				"list": {}, "create": {}, "destroy": {}, "stop": {}, "start": {},
				"init": {}, "status": {}, "restart": {}, "logs": {},
			}[arguments[0]]; ok {
				return []string{arguments[0], "--help"}
			}
		case "secrets":
			if arguments[0] == "push" || arguments[0] == "list" {
				return []string{arguments[0], "--help"}
			}
		}
	}
	return []string{"--help"}
}

func missingCommandOptionValue(command string, arguments []string) (string, string) {
	options := map[string]struct{}{}
	helpCommand := ""
	switch {
	case command == "space" && len(arguments) != 0 && arguments[0] == "create":
		options["--acme-email"] = struct{}{}
		helpCommand = "devctl space --help"
	case command == "space" && len(arguments) != 0 && arguments[0] == "init":
		options["--opsctl"] = struct{}{}
		options["--acme-email"] = struct{}{}
		helpCommand = "devctl space --help"
	case command == "restore":
		options["--at"] = struct{}{}
		helpCommand = "devctl restore --help"
	default:
		return "", ""
	}

	for index, argument := range arguments {
		if _, ok := options[argument]; ok {
			if index+1 == len(arguments) || arguments[index+1] == "" || strings.HasPrefix(arguments[index+1], "-") {
				return "option '" + argument + "' requires a value", helpCommand
			}
		}
		if equal := strings.IndexByte(argument, '='); equal >= 0 {
			if _, ok := options[argument[:equal]]; ok && argument[equal+1:] == "" {
				return "option '" + argument[:equal] + "' requires a value", helpCommand
			}
		}
	}
	return "", ""
}

func runSpace(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	if len(args) != 0 {
		switch args[0] {
		case "create":
			return spacecreate.Run(ctx, args[1:], stdout, deps, profile)
		case "init":
			return spaceinit.Run(ctx, args[1:], stdout, deps, profile)
		case "restart":
			if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
				_, _ = fmt.Fprint(stdout, spaceRestartUsage)
				return nil
			}
			return spaceapps.Run(ctx, args, stdout, deps, profile)
		case "logs":
			if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
				_, _ = fmt.Fprint(stdout, spaceLogsUsage)
				return nil
			}
			return spaceapps.Run(ctx, args, stdout, deps, profile)
		}
	}
	return space.Run(ctx, args, stdout, deps, profile)
}

func spaceDomain(command string, arguments []string) (string, bool) {
	switch command {
	case "space":
		if len(arguments) >= 2 && (arguments[0] == "stop" || arguments[0] == "start" || arguments[0] == "status") {
			return arguments[1], true
		}
	case "secrets":
		if len(arguments) >= 2 && arguments[0] == "push" {
			return arguments[1], true
		}
	case "deploy", "restore":
		if len(arguments) >= 1 {
			return arguments[0], true
		}
	}
	return "", false
}

func operationError(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		detail := ""
		var detailed interface{ Detail() string }
		if errors.As(err, &detailed) {
			detail = detailed.Detail()
		}
		writeDiagnostic(stderr, err.Error(), "", detail, false)
		return coded.ExitCode()
	}
	var cloudError *cloud.Error
	if errors.As(err, &cloudError) {
		writeDiagnostic(stderr, cloudError.Error(), "", "", false)
		return 1
	}
	var noSpaceError *account.NoSpaceError
	if errors.As(err, &noSpaceError) {
		writeDiagnostic(stderr, noSpaceError.Error(), "", "", false)
		return 1
	}
	var noZoneError *account.NoZoneError
	if errors.As(err, &noZoneError) {
		writeDiagnostic(stderr, noZoneError.Error(), "", "", false)
		return 1
	}
	var notInCheckoutError *checkout.NotInCheckoutError
	if errors.As(err, &notInCheckoutError) {
		writeDiagnostic(stderr, notInCheckoutError.Error(), "", "", false)
		return 2
	}
	var noAppError *checkout.NoAppError
	if errors.As(err, &noAppError) {
		writeDiagnostic(stderr, noAppError.Error(), "", "", false)
		return 2
	}
	var manifestError *checkout.ManifestError
	if errors.As(err, &manifestError) {
		writeDiagnostic(stderr, manifestError.Error(), "", "", false)
		return 2
	}
	var gitError *checkout.GitError
	if errors.As(err, &gitError) {
		writeDiagnostic(stderr, gitError.Error(), gitError.Stderr, "", false)
		return 1
	}
	var noValueError *keyring.NoValueError
	if errors.As(err, &noValueError) {
		writeDiagnostic(stderr, err.Error(), "", "", false)
		return 2
	}
	writeDiagnostic(stderr, err.Error(), "", "", false)
	return 1
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
	writeDiagnostic(stderr, message, "", "see '"+helpCommand+"' for usage", false)
	return 2
}

func writeDiagnostic(stderr io.Writer, message, detail, advice string, detailReported bool) {
	_, _ = fmt.Fprintf(stderr, "devctl: %s\n", message)
	if detailReported {
		detail = ""
	}
	quotedDetail := seam.QuoteOutput(detail)
	if quotedDetail == "" && advice == "" {
		return
	}
	_, _ = fmt.Fprintln(stderr)
	if quotedDetail != "" {
		_, _ = fmt.Fprintln(stderr, quotedDetail)
	}
	if advice != "" {
		_, _ = fmt.Fprintln(stderr, advice)
	}
}
