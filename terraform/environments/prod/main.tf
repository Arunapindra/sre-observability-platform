###############################################################################
# Production Environment - SRE Observability Platform
###############################################################################

terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.40" }
  }

  backend "s3" {
    bucket         = "sre-platform-terraform-state"
    key            = "environments/prod/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "sre-platform-terraform-locks"
  }
}

provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = {
      Project     = "sre-observability-platform"
      Environment = "prod"
      ManagedBy   = "terraform"
    }
  }
}

module "networking" {
  source      = "../../modules/networking"
  project     = "sre-platform"
  environment = "prod"
  vpc_cidr    = "10.1.0.0/16"
}

module "monitoring" {
  source      = "../../modules/monitoring"
  project     = "sre-platform"
  environment = "prod"
  alert_email = ""
}
