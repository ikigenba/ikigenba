# The default-VPC lookups (data.aws_vpc.default, data.aws_subnets.default)
# are declared in shared.tf and shared with the dev host.
resource "aws_security_group" "space" {
  name_prefix = "ikigenba-space-"
  description = "space hosts: public HTTP/HTTPS, admin-only SSH"
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
