resource "aws_route53_zone" "sbx" {
  name    = "sbx.ikigenba.dev"
  comment = "ephemeral spaces (*.sbx.ikigenba.dev); delegated from the ikigenba.dev zone in account 295229566359"

  lifecycle {
    prevent_destroy = true
  }
}
