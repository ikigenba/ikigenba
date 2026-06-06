resource "aws_route53_record" "delegation_prod" {
  zone_id = aws_route53_zone.metaspot_org.zone_id
  name    = "prod.metaspot.org"
  type    = "NS"
  ttl     = 300
  records = [
    "ns-1519.awsdns-61.org.",
    "ns-1727.awsdns-23.co.uk.",
    "ns-4.awsdns-00.com.",
    "ns-759.awsdns-30.net.",
  ]
}

resource "aws_route53_record" "delegation_test" {
  zone_id = aws_route53_zone.metaspot_org.zone_id
  name    = "test.metaspot.org"
  type    = "NS"
  ttl     = 300
  records = [
    "ns-1078.awsdns-06.org.",
    "ns-160.awsdns-20.com.",
    "ns-1934.awsdns-49.co.uk.",
    "ns-741.awsdns-28.net.",
  ]
}

resource "aws_route53_record" "delegation_sandbox" {
  zone_id = aws_route53_zone.metaspot_org.zone_id
  name    = "sandbox.metaspot.org"
  type    = "NS"
  ttl     = 300
  records = [
    "ns-1257.awsdns-29.org.",
    "ns-1912.awsdns-47.co.uk.",
    "ns-443.awsdns-55.com.",
    "ns-765.awsdns-31.net.",
  ]
}
