resource "aws_ssm_parameter" "account" {
  name        = "/ikigenba/account"
  type        = "String"
  description = "Account properties read by devctl; written only by terraform. `domain` is the suffix every space's domain must end in (equal to it for the apex space)."
  value = jsonencode({
    domain                       = "sbx.ikigenba.dev"
    backup_bucket                = aws_s3_bucket.backups.bucket
    backup_host_files_seconds    = local.backup_host_files_seconds
    backup_service_files_seconds = local.backup_service_files_seconds
    backup_service_db_seconds    = local.backup_service_db_seconds
    backup_service_wal_seconds   = local.backup_service_wal_seconds
    launch_template_id           = aws_launch_template.space.id
    permissions_boundary_arn     = aws_iam_policy.space_boundary.arn
    region                       = "us-east-2"
    delete_secrets_on_destroy    = true
    delete_backups_on_destroy    = true
  })
}
