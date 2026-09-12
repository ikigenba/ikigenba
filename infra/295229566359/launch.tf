# The instance profile is deliberately absent: the operator-side tool passes
# the per-space profile at launch time.
resource "aws_launch_template" "space" {
  name                   = "ikigenba-space"
  image_id               = local.ami
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

  user_data = base64encode(file("${path.module}/../templates/space-first-boot.sh"))
}
