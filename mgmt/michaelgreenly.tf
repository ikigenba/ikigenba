# michaelgreenly.com — registered in mgmt, now parked.
#
# The apex and www used to resolve to a static-site box in the *prod* account
# (prod/michaelgreenly.tf). That box was torn down along with the rest of prod,
# so:
#   - www.michaelgreenly.com is gone entirely (parked domains keep no subdomains)
#   - the apex A record now lives in parked.tf, pointing at the int box
#
# The zone itself is deliberately retained: the domain registration lives in
# this account and the registrar delegates to these nameservers, so deleting the
# zone would break the registration's delegation for no saving worth having.
resource "aws_route53_zone" "michaelgreenly_com" {
  name = "michaelgreenly.com"
}

# michaelgreenly.dev — registered in mgmt 2026-08-10, parked from day one.
# Zone imported from the hosted zone Route 53 auto-created at registration
# (same discipline as ikigenba.tf: register-domain → terraform import →
# plan no-op; never apply before importing). Apex A record lives in parked.tf.
# Note .dev is HSTS-preloaded: browsers force HTTPS, so the apex is only usable
# with a valid certificate. It is therefore a member of the int box's shared
# `parked` certificate lineage (ikigenba wip-domain `deploy.md`), which is what
# lets it serve the parked page rather than a cert error.
resource "aws_route53_zone" "michaelgreenly_dev" {
  name = "michaelgreenly.dev"
}

# Apex TXT: domain-ownership verification + SPF. Both are TXT at the apex, and
# Route 53 keeps all TXT strings for one name in a single record set, so SPF
# rides alongside the google-site-verification string here (two independent
# strings, not concatenated). SPF authorizes Google Workspace as the only
# sender; ~all soft-fails everything else so unaligned mail is marked, not
# dropped.
resource "aws_route53_record" "michaelgreenly_dev_txt" {
  zone_id = aws_route53_zone.michaelgreenly_dev.zone_id
  name    = "michaelgreenly.dev"
  type    = "TXT"
  ttl     = 60
  records = [
    "google-site-verification=LxgyHDB6CzwqMc2CB9f7mTUwsZ3eML5s69U1fEF7NiE",
    "v=spf1 include:_spf.google.com ~all",
  ]
}

# DMARC policy. p=none is monitor-only (no enforcement yet) — it just asks
# receivers to send aggregate reports to bot@ so alignment can be observed
# before tightening to quarantine/reject.
resource "aws_route53_record" "michaelgreenly_dev_dmarc" {
  zone_id = aws_route53_zone.michaelgreenly_dev.zone_id
  name    = "_dmarc.michaelgreenly.dev"
  type    = "TXT"
  ttl     = 60
  records = ["v=DMARC1; p=none; rua=mailto:bot@michaelgreenly.dev"]
}

# Google Workspace mail — single-host MX (same shape as metaspot.org apex_mx).
resource "aws_route53_record" "michaelgreenly_dev_mx" {
  zone_id = aws_route53_zone.michaelgreenly_dev.zone_id
  name    = "michaelgreenly.dev"
  type    = "MX"
  ttl     = 60
  records = ["1 SMTP.GOOGLE.COM"]
}

# Google Workspace DKIM (2048-bit). The value exceeds the 255-char TXT string
# limit, so it is split into two quoted chunks that Route 53 concatenates.
resource "aws_route53_record" "michaelgreenly_dev_dkim" {
  zone_id = aws_route53_zone.michaelgreenly_dev.zone_id
  name    = "google._domainkey.michaelgreenly.dev"
  type    = "TXT"
  ttl     = 60
  records = ["v=DKIM1;k=rsa;p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvsQip8Tmj0Y/1mYSRJVxH9Zh8Tq2IuZQoD2rDUwlnwLxqyLBYS0iLYCstQrc36uLSxezDKNRK2eKODnzuiqkR8s35r90e1DczNXZx2ZZxhjln1tk/HDZeiaXP3MFesQxIccdf38B39PD3VMz+93LHo76TznNurHI+NLsZwVF8d99PQ+apIgfh55TYgV9DDlogyb\"\"dYKBmQOULEgwbaORF4rqVBlj8wmL3U1j8t8OOnfeW4atAffvIcY96HwtaA/2qz5GVFB3C5l4MdIGJ0CNo2EED0qTuE6HkX1eun42PH0MrctGJaLSuf8qYhX5krXhHAB4eAF96erjDE8UUEzVG6QIDAQAB"]
}
