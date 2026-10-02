package cli

func expectedHelpText(command string) string {
	switch command {
	case "":
		return `Usage: sandbox [options] <command> [arguments]

Run the whole suite from this worktree on this machine. Never run as root.

Commands:
  up        build every app and start the sandbox
  down      stop a sandbox and keep its data
  wipe      delete a stopped sandbox's data
  ls        list every known sandbox
  url       print the sandbox's URLs
  status    show the state of each unit
  logs      print the sandbox's journal
  token     print or store the sandbox's bearer token
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a precondition was not met
  3  refused: sandbox must not run as root

Run 'sandbox <command> --help' for details on a command.
`
	case "up":
		return `Usage: sandbox up

Build every app in this worktree and start its sandbox, or redeploy it if it
is already up. Prints one URL per app.

Options:
  -h, --help  print this help
`
	case "url":
		return `Usage: sandbox url

Print the URLs of this worktree's sandbox, as the last 'sandbox up' printed
them.

Options:
  -h, --help  print this help
`
	case "down":
		return `Usage: sandbox down [<name>]

Stop a sandbox and remove its units and generated files, keeping its data,
token and port. Without a name, the sandbox is this worktree's.

Options:
  -h, --help  print this help
`
	case "wipe":
		return `Usage: sandbox wipe [<name>]

Delete a stopped sandbox's data, token and registry entry, freeing its port.
Without a name, the sandbox is this worktree's.

Options:
  -h, --help  print this help
`
	case "ls":
		return `Usage: sandbox ls

List every known sandbox: its name, port, state and worktree, marking a
worktree that no longer exists.

Options:
  -h, --help  print this help
`
	case "status":
		return `Usage: sandbox status

Show the state of nginx and of each app's service in this worktree's
sandbox.

Options:
  -h, --help  print this help
`
	case "logs":
		return `Usage: sandbox logs [options] [<app>]

Print the journal of this worktree's sandbox: nginx and every app, or only
<app>.

Options:
  -n, --lines <count>  print the last <count> lines (default 100)
  -f, --follow         keep printing new lines until interrupted
  -h, --help           print this help
`
	case "token":
		return `Usage: sandbox token
       sandbox token set

Print the bearer token stored for this worktree's sandbox, or store one read
from stdin.

Subcommands:
  set  store the bearer token read from stdin, replacing any earlier one

Options:
  -h, --help  print this help
`
	case "version":
		return `Usage: sandbox version

Print the version of sandbox.

Options:
  -h, --help  print this help
`
	default:
		return ""
	}
}
