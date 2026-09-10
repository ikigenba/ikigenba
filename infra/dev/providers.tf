terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-dev-tfstate-295229566359"
    key          = "dev/terraform.tfstate"
    region       = "us-east-2"
    profile      = "dev"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "dev"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "dev"
      ManagedBy   = "terraform"
    }
  }
}
