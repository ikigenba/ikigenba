terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  backend "s3" {
    bucket       = "metaspot-test-tfstate-213629091798"
    key          = "test/terraform.tfstate"
    region       = "us-east-2"
    profile      = "test"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = "test"
  region  = "us-east-2"

  default_tags {
    tags = {
      Project     = "metaspot"
      Environment = "test"
      ManagedBy   = "terraform"
    }
  }
}
