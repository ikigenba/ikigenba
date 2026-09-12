# The bucket itself (aws_s3_bucket.backups, ikigenba-dev-295229566359) and its
# access block, ownership, encryption and policy are declared in shared.tf.
resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id

  rule {
    id     = "expire"
    status = "Enabled"

    filter {}

    expiration {
      days = local.backup_expiry_days
    }
  }
}
