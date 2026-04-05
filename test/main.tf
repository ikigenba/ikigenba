resource "aws_route53_zone" "env" {
  name    = "test.metaspot.org"
  comment = "test environment subdomain, delegated from metaspot.org in the mgmt account"
}
