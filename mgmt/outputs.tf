output "apex_zone_id" {
  value = aws_route53_zone.metaspot_org.zone_id
}

output "apex_name_servers" {
  value = aws_route53_zone.metaspot_org.name_servers
}
