package deploy

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

func runRelease(ctx context.Context, invocation invocation, version string, stdout io.Writer, deps seam.Deps) error {
	c, err := checkout.Open(ctx, deps)
	if err != nil {
		return err
	}
	sha, label, err := release.Resolve(ctx, c, invocation.file)
	if err != nil {
		return err
	}
	root, err := c.ReadRootFile()
	if err != nil {
		return err
	}
	ref, err := spaceref.Parse(invocation.space, root.Domain)
	if err != nil {
		return err
	}
	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return err
	}
	target, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, ref.Domain)
	if err != nil {
		return err
	}
	if target.State != cloud.StateRunning {
		return &space.NotRunningError{Domain: ref.Domain, State: target.State}
	}
	built, err := build.Suite(ctx, c, sha, version)
	if err != nil {
		return err
	}
	detail := built.File
	if label != "" {
		detail = label + ", " + detail
	}
	space.Step(stdout, "build", detail)
	keys := 0
	for _, manifest := range built.Manifests {
		if len(manifest.Secrets) == 0 {
			continue
		}
		held, err := secrets.Names(ctx, session.Clients.SSM, ref.Domain, manifest.App)
		if err != nil {
			return err
		}
		heldSet := make(map[string]bool, len(held))
		for _, name := range held {
			heldSet[name] = true
		}
		required := make(map[string]bool, len(manifest.Secrets))
		for _, name := range manifest.Secrets {
			required[name] = true
		}
		missing := make([]string, 0)
		for name := range required {
			if !heldSet[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) != 0 {
			sort.Strings(missing)
			return &MissingSecretsError{App: manifest.App, Space: ref.Label, Names: missing}
		}
		keys += len(required)
	}
	space.Step(stdout, "secrets", fmt.Sprintf("%d apps, %d keys", len(built.Manifests), keys))
	h := host.Host{Address: target.Address, Deps: deps}
	if err := release.Put(ctx, h, stdout, c.Path(built.File), sha); err != nil {
		return err
	}
	return release.Activate(ctx, h, stdout, sha, label)
}
