package space

import (
	"context"
	"fmt"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// WaitState waits until an instance reaches state. A running instance is not
// ready until it also has a public address.
func WaitState(
	ctx context.Context,
	deps seam.Deps,
	acct *account.Account,
	id string,
	state cloud.InstanceState,
) (cloud.Instance, error) {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		instance, err := acct.Clients.EC2.DescribeInstance(ctx, id)
		if err != nil {
			return cloud.Instance{}, err
		}
		if instance.State == state && (state != cloud.StateRunning || instance.Address != "") {
			return instance, nil
		}
		if attempt+1 < PollAttempts {
			select {
			case <-ctx.Done():
				return cloud.Instance{}, ctx.Err()
			case <-deps.After(PollInterval):
			}
		}
	}
	return cloud.Instance{}, &WaitError{Subject: id, Want: "be " + string(state)}
}

// WaitChecks waits until both instance status checks pass.
func WaitChecks(ctx context.Context, deps seam.Deps, acct *account.Account, id string) error {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		passed, err := acct.Clients.EC2.InstanceChecksPassed(ctx, id)
		if err != nil {
			return err
		}
		if passed {
			return nil
		}
		if attempt+1 < PollAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deps.After(PollInterval):
			}
		}
	}
	return &WaitError{Subject: id, Want: "pass its status checks"}
}

// WaitLaunchReady waits until EC2 accepts a launch spec.
func WaitLaunchReady(
	ctx context.Context,
	deps seam.Deps,
	acct *account.Account,
	spec cloud.LaunchSpec,
) error {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		ready, err := acct.Clients.EC2.LaunchReady(ctx, spec)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		if attempt+1 < PollAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deps.After(PollInterval):
			}
		}
	}
	return &WaitError{Subject: spec.InstanceProfile, Want: "become usable for launch"}
}

// ElasticIP finds the address allocated to domain.
func ElasticIP(ctx context.Context, acct *account.Account, domain string) (cloud.Address, bool, error) {
	addresses, err := acct.Clients.EC2.ListSpaceAddresses(ctx)
	if err != nil {
		return cloud.Address{}, false, err
	}
	var match cloud.Address
	found := false
	for _, address := range addresses {
		if address.Space != domain {
			continue
		}
		if found {
			return cloud.Address{}, false, fmt.Errorf("multiple elastic IPs for %s", domain)
		}
		match = address
		found = true
	}
	return match, found, nil
}

// ReleaseElasticIP disassociates addr when necessary, then releases it.
func ReleaseElasticIP(ctx context.Context, acct *account.Account, addr cloud.Address) error {
	if addr.AssociationID != "" {
		if err := acct.Clients.EC2.DisassociateAddress(ctx, addr.AssociationID); err != nil {
			return err
		}
	}
	return acct.Clients.EC2.ReleaseAddress(ctx, addr.AllocationID)
}

// PutRecords upserts the apex and wildcard A records and waits for propagation.
func PutRecords(
	ctx context.Context,
	deps seam.Deps,
	acct *account.Account,
	zone cloud.Zone,
	domain string,
	address string,
) error {
	names := RecordNames(domain)
	changes := make([]cloud.RecordChange, 0, len(names))
	for _, name := range names {
		changes = append(changes, cloud.RecordChange{
			Action: cloud.ChangeUpsert,
			Record: cloud.Record{Name: name, Type: "A", TTL: RecordTTL, Values: []string{address}},
		})
	}
	changeID, err := acct.Clients.Route53.ChangeRecords(ctx, zone.ID, changes)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < PollAttempts; attempt++ {
		status, err := acct.Clients.Route53.ChangeStatus(ctx, changeID)
		if err != nil {
			return err
		}
		if status == cloud.ChangeInsync {
			return nil
		}
		if attempt+1 < PollAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deps.After(PollInterval):
			}
		}
	}
	return &WaitError{Subject: changeID, Want: "reach INSYNC"}
}

// DeleteRecords deletes the returned apex and wildcard A record sets exactly
// as the provider represented them.
func DeleteRecords(ctx context.Context, acct *account.Account, zone cloud.Zone, domain string) (int, error) {
	records, err := acct.Clients.Route53.ListRecords(ctx, zone.ID)
	if err != nil {
		return 0, err
	}
	wildcard := "*." + domain
	escapedWildcard := `\052.` + domain
	changes := make([]cloud.RecordChange, 0, 2)
	for _, record := range records {
		if record.Type == "A" && (record.Name == domain || record.Name == wildcard || record.Name == escapedWildcard) {
			changes = append(changes, cloud.RecordChange{Action: cloud.ChangeDelete, Record: record})
		}
	}
	if len(changes) == 0 {
		return 0, nil
	}
	if _, err := acct.Clients.Route53.ChangeRecords(ctx, zone.ID, changes); err != nil {
		return 0, err
	}
	return len(changes), nil
}

// DeleteRole deletes the role and same-named instance profile when either
// exists. It reports whether there was anything to delete.
func DeleteRole(ctx context.Context, acct *account.Account, domain string) (bool, error) {
	name := RoleName(domain)
	roleExists, err := acct.Clients.IAM.RoleExists(ctx, name)
	if err != nil {
		return false, err
	}
	roles, profileExists, err := acct.Clients.IAM.InstanceProfileRoles(ctx, name)
	if err != nil {
		return false, err
	}
	if !roleExists && !profileExists {
		return false, nil
	}
	if err := acct.Clients.IAM.DeleteRolePolicy(ctx, name, PolicyName); err != nil {
		return false, err
	}
	for _, role := range roles {
		if err := acct.Clients.IAM.RemoveRoleFromInstanceProfile(ctx, name, role); err != nil {
			return false, err
		}
	}
	if err := acct.Clients.IAM.DeleteInstanceProfile(ctx, name); err != nil {
		return false, err
	}
	if err := acct.Clients.IAM.DeleteRole(ctx, name); err != nil {
		return false, err
	}
	return true, nil
}
