# The account's knobs. Everything a space host is launched with comes from
# here; changes to the launch template affect new launches only, never a
# running space.
locals {
  ami                = "ami-01c265752adadcdf8" # AL2023 x86_64, us-east-2 (same pin as the durable root's dev host)
  instance_type      = "t3.small"
  root_volume_gb     = 10
  backup_expiry_days = 7
  ssh_admin_cidr     = "208.118.151.172/32"
  # The monthly cost budget only notifies; it never stops or changes anything.
  budget_monthly_usd = 50
  budget_email       = "mgreenly+602773793009@gmail.com"
}
