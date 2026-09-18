# The account's knobs. Everything a space host is launched with comes from
# here; changes to the launch template affect new launches only, never a
# running space.
locals {
  ami                = "ami-01c265752adadcdf8" # AL2023 x86_64, us-east-2 (same pin as the dev host in 295229566359)
  instance_type      = "t3.small"
  root_volume_gb     = 10
  backup_expiry_days = 7
  # Backup periods, in seconds, for every space; 0 means never. The names are
  # opsctl's backup.* keys, which devctl hands to the host at create.
  backup_host_files_seconds    = 86400
  backup_service_files_seconds = 86400
  backup_service_db_seconds    = 86400
  backup_service_wal_seconds   = 60
  ssh_admin_cidr               = "208.118.151.172/32"
  # The monthly cost budget only notifies; it never stops or changes anything.
  budget_monthly_usd = 50
  budget_email       = "mgreenly+602773793009@gmail.com"
}
