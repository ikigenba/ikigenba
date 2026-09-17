// Package cli owns devctl's command-line grammar and application dispatch.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

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

// Run executes one devctl invocation and returns its process exit code.
func Run(_ context.Context, args []string, _ io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	deps = deps.Defaults()
	if deps.EUID == 0 {
		_, _ = fmt.Fprintln(stderr, "devctl: must not run as root")
		return 3
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}

	_, _ = fmt.Fprintln(stderr, "devctl: a command is required")
	return 2
}
