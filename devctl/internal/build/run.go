package build

import (
	"context"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	helpCommand = "devctl build --help"
	usageText   = `Usage: devctl build <app>

Build <app> for linux/amd64 and write <app>/dist/<app>-<version>.tar.xz, the
file deploy copies to a host and opsctl installs. HEAD must be a commit that
the app's version tag (<app>/v<semver>) points at, with no uncommitted
changes.
`
)

// Run builds the app named by args.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps) error {
	if containsHelp(args) {
		_, err := io.WriteString(stdout, usageText)
		return err
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usage("unknown option '" + arg + "'")
		}
	}

	switch len(args) {
	case 0:
		return usage("build needs <app>")
	case 1:
		return run(ctx, args[0], stdout, deps)
	default:
		return usage("build takes one <app>")
	}
}

func containsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: helpCommand}
}
