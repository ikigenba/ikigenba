package spacecreate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func provision(ctx context.Context, stdout io.Writer, deps seam.Deps, invocation invocation, result preflightResult) error {
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

	if err := runHostSteps(ctx, stdout, deps, result, address.IP, running.ID, invocation.acmeEmail); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s %s\n", result.sp.Domain, address.IP)
	return err
}
