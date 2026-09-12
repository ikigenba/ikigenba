resource "aws_security_group" "dev" {
  name_prefix = "dev-"
  description = "ikigenba.dev: public HTTP/HTTPS, admin-only SSH"
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
    cidr_blocks = [local.ssh_admin_cidr]
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

resource "aws_iam_role" "dev" {
  name = "dev"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_instance_profile" "dev" {
  name = "dev"
  role = aws_iam_role.dev.name
}

resource "aws_iam_role_policy" "dev_backups_rw" {
  name = "backups-rw"
  role = aws_iam_role.dev.id
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

resource "aws_iam_role_policy" "dev_dns_rw" {
  name = "dns-rw"
  role = aws_iam_role.dev.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "route53:ChangeResourceRecordSets",
          "route53:ListResourceRecordSets",
        ]
        Resource = aws_route53_zone.env.arn
      },
      {
        Effect   = "Allow"
        Action   = "route53:GetChange"
        Resource = "arn:aws:route53:::change/*"
      },
    ]
  })
}

resource "aws_iam_role_policy" "dev_app_config" {
  name = "app-config"
  role = aws_iam_role.dev.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ssm:GetParameter",
          "ssm:PutParameter",
        ]
        Resource = "arn:aws:ssm:us-east-2:295229566359:parameter/ikigenba/dev/app-config/*"
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

resource "aws_instance" "dev" {
  ami           = "ami-01c265752adadcdf8" # AL2023 x86_64, us-east-2, resolved 2026-09-07
  instance_type = "t3.small"
  subnet_id     = data.aws_subnets.default.ids[0]

  vpc_security_group_ids = [aws_security_group.dev.id]
  key_name               = aws_key_pair.dev.key_name
  iam_instance_profile   = aws_iam_instance_profile.dev.name

  root_block_device {
    volume_type = "gp3"
    volume_size = 10
  }

  user_data = join("\n", [
    templatefile("${path.module}/../templates/ikigenba-env.sh.tftpl", {
      env             = "dev"
      node            = "dev"
      fqdn            = "ikigenba.dev"
      domain          = "ikigenba.dev"
      dns_zone        = aws_route53_zone.env.zone_id
      aws_account_id  = "295229566359"
      aws_region      = "us-east-2"
      backup_bucket   = aws_s3_bucket.backups.bucket
      launcher_source = file("${path.module}/../templates/ikigenba-launch")
    }),
    "dnf install -y -q nginx certbot",
    "systemctl enable nginx",
  ])
  user_data_replace_on_change = false

  tags = {
    Name = "dev"
  }

  lifecycle {
    ignore_changes = [ami, user_data]
  }
}

resource "aws_eip" "dev" {
  instance = aws_instance.dev.id
  domain   = "vpc"

  tags = {
    Name = "dev"
  }
}

resource "aws_route53_record" "dev_apex" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "ikigenba.dev"
  type    = "A"
  ttl     = 300
  records = [aws_eip.dev.public_ip]
}

resource "aws_route53_record" "dev_wildcard" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "*.ikigenba.dev"
  type    = "A"
  ttl     = 300
  records = [aws_eip.dev.public_ip]
}
