# -----------------------------------------------------------------------------
# int.ikigenba.com — the one server for the int account.
#
# Built per the AGENTS.md "create a server" spec: one box per account, named
# after the account, answering on <account>.ikigenba.com plus the wildcard
# *.<account>.ikigenba.com for host-header-routed services.
# -----------------------------------------------------------------------------

resource "aws_security_group" "int" {
  name_prefix = "int-"
  description = "int box: public HTTP/HTTPS, admin-only SSH"
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

resource "aws_iam_role" "int" {
  name = "int"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_instance_profile" "int" {
  name = "int"
  role = aws_iam_role.int.name
}

# Bucket-wide backups grant. Per-app prefix (<app>/) is a convention enforced
# by bin/backup, not by IAM — see AGENTS.md Backups.
resource "aws_iam_role_policy" "int_backups_rw" {
  name = "backups-rw"
  role = aws_iam_role.int.id
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
resource "aws_iam_role_policy" "int_app_config" {
  name = "app-config"
  role = aws_iam_role.int.id
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

resource "aws_instance" "int" {
  ami           = "ami-078f95be0757084a3" # AL2023 x86_64, us-east-2, resolved 2026-06-06
  instance_type = "t3.micro"
  subnet_id     = data.aws_subnets.default.ids[0]

  vpc_security_group_ids = [aws_security_group.int.id]
  key_name               = aws_key_pair.int.key_name
  iam_instance_profile   = aws_iam_instance_profile.int.name

  root_block_device {
    volume_type = "gp3"
    volume_size = 10
  }

  user_data = templatefile("${path.module}/../templates/ikigenba-env.sh.tftpl", {
    env             = "int"
    node            = "int"
    fqdn            = "int.ikigenba.com"
    domain          = "int.ikigenba.com"
    dns_zone        = aws_route53_zone.env.zone_id
    aws_account_id  = "704229156466"
    aws_region      = "us-east-2"
    backup_bucket   = aws_s3_bucket.backups.bucket
    launcher_source = file("${path.module}/../templates/ikigenba-launch")
  })
  user_data_replace_on_change = false

  tags = {
    Name = "int"
  }

  lifecycle {
    ignore_changes = [ami, user_data]
  }
}

resource "aws_eip" "int" {
  instance = aws_instance.int.id
  domain   = "vpc"

  tags = {
    Name = "int"
  }
}

resource "aws_route53_record" "int_apex" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "int.ikigenba.com"
  type    = "A"
  ttl     = 300
  records = [aws_eip.int.public_ip]
}

resource "aws_route53_record" "int_wildcard" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "*.int.ikigenba.com"
  type    = "A"
  ttl     = 300
  records = [aws_eip.int.public_ip]
}
