output "sandbox_name_servers" {
  description = "Name servers of the sandbox.ikigenba.dev zone; hardcoded into the NS delegation record in the ikigenba.dev zone in account 295229566359."
  value       = aws_route53_zone.sandbox.name_servers
}
