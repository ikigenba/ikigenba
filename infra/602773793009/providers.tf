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
    profile      = "ikigenba-sandbox"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "ikigenba-sandbox"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project   = "ikigenba"
      Account   = "602773793009"
      ManagedBy = "terraform"
    }
  }
}
