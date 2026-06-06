# ikigenba.com / ikigenba.dev — registered in mgmt. Unlike michaelgreenly.com
# (parked), these are live. ikigenba.com is a second *customer* apex that plays
# the same role metaspot.org does: mgmt owns the apex zone and delegates
# <account>.ikigenba.com out to per-customer member accounts via NS records,
# added here as customers onboard (mirroring delegations.tf). No customers exist
# yet, so there are no delegations today.
#
# ikigenba.dev is the developer-docs site for the ikigenba.com services and
# carries NO customer subdomains — a single apex, no delegation tree.
#
# Both zones are imported from the hosted zones Route 53 auto-creates at
# registration; the registrar nameservers already point at them (same as
# michaelgreenly.com), so no nameserver cutover is needed. Do NOT `terraform
# apply` these resources before importing — apply would create fresh empty
# zones distinct from the registered ones. Sequence is: register-domain →
# terraform import → plan (no-op).
resource "aws_route53_zone" "ikigenba_com" {
  name = "ikigenba.com"
}

resource "aws_route53_zone" "ikigenba_dev" {
  name = "ikigenba.dev"
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
