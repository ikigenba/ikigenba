package spacecreate

import (
	"context"
	"fmt"
	"io"

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
	result, err := preflight(ctx, deps, invocation)
	if err != nil {
		return err
	}
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
	apps := make([]checkout.App, len(built.Manifests))
	for i, manifest := range built.Manifests {
		apps[i] = checkout.App{Name: manifest.App, Manifest: manifest}
	}
	entries, err := secrets.Push(ctx, deps, result.session.Clients.SSM, result.sp.Domain, apps)
	if err != nil {
		return err
	}
	space.Step(stdout, "secrets", fmt.Sprintf("%d apps", len(entries)))
	return provision(ctx, stdout, deps, invocation, result, built)
}
