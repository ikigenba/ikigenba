terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }

  # A backend block takes no variables, so the profile is not written here:
  # run Terraform with AWS_PROFILE set to the domain (see AGENTS.md). The
  # region literal duplicates terraform.tfvars.json for the same reason. The
  # bucket and key predate the single-root layout and keep their names so the
  # state never moves.
  backend "s3" {
    bucket       = "metaspot-dev-tfstate-295229566359"
    key          = "295229566359/terraform.tfstate"
    region       = "us-east-2"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  profile = var.domain
  region  = var.region

  default_tags {
    tags = {
      Domain    = var.domain
      ManagedBy = "terraform"
    }
  }
}
