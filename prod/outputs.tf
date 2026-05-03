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

output "dnd_public_ip" {
  description = "Elastic IP for dnd.prod.metaspot.org"
  value       = aws_eip.dnd.public_ip
}

output "dnd_ssh" {
  description = "SSH command for the DnD web server"
  value       = "ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@${aws_eip.dnd.public_ip}"
}

output "dnd_rsync" {
  description = "rsync command to deploy static content"
  value       = "rsync -avz -e 'ssh -i ~/.ssh/id_ed25519_ai4mgreenly' html/ ec2-user@${aws_eip.dnd.public_ip}:/var/www/dnd/"
}

output "ralph_scoops_public_ip" {
  description = "Elastic IP for ralph-scoops.prod.metaspot.org"
  value       = aws_eip.ralph_scoops.public_ip
}

output "ralph_scoops_ssh" {
  description = "SSH command for the ralph-scoops server"
  value       = "ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@${aws_eip.ralph_scoops.public_ip}"
}
