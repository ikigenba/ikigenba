// Package access issues short-lived credentials for each host backup prefix.
package access

import "github.com/ikigenba/ikigenba/accounts/internal/settings"

// Policy returns the exact STS session policy for objects under prefix.
func Policy(bucket, partition, prefix string) string {
	arn := "arn:" + partition + ":s3:::" + bucket
	return `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:AbortMultipartUpload"],"Resource":"` + arn + "/" + prefix +
		`*"},{"Effect":"Allow","Action":"s3:ListBucket","Resource":"` + arn + `","Condition":{"StringLike":{"s3:prefix":"` + prefix +
		`*"}}},{"Effect":"Allow","Action":"s3:GetBucketLocation","Resource":"` + arn + `"}]}`
}

// RoleARN returns the backup role in the caller's partition and account.
func RoleARN(partition, account string, h settings.Hosts) string {
	return "arn:" + partition + ":iam::" + account + ":role/" + h.BackupRole()
}

// SessionName distinguishes backup role sessions from instance role sessions.
func SessionName(host, slug string) string {
	name := "backup-" + host
	if slug != "" {
		name += "-" + slug
	}
	return name[:min(len(name), 64)]
}
