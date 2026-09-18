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

func provision(
	ctx context.Context,
	stdout io.Writer,
	deps seam.Deps,
	invocation invocation,
	result preflightResult,
) error {
	acct := result.account
	domain := invocation.domain
	roleName := space.RoleName(domain)
	role := cloud.RoleSpec{
		Name:                   roleName,
		AssumeRolePolicy:       AssumeRolePolicy,
		PermissionsBoundaryARN: acct.Properties.PermissionsBoundaryARN,
	}
	if err := acct.Clients.IAM.CreateRole(ctx, role); err != nil {
		return err
	}
	policy := policyDocument(domain, result.zone.ID, result.accountID, acct.Properties.BackupBucket)
	if err := acct.Clients.IAM.PutRolePolicy(ctx, roleName, space.PolicyName, policy); err != nil {
		return err
	}
	if err := acct.Clients.IAM.CreateInstanceProfile(ctx, roleName); err != nil {
		return err
	}
	if err := acct.Clients.IAM.AddRoleToInstanceProfile(ctx, roleName, roleName); err != nil {
		return err
	}
	launchSpec := cloud.LaunchSpec{
		LaunchTemplateID: acct.Properties.LaunchTemplateID,
		InstanceProfile:  roleName,
		Space:            domain,
	}
	if err := space.WaitLaunchReady(ctx, deps, acct, launchSpec); err != nil {
		return err
	}
	space.Step(stdout, "role", roleName)

	launched, err := acct.Clients.EC2.RunInstance(ctx, launchSpec)
	if err != nil {
		return err
	}
	running, err := space.WaitState(ctx, deps, acct, launched.ID, cloud.StateRunning)
	if err != nil {
		return err
	}
	space.Step(stdout, "instance", fmt.Sprintf("%s running, %s", running.ID, running.Address))

	address, err := acct.Clients.EC2.AllocateAddress(ctx, domain)
	if err != nil {
		return err
	}
	if err := acct.Clients.EC2.AssociateAddress(ctx, address.AllocationID, running.ID); err != nil {
		return err
	}
	space.Step(stdout, "address", "elastic ip "+address.IP+" associated")

	if err := space.PutRecords(ctx, deps, acct, result.zone, domain, address.IP); err != nil {
		return err
	}
	space.Step(stdout, "records", fmt.Sprintf("created %s -> %s, INSYNC", strings.Join(space.RecordNames(domain), ", "), address.IP))

	if err := RunHostSteps(ctx, stdout, deps, acct, domain, address.IP, running.ID, result.zone, invocation.acmeEmail); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s %s\n", domain, address.IP)
	return err
}
