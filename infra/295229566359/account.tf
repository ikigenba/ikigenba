resource "aws_ssm_parameter" "account" {
  name        = "/ikigenba/account"
  type        = "String"
  description = "Account properties read by devctl; written only by terraform. `domain` is the default suffix for a space's domain."
  value = jsonencode({
    domain                    = "ikigenba.dev"
    backup_bucket             = aws_s3_bucket.backups.bucket
    launch_template_id        = aws_launch_template.space.id
    permissions_boundary_arn  = aws_iam_policy.space_boundary.arn
    region                    = "us-east-2"
    deploy_from_main_only     = true
    delete_secrets_on_destroy = false
    delete_backups_on_destroy = false
  })
}
