package spacereplace

import (
	"context"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

type preflightResult struct {
	checkout   *checkout.Checkout
	sha, label string
	root       checkout.RootFile
	sp         spaceref.Space
	session    cloud.Session
	zone       cloud.Zone
	template   string
	old        cloud.Space
}

func preflight(ctx context.Context, deps seam.Deps, inv invocation) (preflightResult, error) {
	var p preflightResult
	var err error
	p.checkout, err = checkout.Open(ctx, deps)
	if err != nil {
		return p, err
	}
	rev := inv.release
	if rev == "" {
		var found bool
		rev, found, err = p.checkout.NewestRelease(ctx)
		if err != nil {
			return p, err
		}
		if !found {
			return p, &spacecreate.RefusedError{Message: "no release-<N> tag in this checkout; name one with --release <sha|tag>"}
		}
	}
	p.sha, p.label, err = release.Resolve(ctx, p.checkout, rev)
	if err != nil {
		return p, err
	}
	p.root, err = p.checkout.ReadRootFile()
	if err != nil {
		return p, err
	}
	p.sp, err = spaceref.Parse(inv.operand, p.root.Domain)
	if err != nil {
		return p, err
	}
	p.session, err = cloud.Connect(ctx, deps.Cloud, p.root.Domain, p.root.Region)
	if err != nil {
		return p, err
	}
	clients := p.session.Clients
	p.zone, err = clients.Route53.Zone(ctx, p.root.Domain)
	if err != nil {
		return p, err
	}
	p.template, err = clients.EC2.LaunchTemplate(ctx, p.root.Domain)
	if err != nil {
		return p, err
	}
	_, err = clients.IAM.PermissionsBoundary(ctx, p.root.Domain)
	if err != nil {
		return p, err
	}
	p.old, err = cloud.LookupSpace(ctx, clients.EC2, p.root.Domain, p.sp.Domain)
	if err != nil {
		return p, err
	}
	if !inv.noBackup && p.old.ID != "" && p.old.State != cloud.StateRunning && p.old.State != cloud.StateStopped {
		return p, &space.NotRunningError{Domain: p.sp.Domain, State: p.old.State}
	}
	return p, nil
}
