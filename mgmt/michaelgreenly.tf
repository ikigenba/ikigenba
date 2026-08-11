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
