output "hosted_zone_id" {
  value = aws_route53_zone.env.zone_id
}

output "hosted_zone_name_servers" {
  description = "NS records to delegate int.ikigenba.com from the ikigenba.com zone in mgmt."
  value       = aws_route53_zone.env.name_servers
}

output "int_public_ip" {
  description = "Elastic IP for int.ikigenba.com"
  value       = aws_eip.int.public_ip
}

output "int_ssh" {
  description = "SSH command for the int server"
  value       = "ssh -i ~/.ssh/id_ed25519_int_ikigenba_com ec2-user@${aws_eip.int.public_ip}"
}
