package spacecreate

import (
	_ "embed"
	"strings"
)

// AssumeRolePolicy is the trust policy for a space's EC2 role.
const AssumeRolePolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`

// PolicyTemplate is the permissions policy for a space's EC2 role.
//
//go:embed templates/space-role-policy.json
var PolicyTemplate string

func policyDocument(domain, zoneID, accountID, bucket string) string {
	return strings.NewReplacer(
		"<domain>", domain,
		"<zone_id>", zoneID,
		"<account_id>", accountID,
		"<bucket>", bucket,
	).Replace(PolicyTemplate)
}
