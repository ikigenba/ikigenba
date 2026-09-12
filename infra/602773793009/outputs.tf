output "sbx_name_servers" {
  description = "Name servers of the sbx.ikigenba.dev zone; hardcoded into the NS delegation record in the ikigenba.dev zone in account 295229566359."
  value       = aws_route53_zone.sbx.name_servers
}

output "launch_template_id" {
  description = "The ikigenba-space launch template every space host is launched from."
  value       = aws_launch_template.space.id
}

output "space_boundary_arn" {
  description = "Permissions boundary attached to every per-space instance role."
  value       = aws_iam_policy.space_boundary.arn
}

output "backup_bucket" {
  description = "Backup bucket shared by all spaces; each space writes under its own <domain>/ prefix."
  value       = aws_s3_bucket.backups.bucket
}

output "space_security_group_id" {
  description = "Security group the launch template attaches to every space host."
  value       = aws_security_group.space.id
}
