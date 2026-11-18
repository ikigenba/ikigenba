package space

import (
	"context"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

// HostsTagKey and HostsTagValue identify the role of the hosts operator.
const (
	HostsTagKey   = "HostsOperator"
	HostsTagValue = "true"
)

// HostsPolicyName returns the name of the managed policy for the hosts operator.
func HostsPolicyName(root string) string {
	return root + "-hosts-operator"
}

// DetachHostsPolicy detaches the hosts operator policy if it is attached to the role.
func DetachHostsPolicy(ctx context.Context, iam cloud.IAM, root, domain string) (bool, error) {
	role := RoleName(domain)
	arn, attached, err := iam.AttachedPolicy(ctx, role, HostsPolicyName(root))
	if err != nil || !attached {
		return false, err
	}
	if err := iam.DetachRolePolicy(ctx, role, arn); err != nil {
		return false, err
	}
	return true, nil
}
