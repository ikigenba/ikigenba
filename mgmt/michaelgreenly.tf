# michaelgreenly.com — registered in mgmt, no longer parked. The apex and www
# now resolve to a static-site box that lives in the *prod* account
# (prod/michaelgreenly.tf). Not part of the metaspot naming family, but kept
# under management here alongside the other registered domains. Zone imported
# from the pre-existing hosted zone; registrar nameservers already point here.
resource "aws_route53_zone" "michaelgreenly_com" {
  name = "michaelgreenly.com"
}

# Cross-account pointer to the prod box's EIP. Hardcoded literal — same
# discipline as the NS delegations and the pinned AMI (the repo never wires a
# terraform_remote_state cross-account read). Fill this in from the prod root's
# `michaelgreenly_public_ip` output after `terraform apply` in prod/.
#
#   Value is the prod root's michaelgreenly_public_ip output (EIP allocated once
#   and stable). Apply order: prod/ apply → set EIP here → mgmt/ apply → run the
#   about repo's setup.sh.
locals {
  michaelgreenly_eip = "3.13.196.10" # prod aws_eip.michaelgreenly, EIP alloc eipalloc-0a1b4c34af0f4807c
}

resource "aws_route53_record" "michaelgreenly_apex" {
  zone_id = aws_route53_zone.michaelgreenly_com.zone_id
  name    = "michaelgreenly.com"
  type    = "A"
  ttl     = 300
  records = [local.michaelgreenly_eip]
}

resource "aws_route53_record" "michaelgreenly_www" {
  zone_id = aws_route53_zone.michaelgreenly_com.zone_id
  name    = "www.michaelgreenly.com"
  type    = "A"
  ttl     = 300
  records = [local.michaelgreenly_eip]
}
