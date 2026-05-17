output "hosted_zone_id" {
  value = aws_route53_zone.env.zone_id
}

output "hosted_zone_name_servers" {
  description = "NS records to delegate prod.metaspot.org from the metaspot.org zone in mgmt."
  value       = aws_route53_zone.env.name_servers
}

output "ai_hosted_zone_id" {
  value = aws_route53_zone.ai.zone_id
}

output "ai_hosted_zone_name_servers" {
  description = "NS records to delegate ai.metaspot.org from the metaspot.org zone in mgmt."
  value       = aws_route53_zone.ai.name_servers
}

output "biz_public_ip" {
  description = "Elastic IP for biz.prod.metaspot.org"
  value       = aws_eip.biz.public_ip
}

output "biz_ssh" {
  description = "SSH command for the biz server"
  value       = "ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@${aws_eip.biz.public_ip}"
}
