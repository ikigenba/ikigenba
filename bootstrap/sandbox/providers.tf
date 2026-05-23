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
  profile = "sandbox"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "sandbox"
      ManagedBy   = "terraform"
      Component   = "bootstrap"
    }
  }
}
