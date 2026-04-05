terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-prod-tfstate-853624428511"
    key          = "prod/terraform.tfstate"
    region       = "us-east-2"
    profile      = "prod"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "prod"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "prod"
      ManagedBy   = "terraform"
    }
  }
}
