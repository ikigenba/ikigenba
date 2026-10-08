package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	helpCommand = "devctl deploy --help"
	helpText    = `Usage: devctl deploy <space> <sha|tag>

Build the suite at <sha|tag> as build does, check that the space holds every
secret the release's manifests declare, copy dist/<sha>.tar.xz to the space's
host, unpack it into /opt/ikigenba/releases/<sha>/, and have that release's
opsctl activate it. A tag is the release's label, exactly as typed; a sha
gives none.
`
)

type invocation struct {
	space   string
	operand string
}

// Run builds and deploys a suite release.
func Run(ctx context.Context, args []string, version string, stdout io.Writer, deps seam.Deps) error {
	invocation, help, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if help {
		_, _ = fmt.Fprint(stdout, helpText)
		return nil
	}
	return runRelease(ctx, invocation, version, stdout, deps)
}

func parseInvocation(args []string) (invocation, bool, error) {
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			return invocation{}, true, nil
		}
	}
	for _, argument := range args {
		if strings.HasPrefix(argument, "-") {
			return invocation{}, false, usage("unknown option '" + argument + "'")
		}
	}
	if len(args) < 2 {
		return invocation{}, false, usage("deploy needs <space> and <sha|tag>")
	}
	if len(args) > 2 {
		return invocation{}, false, usage("deploy takes only <space> and <sha|tag>")
	}
	return invocation{space: args[0], operand: args[1]}, false, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: helpCommand}
}
