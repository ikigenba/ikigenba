# Only the public half is here; the private key is the operator's.
resource "aws_key_pair" "space" {
  key_name   = var.domain
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMXmTHdexci0p0SIK2dhOdvT4zzeLaDbGYTNBHBflm+D ikigenba.dev"
}
