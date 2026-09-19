# The two facts every other value derives from. Both come from
# terraform.tfvars.json beside this file, which Terraform loads on its own and
# devctl reads as well; there is no other statement of either.
variable "domain" {
  description = "The root domain. Also the AWS profile name and the name of every resource here."
  type        = string
}

variable "region" {
  description = "The AWS region everything lives in."
  type        = string
}

data "aws_caller_identity" "current" {}
