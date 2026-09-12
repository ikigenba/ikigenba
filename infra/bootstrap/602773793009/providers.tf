terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
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
      Component = "bootstrap"
    }
  }
}
