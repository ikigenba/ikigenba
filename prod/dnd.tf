# -----------------------------------------------------------------------------
# dnd.prod.metaspot.org — static website served by nginx on a t3.micro
# -----------------------------------------------------------------------------

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

# --- SSH key ---

resource "aws_key_pair" "ai4mgreenly" {
  key_name   = "ai4mgreenly"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICrlK7XC7ym0s74i/nUce7aHcqV3khy8irgKVDj4yc5S claude@logic-refinery.com"
}

# --- Security group ---

resource "aws_security_group" "dnd" {
  name        = "dnd-web"
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

# --- EC2 instance ---

resource "aws_instance" "dnd" {
  ami                    = "ami-0cf8dce2cda56aa67" # Amazon Linux 2023, us-east-2
  instance_type          = "t3.micro"
  key_name               = aws_key_pair.ai4mgreenly.key_name
  vpc_security_group_ids = [aws_security_group.dnd.id]
  subnet_id              = data.aws_subnets.default.ids[0]

  tags = {
    Name = "dnd-web"
  }
}

# --- Elastic IP ---

resource "aws_eip" "dnd" {
  instance = aws_instance.dnd.id

  tags = {
    Name = "dnd-web"
  }
}

# --- DNS ---

resource "aws_route53_record" "dnd" {
  zone_id = aws_route53_zone.env.zone_id
  name    = "dnd.prod.metaspot.org"
  type    = "A"
  ttl     = 300
  records = [aws_eip.dnd.public_ip]
}

resource "aws_route53_record" "dnd_ai" {
  zone_id = aws_route53_zone.ai.zone_id
  name    = "dnd.ai.metaspot.org"
  type    = "CNAME"
  ttl     = 300
  records = [aws_route53_record.dnd.name]
}
