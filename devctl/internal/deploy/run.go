package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
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
	invocation, err = validateFile(invocation, deps.Dir)
	if err != nil {
		return err
	}
	if err := inspectArchive(ctx, invocation, deps.Defaults()); err != nil {
		return err
	}
	space.Step(stdout, "file", invocation.app+" "+invocation.version)

	return nil
}

// ObjectKey returns the deploy object key for an artifact basename.
func ObjectKey(domain, filename string) string {
	return domain + "/deploy/" + filename
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

func inspectArchive(ctx context.Context, value invocation, deps seam.Deps) error {
	list := seam.Cmd{
		Path: "tar",
		Args: []string{"-t", "-J", "-f", value.file},
		Dir:  deps.Dir,
	}
	result, err := runArchiveCommand(ctx, deps, list)
	if err != nil {
		return err
	}
	members := make(map[string]struct{})
	for line := range strings.SplitSeq(string(result.Stdout), "\n") {
		if line != "" {
			members[line] = struct{}{}
		}
	}
	if _, ok := members[checkout.ManifestFile]; !ok {
		return &FileError{Path: value.file, Reason: "no " + checkout.ManifestFile + " in the archive"}
	}

	extract := seam.Cmd{
		Path: "tar",
		Args: []string{"-x", "-J", "-O", "-f", value.file, checkout.ManifestFile},
		Dir:  deps.Dir,
	}
	result, err = runArchiveCommand(ctx, deps, extract)
	if err != nil {
		return err
	}
	manifest, err := checkout.DecodeManifest(bytes.NewReader(result.Stdout))
	if err != nil {
		return &FileError{Path: value.file, Reason: checkout.ManifestFile + ": " + err.Error()}
	}
	if manifest.App != value.app {
		return &FileError{Path: value.file, Reason: "manifest app does not match file name"}
	}
	if _, ok := members["bin/"+manifest.App]; !ok {
		return &FileError{Path: value.file, Reason: "no bin/" + manifest.App + " in the archive"}
	}
	return nil
}

func runArchiveCommand(ctx context.Context, deps seam.Deps, command seam.Cmd) (seam.Result, error) {
	result, err := deps.Exec(ctx, command)
	if err != nil {
		return seam.Result{}, fmt.Errorf("%s: %w", command.Path, err)
	}
	if result.ExitCode != 0 {
		label := strings.Join(append([]string{command.Path}, command.Args...), " ")
		return seam.Result{}, &ProcessError{Label: label, Status: result.ExitCode, Stderr: string(result.Stderr)}
	}
	return result, nil
}

func usage(message string) *UsageError {
	return &UsageError{Message: message, Help: helpCommand}
}
