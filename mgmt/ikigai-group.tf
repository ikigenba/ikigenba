# ikigai-group.io — registered in mgmt, parked.
#
# The hosted zone predates this repo and was created outside Terraform, so it is
# adopted with the import block below rather than created. Zone currently holds
# only its own NS and SOA; there is no mail, verification, or delegation record
# to preserve. Its apex A record is declared in parked.tf alongside the other
# parked domains.
#
# Remove the import block once the first `terraform apply` has adopted the zone;
# it is a one-shot adoption directive, not ongoing configuration.
#
# WARNING: this registration has AutoRenew = false and expires 2026-09-02.
# Unlike every other domain in this account, it will lapse on that date unless
# auto-renew is turned back on. Parking it at the int box does not change that.
import {
  to = aws_route53_zone.ikigai_group_io
  id = "Z0936316M120RO4MQ7WG"
}

resource "aws_route53_zone" "ikigai_group_io" {
  name = "ikigai-group.io"
}
