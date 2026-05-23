terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-sandbox-tfstate-654596473544"
    key          = "sandbox/terraform.tfstate"
    region       = "us-east-2"
    profile      = "sandbox"
    encrypt      = true
    use_lockfile = true
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
    }
  }
}
