resource "aws_route53_record" "apex_mx" {
  zone_id = aws_route53_zone.metaspot_org.zone_id
  name    = "metaspot.org"
  type    = "MX"
  ttl     = 60
  records = ["1 SMTP.GOOGLE.COM"]
}

resource "aws_route53_record" "apex_txt" {
  zone_id = aws_route53_zone.metaspot_org.zone_id
  name    = "metaspot.org"
  type    = "TXT"
  ttl     = 60
  records = ["google-site-verification=SVxnpxWDWCdHfyExry-8qSdWZBuM1FUIJMsoRUjXrWQ"]
}
