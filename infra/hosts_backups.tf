# Host backups: one bucket for every SSH host's accounts, keyed
# accounts/<account>/files/ (restic), accounts/<account>/data/ (Litestream)
# and hosts/<host>/ (the host key). Separate from the suite's bucket, whose
# expiry would corrupt a restic repository. Named for the hosts domain with
# its dots as dashes, so clients address it virtual-hosted over TLS. No
# force_destroy: it holds customers' data.
resource "aws_s3_bucket" "hosts_backups" {
  bucket = "${replace(local.hosts_domain, ".", "-")}-hosts-backups"
}

resource "aws_s3_bucket_public_access_block" "hosts_backups" {
  bucket                  = aws_s3_bucket.hosts_backups.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "hosts_backups" {
  bucket = aws_s3_bucket.hosts_backups.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "hosts_backups" {
  bucket = aws_s3_bucket.hosts_backups.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# A delete only hides an object: restic and Litestream prune their own
# history, and the hidden copy stays until the lifecycle rule removes it.
resource "aws_s3_bucket_versioning" "hosts_backups" {
  bucket = aws_s3_bucket.hosts_backups.id
  versioning_configuration {
    status = "Enabled"
  }
}

# TLS only, and no credentials issued through the backup role may remove a
# version, whatever a session policy says; so nothing on a host can
# permanently destroy a backup.
resource "aws_s3_bucket_policy" "hosts_backups" {
  bucket = aws_s3_bucket.hosts_backups.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyInsecureTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource = [
          aws_s3_bucket.hosts_backups.arn,
          "${aws_s3_bucket.hosts_backups.arn}/*",
        ]
        Condition = {
          Bool = { "aws:SecureTransport" = "false" }
        }
      },
      {
        Sid       = "DenyHostVersionDeletes"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:DeleteObjectVersion"
        Resource  = "${aws_s3_bucket.hosts_backups.arn}/*"
        Condition = {
          ArnEquals = { "aws:PrincipalArn" = aws_iam_role.hosts_backup.arn }
        }
      },
    ]
  })
}

# No rule expires a current object. Hidden (noncurrent) versions go after
# hosts_backup_noncurrent_days; unfinished multipart uploads are aborted.
resource "aws_s3_bucket_lifecycle_configuration" "hosts_backups" {
  bucket     = aws_s3_bucket.hosts_backups.id
  depends_on = [aws_s3_bucket_versioning.hosts_backups]

  rule {
    id     = "noncurrent"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = local.hosts_backup_noncurrent_days
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = local.hosts_backup_abort_days
    }
  }
}

# The role accounts assumes to issue each host its backup credentials, with a
# session policy narrowing these rights to that host's own accounts'
# prefixes and its hosts/<host>/ prefix; the credentials expire within the
# hour. It trusts the account, so only an identity policy that grants
# sts:AssumeRole on it (the hosts-operator policy) lets anything assume it.
# No host holds it: the hosts' own role has no rights on the bucket.
resource "aws_iam_role" "hosts_backup" {
  name        = "${var.domain}-hosts-backup"
  description = "Assumed by accounts with a session policy to issue SSH hosts scoped backup credentials."
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "sts:AssumeRole"
      },
    ]
  })
}

resource "aws_iam_role_policy" "hosts_backup" {
  name = "hosts-backups"
  role = aws_iam_role.hosts_backup.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # Delete hides an object (versioning); version deletes are denied
        # by the bucket policy.
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:AbortMultipartUpload",
        ]
        Resource = "${aws_s3_bucket.hosts_backups.arn}/*"
      },
      {
        # restic and Litestream list their prefix and look up the bucket's
        # region.
        Effect = "Allow"
        Action = [
          "s3:ListBucket",
          "s3:GetBucketLocation",
        ]
        Resource = aws_s3_bucket.hosts_backups.arn
      },
    ]
  })
}
