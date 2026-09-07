// Package cli is the opsctl command-line interface.
package cli

import (
	"io"
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
	if isHelp(args) {
		return int(writeHelp(stdout))
	}
	if len(args) == 0 {
		return int(exitUsage)
	}
	return int(dispatch(args[0], args[1:], stdout, stderr, deps))
}

func isHelp(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h")
}

func writeHelp(stdout io.Writer) exitCode {
	if _, err := io.WriteString(stdout, usageText); err != nil {
		return exitFail
	}
	return exitOK
}

func dispatch(name string, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	switch name {
	case "config":
		return runConfig(args, stdout, stderr, deps)
	default:
		return exitUsage
	}
}
