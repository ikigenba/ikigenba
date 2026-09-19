package spacecreate

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

type preflightResult struct {
	apps           []checkout.App
	root           checkout.RootFile
	sp             spaceref.Space
	session        cloud.Session
	zone           cloud.Zone
	launchTemplate string
	boundaryARN    string
}

func preflight(ctx context.Context, deps seam.Deps, operand string) (preflightResult, error) {
	opened, err := checkout.Open(ctx, deps)
	if err != nil {
		return preflightResult{}, err
	}
	apps, err := opened.Apps()
	if err != nil {
		return preflightResult{}, err
	}
	root, err := opened.ReadRootFile()
	if err != nil {
		return preflightResult{}, err
	}
	sp, err := spaceref.Parse(operand, root.Domain)
	if err != nil {
		return preflightResult{}, err
	}
	if len(sp.Domain) > 64 {
		return preflightResult{}, &RefusedError{Message: fmt.Sprintf("'%s' is too long: a space's role name is at most 64 characters", sp.Domain)}
	}

	session, err := cloud.Connect(ctx, deps.Cloud, root.Domain, root.Region)
	if err != nil {
		return preflightResult{}, err
	}
	zone, err := session.Clients.Route53.Zone(ctx, root.Domain)
	if err != nil {
		return preflightResult{}, err
	}
	launchTemplate, err := session.Clients.EC2.LaunchTemplate(ctx, root.Domain)
	if err != nil {
		return preflightResult{}, err
	}
	boundaryARN, err := session.Clients.IAM.PermissionsBoundary(ctx, root.Domain)
	if err != nil {
		return preflightResult{}, err
	}
	_, delegated, err := session.Clients.Route53.FindRecord(ctx, zone.ID, sp.Domain, "NS")
	if err != nil {
		return preflightResult{}, err
	}
	if delegated {
		return preflightResult{}, &RefusedError{Message: fmt.Sprintf("'%s' is delegated away from '%s'", sp.Domain, zone.Name)}
	}
	existing, err := cloud.LookupSpace(ctx, session.Clients.EC2, root.Domain, sp.Domain)
	if err == nil {
		return preflightResult{}, &RefusedError{Message: fmt.Sprintf("a space at '%s' already exists (%s)", sp.Domain, existing.ID)}
	}
	var noSpace *cloud.NoSpaceError
	if !errors.As(err, &noSpace) {
		return preflightResult{}, err
	}

	roleName := space.RoleName(sp.Domain)
	roleExists, err := session.Clients.IAM.RoleExists(ctx, roleName)
	if err != nil {
		return preflightResult{}, err
	}
	_, profileExists, err := session.Clients.IAM.InstanceProfileRoles(ctx, roleName)
	if err != nil {
		return preflightResult{}, err
	}
	if roleExists || profileExists {
		return preflightResult{}, &RefusedError{Message: fmt.Sprintf("a role for '%s' already exists", sp.Domain)}
	}

	return preflightResult{apps: apps, root: root, sp: sp, session: session, zone: zone, launchTemplate: launchTemplate, boundaryARN: boundaryARN}, nil
}

func pushSecretsAndReport(ctx context.Context, deps seam.Deps, result preflightResult, stdout io.Writer) ([]secrets.Entry, error) {
	entries, err := secrets.Push(ctx, deps, result.session.Clients.SSM, result.sp.Domain, result.apps)
	if err != nil {
		return nil, err
	}
	space.Step(stdout, "account", fmt.Sprintf("%s, %s, %s", result.root.Domain, result.root.Region, result.session.AccountID))
	space.Step(stdout, "domain", fmt.Sprintf("zone %s %s", result.zone.Name, result.zone.ID))
	space.Step(stdout, "secrets", fmt.Sprintf("%d apps", len(entries)))
	return entries, nil
}
