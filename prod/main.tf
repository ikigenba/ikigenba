resource "aws_route53_zone" "env" {
  name    = "prod.metaspot.org"
  comment = "prod environment subdomain, delegated from metaspot.org in the mgmt account"
}

resource "aws_route53_zone" "ai" {
  name    = "ai.metaspot.org"
  comment = "ai subdomain, delegated from metaspot.org in the mgmt account"
}
