# The one hosted zone. Delegated at the registrar from the management account,
# which is out of scope here. It holds no records of its own: the apex A
# record and every space's records are written by devctl.
resource "aws_route53_zone" "root" {
  name    = var.domain
  comment = "${var.domain} root; delegated at the registrar from the mgmt account"

  lifecycle {
    prevent_destroy = true
  }
}

moved {
  from = aws_route53_zone.env
  to   = aws_route53_zone.root
}
