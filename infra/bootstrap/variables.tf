# Terraform loads terraform.tfvars.json only from its own root, so this root
# takes the same two values by `-var-file=../terraform.tfvars.json`.
variable "domain" {
  description = "The root domain; also the AWS profile name."
  type        = string
}

variable "region" {
  description = "The AWS region."
  type        = string
}
