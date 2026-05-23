output "hosted_zone_id" {
  value = aws_route53_zone.env.zone_id
}

output "hosted_zone_name_servers" {
  description = "NS records to delegate ai.metaspot.org from the metaspot.org zone in mgmt."
  value       = aws_route53_zone.env.name_servers
}

output "ai_public_ip" {
  description = "Elastic IP for ai.metaspot.org"
  value       = aws_eip.ai.public_ip
}

output "ai_ssh" {
  description = "SSH command for the ai server"
  value       = "ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@${aws_eip.ai.public_ip}"
}
