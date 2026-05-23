# -----------------------------------------------------------------------------
# hal.prod.metaspot.org — t3.micro with public IP.
#
# Mirrors biz.tf (the canonical "create a server" reference, see AGENTS.md).
# Reuses the shared data.aws_vpc.default / data.aws_subnets.default,
# aws_key_pair.ai4mgreenly, the Route53 zones, and the backups bucket from
# shared.tf — one SSH key for the whole fleet.
#
# Created with: secrets = yes (app-config grant present), t3.micro, 10 GiB gp3.
# -----------------------------------------------------------------------------

resource "aws_security_group" "hal" {
  # name_prefix (not name) + create_before_destroy: an SG's description is
  # immutable, so editing it forces a replace. With a fixed name the new SG
  # collides with the old one's name during create_before_destroy; with a
  # prefix AWS generates a unique name. create_before_destroy makes Terraform
  # build the new SG and repoint the instance to it *before* deleting the old
  # one — without it the default destroy-first order hits DependencyViolation
  # (old SG still attached to the live instance) and retries ~15 min.
  name_prefix = "hal-"
  description = "Allow public HTTP/HTTPS and admin-only SSH"
  vpc_id      = data.aws_vpc.default.id

  # Port 80 is open to the world for Let's Encrypt: certbot's HTTP-01
  # challenge is validated by Let's Encrypt servers (no fixed CIDR), and
  # nginx serves the ACME challenge on 80 then redirects all other 80
  # traffic to 443. SSH stays locked to the admin IP — that is the
  # security win we keep; 80 being public is required, not a regression.
  ingress {
    description = "HTTP (ACME challenge + redirect to 443)"
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
    cidr_blocks = ["216.173.146.119/32"]
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

resource "aws_instance" "hal" {
  ami                    = "ami-0f5b1543e7f934f48" # AL2023 2023.11.20260514, kernel-6.18, us-east-2
  instance_type          = "t3.micro"
  key_name               = aws_key_pair.ai4mgreenly.key_name
  vpc_security_group_ids = [aws_security_group.hal.id]
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.hal.name

  root_block_device {
    volume_type = "gp3"
    volume_size = 10
  }

  user_data = templatefile("${path.module}/templates/metaspot-env.sh.tftpl", {
    env            = "prod"
    node           = "hal"
    fqdn           = "hal.prod.metaspot.org"
    dns_zone       = "prod.metaspot.org"
    aws_account_id = "853624428511"
    aws_region     = "us-east-2"
    backup_bucket  = aws_s3_bucket.backups.bucket
  })
  user_data_replace_on_change = false

  tags = {
    Name = "hal"
  }

  lifecycle {
    # AMI is bumped in code for future replacements only; the running box is
    # upgraded in place (devlog 2026-05-17). user_data is the first-boot
    # baseline only — cloud-init never re-runs it, and live boxes get
    # /etc/metaspot/env written out-of-band. Both are ignored so a code
    # change can never force-replace a live prod box.
    ignore_changes = [ami, user_data]
  }
}

# --- Instance role ---
#
# Per the AGENTS.md standard every server has an instance role + profile: the
# backups-prefix grant is standard (below), and the app-config secrets grant
# is the opt-in extra on the same role (present here — created with
# secrets = yes). Shared resources (the app-config parameter, the backups
# bucket) live in app-config.tf / shared.tf, never per-server.

resource "aws_iam_role" "hal" {
  name = "hal"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "hal_app_config" {
  name = "app-config-rw"
  role = aws_iam_role.hal.id

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
      }
    ]
  })
}

resource "aws_iam_role_policy" "hal_backups" {
  name = "backups-rw"
  role = aws_iam_role.hal.id

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
        Resource = "${aws_s3_bucket.backups.arn}/hal/*"
      },
      {
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.backups.arn
        Condition = {
          StringLike = {
            "s3:prefix" = ["hal/*"]
          }
        }
      }
    ]
  })
}

resource "aws_iam_instance_profile" "hal" {
  name = "hal"
  role = aws_iam_role.hal.name
}

resource "aws_eip" "hal" {
  instance = aws_instance.hal.id

  tags = {
    Name = "hal"
  }
}

resource "aws_route53_record" "hal" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "hal.prod.metaspot.org"
  type    = "A"
  ttl     = 300
  records = [aws_eip.hal.public_ip]
}

