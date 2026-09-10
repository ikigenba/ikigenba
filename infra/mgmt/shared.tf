resource "aws_route53_zone" "metaspot_org" {
  name = "metaspot.org"
}

# metaspot.net — registered in mgmt and parked. Empty apex zone (no records
# beyond the NS/SOA Route 53 creates automatically); imported into Terraform
# from the pre-existing hosted zone. Registrar nameservers already point here.
resource "aws_route53_zone" "metaspot_net" {
  name = "metaspot.net"
}
