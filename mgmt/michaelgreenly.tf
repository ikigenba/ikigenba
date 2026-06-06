# michaelgreenly.com — registered in mgmt and parked. Not part of the metaspot
# naming family, but kept under management here alongside the other registered
# domains. Empty apex zone (no records beyond the NS/SOA Route 53 creates
# automatically); imported from the pre-existing hosted zone. Registrar
# nameservers already point here.
resource "aws_route53_zone" "michaelgreenly_com" {
  name = "michaelgreenly.com"
}
