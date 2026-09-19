package space

import (
	"context"
	"fmt"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// WaitState waits until an instance reaches state. A running instance is not
// ready until it also has a public address.
func WaitState(
	ctx context.Context,
	deps seam.Deps,
	ec2 cloud.EC2,
	id string,
	state cloud.InstanceState,
) (cloud.Instance, error) {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		instance, err := ec2.DescribeInstance(ctx, id)
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
func WaitChecks(ctx context.Context, deps seam.Deps, ec2 cloud.EC2, id string) error {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		passed, err := ec2.InstanceChecksPassed(ctx, id)
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
	ec2 cloud.EC2,
	spec cloud.LaunchSpec,
) error {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		ready, err := ec2.LaunchReady(ctx, spec)
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

// WaitInsync waits until a Route 53 change has propagated.
func WaitInsync(ctx context.Context, deps seam.Deps, route53 cloud.Route53, changeID string) error {
	for attempt := 0; attempt < PollAttempts; attempt++ {
		status, err := route53.ChangeStatus(ctx, changeID)
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

// ElasticIP finds the address allocated to domain.
func ElasticIP(ctx context.Context, ec2 cloud.EC2, root, domain string) (cloud.Address, bool, error) {
	addresses, err := ec2.ListSpaceAddresses(ctx, root)
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
func ReleaseElasticIP(ctx context.Context, ec2 cloud.EC2, addr cloud.Address) error {
	if addr.AssociationID != "" {
		if err := ec2.DisassociateAddress(ctx, addr.AssociationID); err != nil {
			return err
		}
	}
	return ec2.ReleaseAddress(ctx, addr.AllocationID)
}

// PutRecords upserts the apex and wildcard A records and waits for propagation.
func PutRecords(
	ctx context.Context,
	deps seam.Deps,
	route53 cloud.Route53,
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
	changeID, err := route53.ChangeRecords(ctx, zone.ID, changes)
	if err != nil {
		return err
	}
	return WaitInsync(ctx, deps, route53, changeID)
}

// DeleteRecords deletes the returned apex and wildcard A record sets exactly
// as the provider represented them.
func DeleteRecords(ctx context.Context, route53 cloud.Route53, zone cloud.Zone, domain string) ([]string, error) {
	records, err := route53.ListRecords(ctx, zone.ID)
	if err != nil {
		return nil, err
	}
	wildcard := "*." + domain
	escapedWildcard := `\052.` + domain
	changes := make([]cloud.RecordChange, 0, 2)
	bareFound := false
	wildcardFound := false
	for _, record := range records {
		if record.Type == "A" && (record.Name == domain || record.Name == wildcard || record.Name == escapedWildcard) {
			changes = append(changes, cloud.RecordChange{Action: cloud.ChangeDelete, Record: record})
			bareFound = bareFound || record.Name == domain
			wildcardFound = wildcardFound || record.Name == wildcard || record.Name == escapedWildcard
		}
	}
	if len(changes) == 0 {
		return []string{}, nil
	}
	if _, err := route53.ChangeRecords(ctx, zone.ID, changes); err != nil {
		return nil, err
	}
	deleted := make([]string, 0, 2)
	if bareFound {
		deleted = append(deleted, domain)
	}
	if wildcardFound {
		deleted = append(deleted, wildcard)
	}
	return deleted, nil
}

// DeleteRole deletes the role and same-named instance profile when either
// exists. It reports whether there was anything to delete.
func DeleteRole(ctx context.Context, iam cloud.IAM, domain string) (bool, error) {
	name := RoleName(domain)
	roleExists, err := iam.RoleExists(ctx, name)
	if err != nil {
		return false, err
	}
	roles, profileExists, err := iam.InstanceProfileRoles(ctx, name)
	if err != nil {
		return false, err
	}
	if !roleExists && !profileExists {
		return false, nil
	}
	if err := iam.DeleteRolePolicy(ctx, name, PolicyName); err != nil {
		return false, err
	}
	for _, role := range roles {
		if err := iam.RemoveRoleFromInstanceProfile(ctx, name, role); err != nil {
			return false, err
		}
	}
	if err := iam.DeleteInstanceProfile(ctx, name); err != nil {
		return false, err
	}
	if err := iam.DeleteRole(ctx, name); err != nil {
		return false, err
	}
	return true, nil
}

// ApexRecord is the root A record and the space whose address it names.
type ApexRecord struct {
	Record cloud.Record
	Found  bool
	Holder string
}

// FindApex finds the root A record and identifies its address holder.
func FindApex(ctx context.Context, route53 cloud.Route53, ec2 cloud.EC2, zoneID, root string) (ApexRecord, error) {
	record, found, err := route53.FindRecord(ctx, zoneID, root, "A")
	if err != nil {
		return ApexRecord{}, err
	}
	if !found {
		return ApexRecord{}, nil
	}
	addresses, err := ec2.ListSpaceAddresses(ctx, root)
	if err != nil {
		return ApexRecord{}, err
	}
	result := ApexRecord{Record: record, Found: true}
	matches := 0
	for _, address := range addresses {
		for _, value := range record.Values {
			if address.IP == value {
				matches++
				result.Holder = address.Space
				break
			}
		}
	}
	if matches > 1 {
		return ApexRecord{}, fmt.Errorf("multiple elastic IPs match apex for %s", root)
	}
	return result, nil
}
