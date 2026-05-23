# -----------------------------------------------------------------------------
# ai.metaspot.org — the one server for the ai account.
#
# Built per the AGENTS.md "create a server" spec: one box per account, named
# after the account, answering on <account>.metaspot.org plus the wildcard
# *.<account>.metaspot.org for host-header-routed services.
# -----------------------------------------------------------------------------

resource "aws_security_group" "ai" {
  name_prefix = "ai-"
  description = "ai box: public HTTP/HTTPS, admin-only SSH"
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

resource "aws_iam_role" "ai" {
  name = "ai"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_instance_profile" "ai" {
  name = "ai"
  role = aws_iam_role.ai.name
}

# Bucket-wide backups grant. Per-app prefix (<app>/) is a convention enforced
# by bin/backup, not by IAM — see AGENTS.md Backups.
resource "aws_iam_role_policy" "ai_backups_rw" {
  name = "backups-rw"
  role = aws_iam_role.ai.id
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

# Env-wide app-config grant (secrets = yes).
resource "aws_iam_role_policy" "ai_app_config" {
  name = "app-config"
  role = aws_iam_role.ai.id
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

resource "aws_instance" "ai" {
  ami           = "ami-00a9f44477dd83e3d" # AL2023 x86_64, us-east-2, resolved 2026-05-23
  instance_type = "t3.micro"
  subnet_id     = data.aws_subnets.default.ids[0]

  vpc_security_group_ids = [aws_security_group.ai.id]
  key_name               = aws_key_pair.ai4mgreenly.key_name
  iam_instance_profile   = aws_iam_instance_profile.ai.name

  root_block_device {
    volume_type = "gp3"
    volume_size = 10
  }

  user_data = templatefile("${path.module}/../templates/metaspot-env.sh.tftpl", {
    env             = "ai"
    node            = "ai"
    fqdn            = "ai.metaspot.org"
    domain          = "ai.metaspot.org"
    dns_zone        = aws_route53_zone.env.zone_id
    aws_account_id  = "417780655767"
    aws_region      = "us-east-2"
    backup_bucket   = aws_s3_bucket.backups.bucket
    launcher_source = file("${path.module}/../templates/metaspot-launch")
  })
  user_data_replace_on_change = false

  tags = {
    Name = "ai"
  }

  lifecycle {
    ignore_changes = [ami, user_data]
  }
}

resource "aws_eip" "ai" {
  instance = aws_instance.ai.id
  domain   = "vpc"

  tags = {
    Name = "ai"
  }
}

resource "aws_route53_record" "ai_apex" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "ai.metaspot.org"
  type    = "A"
  ttl     = 300
  records = [aws_eip.ai.public_ip]
}

resource "aws_route53_record" "ai_wildcard" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "*.ai.metaspot.org"
  type    = "A"
  ttl     = 300
  records = [aws_eip.ai.public_ip]
}
