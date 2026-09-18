package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const (
	helpCommand = "devctl deploy --help"
	helpText    = `Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
`
)

type invocation struct {
	domain  string
	file    string
	app     string
	version string
}

// Run executes a deploy command.
func Run(ctx context.Context, args []string, stdout io.Writer, deps seam.Deps, profile string) error {
	_ = ctx
	_ = profile

	invocation, help, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if help {
		_, _ = fmt.Fprint(stdout, helpText)
		return nil
	}
	_, err = validateFile(invocation, deps.Dir)
	if err != nil {
		return err
	}

	return nil
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
		return invocation{}, false, usage("deploy needs <domain> and <file>")
	}
	if len(args) > 2 {
		return invocation{}, false, usage("deploy takes only <domain> and <file>")
	}
	return invocation{domain: args[0], file: args[1]}, false, nil
}

func validateFile(value invocation, dir string) (invocation, error) {
	path := value.file
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return invocation{}, &NoFileError{Path: value.file}
	}

	app, version, err := appref.ParseFile(filepath.Base(value.file))
	if err != nil {
		return invocation{}, &FileError{
			Path:   value.file,
			Reason: "name is not <app>-v<semver>.tar.xz",
		}
	}
	value.app = app
	value.version = version
	return value, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: helpCommand}
}
