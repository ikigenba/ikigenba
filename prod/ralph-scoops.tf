# -----------------------------------------------------------------------------
# ralph-scoops.prod.metaspot.org — t3.micro with 8 GiB root, public IP
# -----------------------------------------------------------------------------

resource "aws_security_group" "ralph_scoops" {
  name        = "ralph-scoops"
  description = "Allow HTTP, HTTPS, and SSH"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTP"
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

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_instance" "ralph_scoops" {
  ami                    = "ami-0cf8dce2cda56aa67" # Amazon Linux 2023, us-east-2
  instance_type          = "t3.micro"
  key_name               = aws_key_pair.ai4mgreenly.key_name
  vpc_security_group_ids = [aws_security_group.ralph_scoops.id]
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ralph_scoops.name

  tags = {
    Name = "ralph-scoops"
  }
}

# --- Anthropic API key (Parameter Store) ---

resource "aws_ssm_parameter" "anthropic_api_key" {
  name        = "/metaspot/prod/anthropic-api-key"
  description = "Anthropic API key for ralph-scoops. Real value is set out-of-band via CLI."
  type        = "SecureString"
  value       = "PLACEHOLDER-set-via-aws-ssm-put-parameter"

  lifecycle {
    ignore_changes = [value]
  }
}

# --- Instance role: read only this one SSM parameter ---

resource "aws_iam_role" "ralph_scoops" {
  name = "ralph-scoops"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "ralph_scoops_anthropic_key" {
  name = "anthropic-api-key-read"
  role = aws_iam_role.ralph_scoops.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "ssm:GetParameter"
        Resource = aws_ssm_parameter.anthropic_api_key.arn
      },
      {
        Effect   = "Allow"
        Action   = "kms:Decrypt"
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

resource "aws_iam_instance_profile" "ralph_scoops" {
  name = "ralph-scoops"
  role = aws_iam_role.ralph_scoops.name
}

resource "aws_eip" "ralph_scoops" {
  instance = aws_instance.ralph_scoops.id

  tags = {
    Name = "ralph-scoops"
  }
}

resource "aws_route53_record" "ralph_scoops" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "ralph-scoops.prod.metaspot.org"
  type    = "A"
  ttl     = 300
  records = [aws_eip.ralph_scoops.public_ip]
}

resource "aws_route53_record" "ralph_scoops_ai" {
  zone_id = aws_route53_zone.ai.zone_id
  name    = "ralph-scoops.ai.metaspot.org"
  type    = "CNAME"
  ttl     = 300
  records = [aws_route53_record.ralph_scoops.name]
}
