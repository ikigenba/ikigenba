package cli

import (
	"context"
	"strings"
)

// Run serves when Args is empty, otherwise handles a command or usage error.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) == 0 {
		return serve(ctx, p)
	}
	var product string
	known := true
	switch p.Args[0] {
	case "--version":
		product = Version + "\n"
	case "manifest":
		product = Manifest
	case "--help":
		product = Usage
	default:
		known = false
	}
	if known && len(p.Args) == 1 {
		_, _ = p.Stdout.Write([]byte(product))
		return ExitSuccess
	}
	arg := p.Args[0]
	if known {
		arg = p.Args[1]
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = p.Stderr.Write([]byte("repos: unknown " + kind + " '" + arg + "'\n\nsee 'repos --help' for usage\n"))
	return ExitUsage
}
