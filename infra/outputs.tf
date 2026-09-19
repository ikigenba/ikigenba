# For the operator only. devctl reads nothing from here: it finds every
# resource by the domain's name or tag.
output "hosted_zone_name_servers" {
  description = "Name servers to configure at the registrar in the mgmt account."
  value       = aws_route53_zone.root.name_servers
}
