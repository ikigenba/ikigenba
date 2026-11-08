package spacemove

import (
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const usageText = `Usage: devctl space move <space> [--release <sha|tag>]

Move the space to a new instance launched from the current launch template:
build the release, have the current host retire with opsctl retire and take
a host backup with the release's own opsctl, stop it and tag it and its
Elastic IP Retired, launch the new instance with a new Elastic IP, copy and
unpack the release there, restore the host backup, point the space's records
at the new address, restore each app's data, and activate the release. The
retired instance stays, stopped, as the way back until space destroy removes
it. Completed steps remain on failure.

Options:
  --release <sha|tag>     the release to deploy; the newest release-<N> tag otherwise
`

type invocation struct {
	operand, release string
	help             bool
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
		return invocation{}, usage("space move needs <space>")
	}
	if len(operands) > 1 {
		return invocation{}, usage("space move takes only <space>")
	}
	inv.operand = operands[0]
	return inv, nil
}
