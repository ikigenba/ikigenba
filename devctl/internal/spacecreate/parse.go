package spacecreate

import (
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

const (
	helpCommand = "devctl space --help"
	usageText   = `Usage: devctl --account <name> space create <domain> --acme-email <address>

Create the space with secrets, a role, an instance, an Elastic IP and DNS records.
Install the newest published opsctl, set its host configuration and run init.
When the account keeps backups, restore its own host backup before init.
Completed steps remain on failure; use space destroy to clean up.

Options:
  --acme-email <address>   ACME contact address stored on the host; required
`
)

type invocation struct {
	domain    string
	acmeEmail string
	help      bool
}

func parseInvocation(args []string) (invocation, error) {
	result := invocation{}
	operands := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--help" || argument == "-h":
			result.help = true
		case argument == "--acme-email":
			if index+1 == len(args) || args[index+1] == "" {
				return invocation{}, usage("option '--acme-email' requires a value")
			}
			index++
			result.acmeEmail = args[index]
		case strings.HasPrefix(argument, "--acme-email="):
			result.acmeEmail = strings.TrimPrefix(argument, "--acme-email=")
			if result.acmeEmail == "" {
				return invocation{}, usage("option '--acme-email' requires a value")
			}
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
		return invocation{}, usage("space create needs <domain>")
	}
	if len(operands) > 1 {
		return invocation{}, usage("space create takes only <domain>")
	}
	if result.acmeEmail == "" {
		return invocation{}, usage("space create needs --acme-email <address>")
	}
	result.domain = operands[0]
	return result, nil
}

func usage(message string) *space.UsageError {
	return &space.UsageError{Message: message, Help: helpCommand}
}
