package spacecreate

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// Run executes a space create command.
func Run(ctx context.Context, args []string, version string, stdout io.Writer, deps seam.Deps) error {
	invocation, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if invocation.help {
		_, err := fmt.Fprint(stdout, usageText)
		return err
	}
	deps = deps.Defaults()
	result, err := preflight(ctx, deps, invocation)
	if err != nil {
		return err
	}
	dir, err := temporaryDirectory(deps)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	space.Step(stdout, "account", fmt.Sprintf("%s, %s, %s", result.root.Domain, result.root.Region, result.session.AccountID))
	space.Step(stdout, "domain", fmt.Sprintf("zone %s %s", result.zone.Name, result.zone.ID))
	built, err := build.Suite(ctx, result.checkout, result.sha, version)
	if err != nil {
		return err
	}
	detail := built.File
	if result.label != "" {
		detail = result.label + ", " + detail
	}
	space.Step(stdout, "build", detail)
	if invocation.apex != "" {
		found := false
		for _, m := range built.Manifests {
			if m.App == invocation.apex {
				found = true
				break
			}
		}
		if !found {
			name := result.label
			if name == "" {
				name = built.File
			}
			return &NotInReleaseError{App: invocation.apex, Release: name}
		}
	}
	apps := make([]checkout.App, len(built.Manifests))
	for i, manifest := range built.Manifests {
		apps[i] = checkout.App{Name: manifest.App, Manifest: manifest}
	}
	entries, err := secrets.Push(ctx, deps, result.session.Clients.SSM, result.sp.Domain, apps)
	if err != nil {
		return err
	}
	space.Step(stdout, "secrets", fmt.Sprintf("%d apps", len(entries)))
	return provision(ctx, stdout, deps, invocation, result, built, dir)
}

func temporaryDirectory(deps seam.Deps) (string, error) {
	base := deps.Getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	if strings.ContainsFunc(base, unicode.IsSpace) {
		return "", &RefusedError{Message: fmt.Sprintf("temporary directory '%s' holds whitespace", base)}
	}
	dir, err := os.MkdirTemp(base, "create.")
	if err != nil {
		return "", fmt.Errorf("create temporary directory: %w", err)
	}
	return dir, nil
}
