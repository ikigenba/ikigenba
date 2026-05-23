output "hosted_zone_id" {
  value = aws_route53_zone.env.zone_id
}

output "hosted_zone_name_servers" {
  description = "NS records to delegate sandbox.metaspot.org from the metaspot.org zone in mgmt."
  value       = aws_route53_zone.env.name_servers
}
