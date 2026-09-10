output "hosted_zone_id" {
  value = aws_route53_zone.env.zone_id
}

output "hosted_zone_name_servers" {
  description = "Nameservers configured at the ikigenba.dev registrar in the mgmt account."
  value       = aws_route53_zone.env.name_servers
}

output "dev_public_ip" {
  description = "Elastic IP for ikigenba.dev"
  value       = aws_eip.dev.public_ip
}

output "dev_ssh" {
  description = "SSH command for the dev server"
  value       = "ssh -i ~/.ssh/id_ed25519_ikigenba_dev ec2-user@${aws_eip.dev.public_ip}"
}
