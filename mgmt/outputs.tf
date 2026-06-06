output "apex_zone_id" {
  value = aws_route53_zone.metaspot_org.zone_id
}

output "apex_name_servers" {
  value = aws_route53_zone.metaspot_org.name_servers
}

output "metaspot_net_name_servers" {
  value = aws_route53_zone.metaspot_net.name_servers
}

output "michaelgreenly_com_name_servers" {
  value = aws_route53_zone.michaelgreenly_com.name_servers
}

# Nameservers for the migrated logic-refinery.* zones. After these zones are
# created and verified, set each domain's registrar nameservers to its set
# (route53domains update-domain-nameservers, once registration is in mgmt).
output "logic_refinery_name_servers" {
  value = {
    "logic-refinery.com" = aws_route53_zone.logic_refinery_com.name_servers
    "logic-refinery.io"  = aws_route53_zone.logic_refinery_io.name_servers
    "logic-refinery.net" = aws_route53_zone.logic_refinery_net.name_servers
    "logic-refinery.tv"  = aws_route53_zone.logic_refinery_tv.name_servers
  }
}

output "ikigenba_name_servers" {
  value = {
    "ikigenba.com" = aws_route53_zone.ikigenba_com.name_servers
    "ikigenba.dev" = aws_route53_zone.ikigenba_dev.name_servers
  }
}
