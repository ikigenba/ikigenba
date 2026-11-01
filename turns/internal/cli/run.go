// Package cli implements the turns command line.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	diagnostictext "github.com/ikigenba/ikigenba/turns/internal/diagnostic"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

const (
	// ExitOK reports success.
	ExitOK = 0
	// ExitFailed reports an operation failure.
	ExitFailed = 1
	// ExitUsage reports invalid arguments.
	ExitUsage = 2
	// ExitRoot refuses execution as root.
	ExitRoot = 3
	// ExitNotConfigured reports missing configuration.
	ExitNotConfigured = 4
	// Version is the released program version.
	Version = "v0.1.1"
)

const topUsage = `Usage: turns [options] <command> [arguments]

Push this account's agent session turns to an ikigenba space. Never run as root.

Commands:
  configure store the space and token turns pushes with
  install   install the user timer that pushes every five minutes
  push      push new turns to the space
  status    show what is here and what the space holds
  version   print the version
  help      print this help

Options:
  -h, --help     print this help
  --version      print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error
  3  refused: turns must not run as root
  4  refused: not configured

Run 'turns <command> --help' for details on a command.
`
const versionUsage = `Usage: turns version

Print the version turns was released as.

Options:
  -h, --help   print this help
`
const helpUsage = `Usage: turns help

Print the usage text, the same as 'turns --help'.

Options:
  -h, --help   print this help
`
const configureUsage = `Usage: turns configure [<space>] [--token <token>]

Store the space turns pushes to and the personal token it pushes with, in
$XDG_CONFIG_HOME/turns/ (~/.config/turns/ by default). The token is read as
one line from stdin unless --token gives it. Running it again replaces both,
or, with the space omitted, only the token. On a host the space may be
omitted; the host's own space is used.

Options:
  --token <token>   take the token from the argument instead of stdin
  -h, --help        print this help
`
const pushUsage = `Usage: turns push [--dry-run] [--all]

Read this home's Claude Code, Codex CLI and Grok CLI session files, ask the
space which turns it already holds, and push the turns it lacks. Session
files are never changed, and no text leaves the machine except a first
command's name and the first line of its argument.

Options:
  --dry-run    report what would be pushed and push nothing
  --all        read every session file, not just changed ones
  -h, --help   print this help
`
const installUsage = `Usage: turns install

Write turns.service and turns.timer under ~/.config/systemd/user/, reload the
user manager, and enable and start the timer, which runs 'turns push' every
five minutes. On a host whose /etc/systemd/user/ already holds the units, only
enable and start the timer. Safe to run again.

Options:
  -h, --help   print this help
`
const statusUsage = `Usage: turns status

Print the space and token turns uses, when it last pushed, and per tool how
many sessions and turns are on this machine and how many the space holds.
Pushes nothing.

Options:
  -h, --help   print this help
`

// Run executes one invocation with all external dependencies supplied by deps.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	if deps.EUID == 0 {
		return diagnosticCode(stderr, "must not run as root", "", ExitRoot)
	}
	end := 0
	for end < len(args) && strings.HasPrefix(args[end], "-") {
		end++
	}
	for _, arg := range args[:end] {
		if arg == "-h" || arg == "--help" {
			_, _ = io.WriteString(stdout, topUsage)
			return ExitOK
		}
	}
	for _, arg := range args[:end] {
		if arg != "--version" {
			return usageError(stderr, "", fmt.Sprintf("unknown option '%s'", arg))
		}
	}
	if end > 0 {
		_, _ = fmt.Fprintln(stdout, Version)
		return ExitOK
	}
	if len(args) == 0 {
		return usageError(stderr, "", "no command given")
	}
	command := args[0]
	usage, ok := map[string]string{"configure": configureUsage, "install": installUsage, "push": pushUsage, "status": statusUsage, "version": versionUsage, "help": helpUsage}[command]
	if !ok {
		return usageError(stderr, "", fmt.Sprintf("unknown command '%s'", command))
	}
	arguments := args[1:]
	for _, arg := range arguments {
		if arg == "-h" || arg == "--help" {
			_, _ = io.WriteString(stdout, usage)
			return ExitOK
		}
	}
	positional := 0
	for i := 0; i < len(arguments); i++ {
		arg := arguments[i]
		if command == "configure" && arg == "--token" {
			if i+1 < len(arguments) && !strings.HasPrefix(arguments[i+1], "-") {
				i++
			}
			continue
		}
		if command == "configure" && strings.HasPrefix(arg, "--token=") {
			continue
		}
		if command == "push" && (arg == "--dry-run" || arg == "--all") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return usageError(stderr, command, fmt.Sprintf("unknown option '%s'", arg))
		}
		if command != "configure" {
			return usageError(stderr, command, command+" takes no arguments")
		}
		positional++
		if positional > 1 {
			return usageError(stderr, command, fmt.Sprintf("unexpected argument '%s'", arg))
		}
	}
	if deps.Getenv == nil {
		deps.Getenv = func(string) string { return "" }
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Hostname == nil {
		deps.Hostname = os.Hostname
	}
	if deps.HTTP == nil {
		deps.HTTP = http.DefaultClient
	}
	if deps.Exec == nil {
		deps.Exec = seam.Exec
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	switch command {
	case "version":
		_, _ = fmt.Fprintln(stdout, Version)
		return ExitOK
	case "help":
		_, _ = io.WriteString(stdout, topUsage)
		return ExitOK
	case "configure":
		return runConfigure(ctx, arguments, stdin, stdout, stderr, deps)
	case "push":
		return runPush(ctx, arguments, stdin, stdout, stderr, deps)
	case "install":
		return runInstall(ctx, arguments, stdin, stdout, stderr, deps)
	case "status":
		return runStatus(ctx, arguments, stdin, stdout, stderr, deps)
	}
	return ExitFailed
}

func diagnosticCode(stderr io.Writer, summary, detail string, code int) int {
	return writeDiagnostic(stderr, summary, diagnostictext.Text(detail), code)
}

func writeDiagnostic(stderr io.Writer, summary, detail string, code int) int {
	_, _ = fmt.Fprintln(stderr, "turns: "+diagnostictext.Text(summary))
	if detail != "" {
		_, _ = fmt.Fprintln(stderr, "\n"+detail)
	}
	return code
}
func diagnosticOutput(stderr io.Writer, summary, quotedDetail string) int {
	return writeDiagnostic(stderr, summary, quotedDetail, ExitFailed)
}
func diagnostic(stderr io.Writer, summary, detail string) int {
	return diagnosticCode(stderr, summary, detail, ExitFailed)
}
func usageError(stderr io.Writer, command, summary string) int {
	suffix := ""
	if command != "" {
		suffix = " " + command
	}
	return diagnosticCode(stderr, summary, "see 'turns"+suffix+" --help' for usage", ExitUsage)
}
