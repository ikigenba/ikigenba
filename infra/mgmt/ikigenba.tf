# ikigenba.com / ikigenba.dev — registered in mgmt. ikigenba.com is a second
# *customer* apex that plays the same role metaspot.org does: mgmt owns the apex
# zone and delegates <account>.ikigenba.com out to per-customer member accounts
# via NS records added here as customers onboard (mirroring delegations.tf).
#
# ikigenba.dev registration also remains in mgmt, but its registrar nameservers
# delegate authority to the dev account. The old mgmt zone below is transitional
# and answers identically while the former delegation remains cached.
# Authoritative nameservers, set at the registrar on 2026-09-07:
#   ns-132.awsdns-16.com
#   ns-1385.awsdns-45.org
#   ns-2032.awsdns-62.co.uk
#   ns-560.awsdns-06.net
#
# Both zones were imported from the hosted zones Route 53 created at
# registration. Do not recreate them before completing the ikigenba.dev
# transition cleanup.
resource "aws_route53_zone" "ikigenba_com" {
  name = "ikigenba.com"
}

resource "aws_route53_zone" "ikigenba_dev" {
  name = "ikigenba.dev"
}

# Transitional records for the registrar handoff to the dev account's
# authoritative zone. Keep the old zone answering identically while cached
# registrar delegation expires; remove the zone after the handoff has soaked.
moved {
  from = aws_route53_record.parked_apex["ikigenba.dev"]
  to   = aws_route53_record.ikigenba_dev_transition_apex
}

resource "aws_route53_record" "ikigenba_dev_transition_apex" {
  zone_id = aws_route53_zone.ikigenba_dev.zone_id
  name    = "ikigenba.dev"
  type    = "A"
  ttl     = 300
  records = ["77.112.106.79"]
}

resource "aws_route53_record" "ikigenba_dev_transition_wildcard" {
  zone_id = aws_route53_zone.ikigenba_dev.zone_id
  name    = "*.ikigenba.dev"
  type    = "A"
  ttl     = 300
  records = ["77.112.106.79"]
}

# int.ikigenba.com — first customer (dogfooding) account under ikigenba.com.
# NS delegation from the apex zone here in mgmt to the int/ account's own
# int.ikigenba.com hosted zone. Values are the nameservers from the int/ root's
# hosted_zone_name_servers output.
resource "aws_route53_record" "delegation_int_ikigenba" {
  zone_id = aws_route53_zone.ikigenba_com.zone_id
  name    = "int.ikigenba.com"
  type    = "NS"
  ttl     = 300
  records = [
    "ns-1022.awsdns-63.net.",
    "ns-1066.awsdns-05.org.",
    "ns-1855.awsdns-39.co.uk.",
    "ns-320.awsdns-40.com.",
  ]
}
