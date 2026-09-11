resource "aws_ssm_parameter" "account" {
  name        = "/ikigenba/account"
  type        = "String"
  description = "Account properties read by devctl; written only by terraform."
  value = jsonencode({
    zone_name                 = "sandbox.ikigenba.dev"
    zone_id                   = aws_route53_zone.sandbox.zone_id
    backup_bucket             = aws_s3_bucket.backups.bucket
    launch_template_id        = aws_launch_template.space.id
    permissions_boundary_arn  = aws_iam_policy.space_boundary.arn
    region                    = "us-east-2"
    deploy_from_main_only     = false
    delete_secrets_on_destroy = true
    delete_backups_on_destroy = true
  })
}
