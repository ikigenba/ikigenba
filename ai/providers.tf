terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-ai-tfstate-417780655767"
    key          = "ai/terraform.tfstate"
    region       = "us-east-2"
    profile      = "ai"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "ai"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "ai"
      ManagedBy   = "terraform"
    }
  }
}
