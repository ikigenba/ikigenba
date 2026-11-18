# The instance profile is deliberately absent: devctl passes the per-space
# profile at launch time. devctl finds this template by name.
data "aws_ssm_parameter" "ami" {
  name = local.ami_parameter
}

resource "aws_launch_template" "space" {
  name                   = var.domain
  image_id               = data.aws_ssm_parameter.ami.insecure_value
  instance_type          = local.instance_type
  key_name               = aws_key_pair.space.key_name
  update_default_version = true

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_type           = "gp3"
      volume_size           = local.root_volume_gb
      delete_on_termination = true
    }
  }

  network_interfaces {
    associate_public_ip_address = true
    security_groups             = [aws_security_group.space.id]
    delete_on_termination       = true
  }

  metadata_options {
    http_tokens = "required"
  }

  credit_specification {
    cpu_credits = "unlimited"
  }

  user_data = base64encode(file("${path.module}/templates/space-first-boot.sh"))
}

# SSH hosts. accounts finds this template by name and passes the hosts'
# instance profile (iam.tf) and every tag at launch; the template carries
# neither, nor a key pair: there is no admin SSH login on a host. The data
# volume is accounts' own, created and attached apart from the instance.
# Instance tags are readable from IMDS so first boot can read AccountsURL.
resource "aws_launch_template" "hosts" {
  name                   = "${var.domain}-hosts"
  image_id               = data.aws_ssm_parameter.ami.insecure_value
  instance_type          = local.host_instance_type
  update_default_version = true

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_type           = "gp3"
      volume_size           = local.host_root_volume_gb
      delete_on_termination = true
    }
  }

  network_interfaces {
    associate_public_ip_address = true
    security_groups             = [aws_security_group.hosts.id]
    delete_on_termination       = true
  }

  metadata_options {
    http_tokens            = "required"
    instance_metadata_tags = "enabled"
  }

  credit_specification {
    cpu_credits = "unlimited"
  }

  user_data = base64encode(file("${path.module}/templates/hosts-first-boot.sh"))
}
