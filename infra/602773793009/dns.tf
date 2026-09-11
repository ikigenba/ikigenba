resource "aws_route53_zone" "sbx" {
  name    = "sbx.ikigenba.dev"
  comment = "ephemeral spaces (<name>.sbx.ikigenba.dev); delegated from the ikigenba.dev zone in account 295229566359"
}
