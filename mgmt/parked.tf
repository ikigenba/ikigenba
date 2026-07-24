# Parked apex records.
#
# Every registered domain whose apex has no live host of its own points at the
# int box (int/ account 704229156466, aws_eip.int, allocation
# eipalloc-07046468e598fd2f0). Hardcoded literal, the same cross-account
# discipline used by the NS delegations and the pinned AMIs: this repo never
# wires a terraform_remote_state cross-account read. Refresh this value from the
# int root's `int_public_ip` output if that EIP is ever reallocated.
#
# Apex only, deliberately. Parked domains get no www and no other subdomain — a
# dead domain should not carry a subdomain surface. Live subdomains are declared
# next to the zone they belong to and are untouched here; the only one today is
# the int.ikigenba.com NS delegation in ikigenba.tf.
#
# ikigai-group.io is included, but note its zone is adopted via an import block
# in ikigai-group.tf (it was created outside Terraform), and its registration has
# AutoRenew = false with an expiry of 2026-09-02. See that file.
locals {
  int_eip = "16.59.0.148"

  parked_apexes = {
    "metaspot.org"       = aws_route53_zone.metaspot_org.zone_id
    "metaspot.net"       = aws_route53_zone.metaspot_net.zone_id
    "michaelgreenly.com" = aws_route53_zone.michaelgreenly_com.zone_id
    "ikigenba.com"       = aws_route53_zone.ikigenba_com.zone_id
    "ikigenba.dev"       = aws_route53_zone.ikigenba_dev.zone_id
    "logic-refinery.com" = aws_route53_zone.logic_refinery_com.zone_id
    "logic-refinery.io"  = aws_route53_zone.logic_refinery_io.zone_id
    "logic-refinery.net" = aws_route53_zone.logic_refinery_net.zone_id
    "logic-refinery.tv"  = aws_route53_zone.logic_refinery_tv.zone_id
    "ikigai-group.io"    = aws_route53_zone.ikigai_group_io.zone_id
  }
}

resource "aws_route53_record" "parked_apex" {
  for_each = local.parked_apexes

  zone_id = each.value
  name    = each.key
  type    = "A"
  ttl     = 300
  records = [local.int_eip]
}
