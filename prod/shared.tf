# -----------------------------------------------------------------------------
# Shared, env-wide primitives every server implicitly depends on.
#
# Relocated here so tearing down any individual server (it was dnd.tf that
# historically held these) can never take the fleet's network lookups, SSH
# key, or DNS zones with it. Moving a block between .tf files does not change
# its Terraform address, so this relocation is a state no-op — not a destroy.
#
# (The opt-in secrets blob /metaspot/prod/app-config stays in app-config.tf;
# it's a standard component with its own devlog, not generic plumbing.)
# -----------------------------------------------------------------------------

# --- Default VPC / subnets ---

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

# --- Fleet SSH key (one key for every prod box) ---

resource "aws_key_pair" "ai4mgreenly" {
  key_name   = "ai4mgreenly"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICrlK7XC7ym0s74i/nUce7aHcqV3khy8irgKVDj4yc5S claude@logic-refinery.com"
}

# --- Route53 zones (delegated subtrees of metaspot.org, owned by mgmt) ---

resource "aws_route53_zone" "env" {
  name    = "prod.metaspot.org"
  comment = "prod environment subdomain, delegated from metaspot.org in the mgmt account"
}

# --- Shared backups bucket ---
#
# One env-wide bucket. Each server writes only under its own key prefix
# (<node>/), enforced by per-server IAM in <name>.tf — there is no
# bucket-wide object grant anywhere. Isolation is IAM-only (one SSE-S3 key
# for all prefixes); see devlog. Account ID suffix gives a globally-unique
# name without a random suffix.

resource "aws_s3_bucket" "backups" {
  bucket = "metaspot-prod-backups-853624428511"
}

resource "aws_s3_bucket_public_access_block" "backups" {
  bucket                  = aws_s3_bucket.backups.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_policy" "backups" {
  bucket = aws_s3_bucket.backups.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyInsecureTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource = [
          aws_s3_bucket.backups.arn,
          "${aws_s3_bucket.backups.arn}/*",
        ]
        Condition = {
          Bool = { "aws:SecureTransport" = "false" }
        }
      }
    ]
  })
}
