package build

import (
	"context"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	helpCommand = "devctl build --help"
	usageText   = `Usage: devctl build <sha|tag>

Build the suite at <sha|tag> for linux/amd64 and write dist/<sha>.tar.xz, one
release holding every app and opsctl. <sha> is the full commit sha the argument
resolves to; the working tree is not read.
`
)

// Run builds the suite at the commit named by args.
func Run(ctx context.Context, args []string, version string, stdout io.Writer, deps seam.Deps) error {
	if containsHelp(args) {
		_, _ = io.WriteString(stdout, usageText)
		return nil
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return usage("unknown option '" + arg + "'")
		}
	}

	switch len(args) {
	case 0:
		return usage("build needs <sha|tag>")
	case 1:
		opened, err := checkout.Open(ctx, deps)
		if err != nil {
			return err
		}
		return runSuite(ctx, opened, args[0], version, stdout)
	default:
		return usage("build takes one <sha|tag>")
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
