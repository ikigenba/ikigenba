package space

import (
	"encoding/json"

	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

type policyDocument struct {
	Version   string            `json:"Version"`
	Statement []policyStatement `json:"Statement"`
}

type policyStatement struct {
	Effect    string                         `json:"Effect"`
	Action    any                            `json:"Action"`
	Resource  string                         `json:"Resource"`
	Condition map[string]map[string][]string `json:"Condition,omitempty"`
}

// PolicyDocument builds the permissions policy for a space's host role.
func PolicyDocument(root, zoneID string, sp spaceref.Space, apex bool) string {
	recordNames := RecordNames(sp.Domain)
	if apex {
		recordNames = append(recordNames, "_acme-challenge."+root)
	}
	document := policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
			{
				Effect:   "Allow",
				Action:   "ssm:GetParameter",
				Resource: "arn:aws:ssm:*:*:parameter/" + sp.Domain + "/*",
			},
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:PutObject"},
				Resource: "arn:aws:s3:::" + root + "/" + sp.Label + "/*",
			},
			{
				Effect:   "Allow",
				Action:   "s3:ListBucket",
				Resource: "arn:aws:s3:::" + root,
				Condition: map[string]map[string][]string{
					"StringLike": {"s3:prefix": {BackupPrefix(sp.Label), BackupPrefix(sp.Label) + "*"}},
				},
			},
			{
				Effect:   "Allow",
				Action:   "route53:ListResourceRecordSets",
				Resource: "arn:aws:route53:::hostedzone/" + zoneID,
			},
			{
				Effect:   "Allow",
				Action:   "route53:GetChange",
				Resource: "arn:aws:route53:::change/*",
			},
			{
				Effect:   "Allow",
				Action:   "route53:ChangeResourceRecordSets",
				Resource: "arn:aws:route53:::hostedzone/" + zoneID,
				Condition: map[string]map[string][]string{
					"ForAllValues:StringEquals": {
						"route53:ChangeResourceRecordSetsRecordTypes": {"TXT"},
					},
					"ForAllValues:StringLike": {
						"route53:ChangeResourceRecordSetsNormalizedRecordNames": recordNames,
					},
				},
			},
		},
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
