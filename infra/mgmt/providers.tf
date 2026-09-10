terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-mgmt-tfstate-132801647717"
    key          = "mgmt/terraform.tfstate"
    region       = "us-east-2"
    profile      = "mgmt"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "mgmt"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "mgmt"
      ManagedBy   = "terraform"
    }
  }
}
