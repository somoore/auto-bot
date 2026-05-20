locals {
  aws_region                 = "us-east-1"
  project_name               = "auto-bot"
  aws_provider_version       = "6.45.0"
  cloudinit_provider_version = "2.4.0"
  state_bucket               = "${local.project_name}-terraform-state-${get_aws_account_id()}"
  lock_table                 = "${local.project_name}-terraform-locks"
}

remote_state {
  backend = "s3"

  generate = {
    path      = "backend.generated.tf"
    if_exists = "overwrite_terragrunt"
  }

  config = {
    bucket         = local.state_bucket
    key            = "${path_relative_to_include()}/terraform.tfstate"
    region         = local.aws_region
    encrypt        = true
    dynamodb_table = local.lock_table

    s3_bucket_tags = {
      Project   = local.project_name
      ManagedBy = "terragrunt"
    }

    dynamodb_table_tags = {
      Project   = local.project_name
      ManagedBy = "terragrunt"
    }
  }
}

generate "provider" {
  path      = "provider.generated.tf"
  if_exists = "overwrite_terragrunt"

  contents = <<EOF
terraform {
  required_version = "= 1.15.2"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "= ${local.aws_provider_version}"
    }
    cloudinit = {
      source  = "hashicorp/cloudinit"
      version = "= ${local.cloudinit_provider_version}"
    }
  }
}

provider "aws" {
  region = "${local.aws_region}"

  default_tags {
    tags = {
      Project   = "${local.project_name}"
      ManagedBy = "terraform"
    }
  }
}
EOF
}
