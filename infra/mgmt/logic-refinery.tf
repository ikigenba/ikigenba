# logic-refinery.* apex zones, migrated from standalone account 900253156012.
#
# Registration is transferred into mgmt via route53domains
# transfer-domain-to-another-aws-account (registrar stays Route 53, no ICANN
# 60-day lock). The hosted zone does NOT ride along with that transfer, so these
# zones are created fresh here and the domains' nameservers are repointed at
# them after the new zones are verified to resolve. See logic-refinery-MIGRATION
# notes / outputs.tf for the NS sets to set at the registrar.
#
# Only logic-refinery.com carries live content: the Google Workspace MX set
# (replicated verbatim from the source zone). .io/.net/.tv are parked — empty
# apex zones, no records beyond the NS/SOA Route 53 creates automatically. The
# old account's space.logic-refinery.io / public.logic-refinery.io zones are
# being discarded, so no delegations are recreated here.

resource "aws_route53_zone" "logic_refinery_com" {
  name = "logic-refinery.com"
}

resource "aws_route53_zone" "logic_refinery_io" {
  name = "logic-refinery.io"
}

resource "aws_route53_zone" "logic_refinery_net" {
  name = "logic-refinery.net"
}

resource "aws_route53_zone" "logic_refinery_tv" {
  name = "logic-refinery.tv"
}

# Google Workspace mail for logic-refinery.com — the one record set that must be
# byte-identical to the source before the nameserver cutover so mail never gaps.
resource "aws_route53_record" "logic_refinery_com_mx" {
  zone_id = aws_route53_zone.logic_refinery_com.zone_id
  name    = "logic-refinery.com"
  type    = "MX"
  ttl     = 3600
  records = [
    "1 ASPMX.L.GOOGLE.COM",
    "5 ALT1.ASPMX.L.GOOGLE.COM",
    "5 ALT2.ASPMX.L.GOOGLE.COM",
    "10 ALT3.ASPMX.L.GOOGLE.COM",
    "10 ALT4.ASPMX.L.GOOGLE.COM",
  ]
}
