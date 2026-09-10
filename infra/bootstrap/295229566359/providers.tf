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
  profile = "295229566359"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project   = "metaspot"
      Account   = "295229566359"
      ManagedBy = "terraform"
      Component = "bootstrap"
    }
  }
}
