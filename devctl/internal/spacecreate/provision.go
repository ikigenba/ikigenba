package spacecreate

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func provision(ctx context.Context, stdout io.Writer, deps seam.Deps, invocation invocation, result preflightResult, built build.Release, dir string) error {
	clients := result.session.Clients
	roleName := space.RoleName(result.sp.Domain)
	role := cloud.RoleSpec{Name: roleName, AssumeRolePolicy: AssumeRolePolicy, PermissionsBoundaryARN: result.boundaryARN}
	if err := clients.IAM.CreateRole(ctx, role); err != nil {
		return err
	}
	if err := clients.IAM.PutRolePolicy(ctx, roleName, space.PolicyName, space.PolicyDocument(result.root.Domain, result.zone.ID, result.sp, false)); err != nil {
		return err
	}
	if err := clients.IAM.CreateInstanceProfile(ctx, roleName); err != nil {
		return err
	}
	if err := clients.IAM.AddRoleToInstanceProfile(ctx, roleName, roleName); err != nil {
		return err
	}
	launchSpec := cloud.LaunchSpec{LaunchTemplateID: result.launchTemplate, InstanceProfile: roleName, Domain: result.root.Domain, Space: result.sp.Domain}
	if err := space.WaitLaunchReady(ctx, deps, clients.EC2, launchSpec); err != nil {
		return err
	}
	space.Step(stdout, "role", roleName)

	launched, err := clients.EC2.RunInstance(ctx, launchSpec)
	if err != nil {
		return err
	}
	running, err := space.WaitState(ctx, deps, clients.EC2, launched.ID, cloud.StateRunning)
	if err != nil {
		return err
	}
	space.Step(stdout, "instance", fmt.Sprintf("%s running, %s", running.ID, running.Address))

	address, err := clients.EC2.AllocateAddress(ctx, result.root.Domain, result.sp.Domain)
	if err != nil {
		return err
	}
	if err := space.WaitAssociate(ctx, deps, clients.EC2, address.AllocationID, running.ID); err != nil {
		return err
	}
	space.Step(stdout, "address", "elastic ip "+address.IP+" associated")

	if err := space.PutRecords(ctx, deps, clients.Route53, result.zone, result.sp.Domain, address.IP); err != nil {
		return err
	}
	space.Step(stdout, "records", fmt.Sprintf("created %s -> %s, INSYNC", strings.Join(space.RecordNames(result.sp.Domain), ", "), address.IP))

	if err := Resolve(ctx, stdout, deps, result.sp.Domain, address.IP); err != nil {
		return err
	}
	periods := hostsetup.DefaultBackupPeriods()
	l := Launch{InstanceID: running.ID, SHA: result.sha, Label: result.label, File: result.checkout.Path(built.File), Manifests: built.Manifests, Config: hostsetup.Config{Root: result.root.Domain, Region: result.root.Region, ZoneID: result.zone.ID, Space: result.sp, Email: invocation.acmeEmail, Periods: &periods, Opsctl: release.Opsctl(result.sha)}, KnownHosts: filepath.Join(dir, "known_hosts.launch")}
	r, err := SetUpHost(ctx, stdout, deps, clients.EC2, clients.S3, l)
	if err != nil {
		return err
	}
	return finishCreate(ctx, stdout, deps, invocation, result, l, r, address.IP)
}
