package spacereplace

import (
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const usageText = `Usage: devctl space replace <space> [--release <sha|tag>] [--no-backup]

Put the space on a new instance at its address: build the release, have the
current host take its final backups with opsctl retire, disassociate the
space's Elastic IP, terminate the instance, launch a new one and associate
the same Elastic IP with it, wait until its name resolves, copy and unpack
the release on the host, restore the newest host backup, run init, restore
each app's data, and activate the release. A space with no Elastic IP is
first given one, and its records pointed at it, before opsctl retire;
otherwise the records are not touched, and the apex never is. Completed
steps remain on failure; replace again, with --no-backup when the space's
instance is a half-built host.

Options:
  --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise
  --no-backup             skip the final backup; restore from the newest backups already kept
`

type invocation struct {
	operand, release string
	help             bool
	noBackup         bool
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: "devctl space --help"}
}
func parse(args []string) (invocation, error) {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return invocation{help: true}, nil
		}
	}
	var inv invocation
	var operands []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-backup":
			inv.noBackup = true
		case arg == "--release":
			if i+1 == len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				return invocation{}, usage("option '--release' requires a value")
			}
			i++
			inv.release = args[i]
		case strings.HasPrefix(arg, "--release="):
			inv.release = strings.TrimPrefix(arg, "--release=")
			if inv.release == "" {
				return invocation{}, usage("option '--release' requires a value")
			}
		case strings.HasPrefix(arg, "-"):
			return invocation{}, usage("unknown option '" + arg + "'")
		default:
			operands = append(operands, arg)
		}
	}
	if len(operands) == 0 {
		return invocation{}, usage("space replace needs <space>")
	}
	if len(operands) > 1 {
		return invocation{}, usage("space replace takes only <space>")
	}
	inv.operand = operands[0]
	return inv, nil
}
