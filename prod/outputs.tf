output "hosted_zone_id" {
  value = aws_route53_zone.env.zone_id
}

output "hosted_zone_name_servers" {
  description = "NS records to delegate prod.metaspot.org from the metaspot.org zone in mgmt."
  value       = aws_route53_zone.env.name_servers
}

output "michaelgreenly_public_ip" {
  description = "EIP of the michaelgreenly.com box. Copy into mgmt/michaelgreenly.tf apex/www A records."
  value       = aws_eip.michaelgreenly.public_ip
}

output "michaelgreenly_ssh" {
  description = "SSH command for the michaelgreenly.com box."
  value       = "ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@${aws_eip.michaelgreenly.public_ip}"
}
