package spacecreate

import (
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const (
	helpCommand = "devctl space --help"
	usageText   = `Usage: devctl space create <space> --acme-email <address> [--release <sha|tag>]

Create the space and deploy a release to it: build the release, push its
apps' secrets, make the role, launch the instance with an Elastic IP, write
its records, copy and unpack the release on the host, have the release's
opsctl set the ten host keys, restore the space's own backups when the bucket
holds a host backup, run init, and activate the release. Completed steps
remain on failure; use space destroy to clean up.

Options:
  --acme-email <address>  where the CA sends the space's expiry warnings; required
  --release <sha|tag>     the release to deploy; the newest r<N> tag otherwise
`
)

type invocation struct {
	operand   string
	release   string
	acmeEmail string
	help      bool
}

func parseInvocation(args []string) (invocation, error) {
	result := invocation{}
	operands := make([]string, 0, len(args))
	missingACMEEmailValue := false
	missingReleaseValue := false

	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--help" || argument == "-h":
			result.help = true
		case argument == "--acme-email":
			value, ok := following(args[index:])
			if !ok || value == "" {
				missingACMEEmailValue = true
				continue
			}
			index++
			result.acmeEmail = value
			missingACMEEmailValue = false
		case strings.HasPrefix(argument, "--acme-email="):
			result.acmeEmail = strings.TrimPrefix(argument, "--acme-email=")
			missingACMEEmailValue = result.acmeEmail == ""
		case argument == "--release":
			value, ok := following(args[index:])
			if !ok || value == "" {
				missingReleaseValue = true
				continue
			}
			index++
			result.release = value
			missingReleaseValue = false
		case strings.HasPrefix(argument, "--release="):
			result.release = strings.TrimPrefix(argument, "--release=")
			missingReleaseValue = result.release == ""
		case strings.HasPrefix(argument, "-"):
			return invocation{}, usage("unknown option '" + argument + "'")
		default:
			operands = append(operands, argument)
		}
	}

	if result.help {
		return result, nil
	}
	if len(operands) == 0 {
		return invocation{}, usage("space create needs <space>")
	}
	if len(operands) > 1 {
		return invocation{}, usage("space create takes only <space>")
	}
	if missingReleaseValue {
		return invocation{}, usage("option '--release' requires a value")
	}
	if missingACMEEmailValue {
		return invocation{}, usage("option '--acme-email' requires a value")
	}
	if result.acmeEmail == "" {
		return invocation{}, usage("space create needs --acme-email <address>")
	}
	result.operand = operands[0]
	return result, nil
}

func following(arguments []string) (string, bool) {
	if len(arguments) < 2 {
		return "", false
	}
	return arguments[1], true
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: helpCommand}
}
