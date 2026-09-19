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
  profile = var.domain
  region  = var.region

  default_tags {
    tags = {
      Domain    = var.domain
      ManagedBy = "terraform"
      Component = "bootstrap"
    }
  }
}
