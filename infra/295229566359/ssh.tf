# Same public key as the legacy ikigenba_dev pair in shared.tf, under the
# name the ikigenba-space launch template expects.
resource "aws_key_pair" "space" {
  key_name   = "ikigenba"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMXmTHdexci0p0SIK2dhOdvT4zzeLaDbGYTNBHBflm+D ikigenba.dev"
}
