terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "ikigenba-tfstate-602773793009"
    key          = "602773793009/terraform.tfstate"
    region       = "us-east-2"
    profile      = "602773793009"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "602773793009"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project    = "ikigenba"
      Account    = "602773793009"
      Durability = "ephemeral"
      ManagedBy  = "terraform"
    }
  }
}
