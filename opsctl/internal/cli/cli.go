// Package cli is the opsctl command-line interface.
package cli

import (
	"flag"
	"io"
	"strings"
)

type exitCode int

const (
	exitOK      exitCode = 0
	exitFail    exitCode = 1
	exitUsage   exitCode = 2
	exitRefused exitCode = 3
)

// usageText is the top-level usage, byte for byte from D2.
const usageText = `Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  config    read and write the host configuration store
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: opsctl must run as root

Run 'opsctl <command> --help' for details on a command.
`

// version is the opsctl version (vMAJOR.MINOR.PATCH), set in source.
var version = "v0.1.0"

// Deps carries what a command cannot be deterministic about.
type Deps struct {
	Root string // filesystem root every host path is resolved under ("/" in production)
	EUID int    // effective user id of the process
}

// Run executes the CLI. args are the program arguments without the program
// name; all I/O flows through the injected streams and every environmental
// dependency through deps. Run never terminates the process; it returns
// the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int {
	_ = stdin
	return int(run(args, stdout, stderr, deps))
}

func run(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	help, showVersion, rest, err := parseTopLevel(args)
	if err != nil {
		writeUsage(stderr)
		return exitUsage
	}
	if help {
		return writeOut(stdout, usageText)
	}
	if showVersion {
		return writeOut(stdout, version+"\n")
	}
	if len(rest) == 0 {
		writeUsage(stderr)
		return exitUsage
	}
	return dispatch(rest[0], rest[1:], stdout, stderr, deps)
}

func parseTopLevel(args []string) (help, showVersion bool, rest []string, err error) {
	fs := flag.NewFlagSet("opsctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	boolFlag(fs, "h", "help", &help)
	boolFlag(fs, "V", "version", &showVersion)
	if err = fs.Parse(args); err != nil {
		return false, false, nil, err
	}
	return help, showVersion, fs.Args(), nil
}

func boolFlag(fs *flag.FlagSet, short, long string, dest *bool) {
	fs.BoolVar(dest, short, false, "")
	fs.BoolVar(dest, long, false, "")
}

func writeOut(w io.Writer, s string) exitCode {
	if _, err := io.WriteString(w, s); err != nil {
		return exitFail
	}
	return exitOK
}

// writeUsage writes usageText to stderr with every line prefixed by "opsctl: ".
func writeUsage(stderr io.Writer) {
	writePrefixed(stderr, "opsctl: ", usageText)
}

func writePrefixed(w io.Writer, prefix, text string) {
	remaining := text
	for remaining != "" {
		line, rest, found := strings.Cut(remaining, "\n")
		_, _ = io.WriteString(w, prefix+line+"\n")
		if !found {
			break
		}
		remaining = rest
	}
}

// diagnosticArg keeps a user-supplied token on one diagnostic line.
func diagnosticArg(s string) string {
	if !strings.ContainsAny(s, "\n\r") {
		return s
	}
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func dispatch(name string, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	switch name {
	case "config":
		return runConfig(args, stdout, stderr, deps)
	case "version":
		return writeOut(stdout, version+"\n")
	default:
		_, _ = io.WriteString(stderr, "opsctl: unknown command: "+diagnosticArg(name)+"\n")
		writeUsage(stderr)
		return exitUsage
	}
}

func isCommandHelp(args []string) bool {
	return len(args) > 0 && (args[0] == "-h" || args[0] == "--help")
}

func requireRoot(deps Deps, stderr io.Writer) exitCode {
	if deps.EUID == 0 {
		return exitOK
	}
	_, _ = io.WriteString(stderr, "opsctl: must run as root\n")
	return exitRefused
}
