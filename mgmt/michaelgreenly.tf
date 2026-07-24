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
