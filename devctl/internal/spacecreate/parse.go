package spacecreate

import (
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const (
	helpCommand = "devctl space --help"
	usageText   = `Usage: devctl space create <space> --acme-email <address>

Create the space: push its secrets, make its role, launch its instance with an
Elastic IP, write its records, install the newest published opsctl, set its
ten host keys, restore its own host backup when the bucket holds one, and run
opsctl init. Completed steps remain on failure; use space destroy to clean up.

Options:
  --acme-email <address>  where the CA sends the space's expiry warnings; required
`
)

type invocation struct {
	operand   string
	acmeEmail string
	help      bool
}

func parseInvocation(args []string) (invocation, error) {
	result := invocation{}
	operands := make([]string, 0, len(args))
	missingACMEEmailValue := false

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
