# The knobs. Everything a space host is launched with comes from here;
# changes to the launch template affect new launches only, never a running
# space. Backup periods are not here: devctl carries their defaults and sets
# them on the host at space create.
locals {
  # The AMI is the newest AL2023 arm64 (Graviton) image, read from AWS's
  # public SSM parameter at plan time (launch.tf), so an apply after AWS
  # publishes a new image moves the template to it.
  ami_parameter      = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
  instance_type      = "t4g.medium"
  root_volume_gb     = 80
  backup_expiry_days = 30
  # The monthly cost budget only notifies; it never stops or changes anything.
  budget_monthly_usd = 75
  budget_email       = "mgreenly+295229566359@gmail.com"
}
