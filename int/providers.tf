terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-int-tfstate-704229156466"
    key          = "int/terraform.tfstate"
    region       = "us-east-2"
    profile      = "int"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "int"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "int"
      ManagedBy   = "terraform"
    }
  }
}
