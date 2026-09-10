# Live custom-domain host records for metaspot.org.
#
# metaspot.org is served by the sites custom-domain tier on the int box (it is no
# longer merely parked). The apex A record is in parked.tf (points at the same int
# EIP); this adds the wildcard so any bound <sub>.metaspot.org resolves to the box,
# matching the wildcard TLS cert (metaspot-org-dns01.tf) and nginx's
# server_name *.metaspot.org. A subdomain that is not bound to a site still
# resolves but serves no site — the sites /vhost/ router returns not-found.
#
# local.int_eip is defined in parked.tf (module-scoped); reused here so the EIP is
# pinned in exactly one place.
resource "aws_route53_record" "metaspot_org_wildcard" {
  zone_id = aws_route53_zone.metaspot_org.zone_id
  name    = "*.metaspot.org"
  type    = "A"
  ttl     = 300
  records = [local.int_eip]
}
