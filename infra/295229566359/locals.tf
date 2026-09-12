# The account's knobs. Everything a space host is launched with comes from
# here; changes to the launch template affect new launches only, never a
# running space.
locals {
  ami                = "ami-01c265752adadcdf8" # AL2023 x86_64, us-east-2 (same pin as the dev host in dev.tf)
  instance_type      = "t3.small"
  root_volume_gb     = 10
  backup_expiry_days = 30
  # Backup periods, in seconds, for every service in every space; 0 means never.
  backup_full_seconds        = 604800
  backup_incremental_seconds = 86400
  backup_wal_seconds         = 900
  ssh_admin_cidr             = "208.118.151.172/32"
  # The monthly cost budget only notifies; it never stops or changes anything.
  budget_monthly_usd = 75
  budget_email       = "mgreenly+295229566359@gmail.com"
}
