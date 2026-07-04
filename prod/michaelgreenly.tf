# -----------------------------------------------------------------------------
# michaelgreenly.com — static personal site, hosted on a box in the prod account.
#
# Deliberate deviations from the AGENTS.md "create a server" spec, because this
# box serves a *registered apex outside the metaspot.org family* rather than a
# metaspot subdomain:
#
#   Deviation A — the box is named `michaelgreenly`, not `prod`, and answers on
#   michaelgreenly.com. There is NO local A record and NO *.prod.metaspot.org
#   entry here: the michaelgreenly.com zone lives in the mgmt account
#   (mgmt/michaelgreenly.tf), so the apex/www A records that point at this box's
#   EIP are created *there*, referencing the EIP literal (same cross-account
#   hardcode discipline as the NS delegations). After `terraform apply` here,
#   copy the `michaelgreenly_public_ip` output into mgmt/michaelgreenly.tf.
#
#   Deviation B — the site is static (Astro `dist/` served by nginx), not a
#   loopback REST/MCP service. None of the launcher/systemd/SSM/path-routing
#   service machinery applies; deploy follows the simple nginx+certbot+rsync
#   static pattern (see the about repo's scripts/, mirroring the old dnd box).
#
# Everything else follows the standard: own SG (80/443 public, 22 admin-only),
# pinned AL2023 AMI, gp3 root, EIP, instance role with the unconditional
# backups-rw + app-config baseline grants, user_data from the shared template.
# -----------------------------------------------------------------------------

resource "aws_security_group" "michaelgreenly" {
  name_prefix = "michaelgreenly-"
  description = "michaelgreenly box: public HTTP/HTTPS, admin-only SSH"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTP (Lets Encrypt HTTP-01 + 80 to 443 redirect)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "SSH (admin IP only)"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["208.118.151.172/32"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_iam_role" "michaelgreenly" {
  name = "michaelgreenly"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_instance_profile" "michaelgreenly" {
  name = "michaelgreenly"
  role = aws_iam_role.michaelgreenly.name
}

# Bucket-wide backups grant. Per-app prefix (<app>/) is a convention enforced
# by bin/backup, not by IAM — see AGENTS.md Backups.
resource "aws_iam_role_policy" "michaelgreenly_backups_rw" {
  name = "backups-rw"
  role = aws_iam_role.michaelgreenly.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
        ]
        Resource = "${aws_s3_bucket.backups.arn}/*"
      },
      {
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.backups.arn
      },
    ]
  })
}

# Env-wide app-config grant. Unconditional baseline even though a static site
# uses no secrets — keeps every instance role uniform (see AGENTS.md).
resource "aws_iam_role_policy" "michaelgreenly_app_config" {
  name = "app-config"
  role = aws_iam_role.michaelgreenly.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ssm:GetParameter",
          "ssm:PutParameter",
        ]
        Resource = aws_ssm_parameter.app_config.arn
      },
      {
        Effect = "Allow"
        Action = [
          "kms:Decrypt",
          "kms:GenerateDataKey",
        ]
        Resource = "*"
        Condition = {
          StringEquals = {
            "kms:ViaService" = "ssm.us-east-2.amazonaws.com"
          }
        }
      },
    ]
  })
}

resource "aws_instance" "michaelgreenly" {
  ami           = "ami-078f95be0757084a3" # AL2023 x86_64, us-east-2, resolved 2026-06-06
  instance_type = "t3.nano"
  subnet_id     = data.aws_subnets.default.ids[0]

  vpc_security_group_ids = [aws_security_group.michaelgreenly.id]
  key_name               = aws_key_pair.ai4mgreenly.key_name
  iam_instance_profile   = aws_iam_instance_profile.michaelgreenly.name

  root_block_device {
    volume_type = "gp3"
    volume_size = 10
  }

  # dns_zone is the account's env zone (prod.metaspot.org) as identity metadata
  # only; the michaelgreenly.com zone itself lives in the mgmt account. The box
  # does no DNS work, so this value is non-load-bearing.
  user_data = templatefile("${path.module}/../templates/ikigenba-env.sh.tftpl", {
    env             = "prod"
    node            = "michaelgreenly"
    fqdn            = "michaelgreenly.com"
    domain          = "michaelgreenly.com"
    dns_zone        = aws_route53_zone.env.zone_id
    aws_account_id  = "853624428511"
    aws_region      = "us-east-2"
    backup_bucket   = aws_s3_bucket.backups.bucket
    launcher_source = file("${path.module}/../templates/ikigenba-launch")
  })
  user_data_replace_on_change = false

  tags = {
    Name = "michaelgreenly"
  }

  lifecycle {
    ignore_changes = [ami, user_data]
  }
}

resource "aws_eip" "michaelgreenly" {
  instance = aws_instance.michaelgreenly.id
  domain   = "vpc"

  tags = {
    Name = "michaelgreenly"
  }
}
