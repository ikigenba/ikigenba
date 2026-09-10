resource "aws_route53_zone" "sandbox" {
  name    = "sandbox.ikigenba.dev"
  comment = "ephemeral spaces; delegated from the ikigenba.dev zone in account 295229566359"
}
