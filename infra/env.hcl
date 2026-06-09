locals {
  name_prefix = "auto-bot-dev"

  inputs = {
    aws_region  = "us-east-1"
    name_prefix = local.name_prefix

    # Dedicated auto-bot VPC (created by the module) — public subnets only, no
    # NAT. Kept separate from any other project's VPC to avoid coupling/drift.
    vpc_cidr = "10.40.0.0/16"

    voice_provider = "nova-sonic"

    # Scale-to-zero by default: a fresh apply creates the stack with no running
    # task (≈$0 compute idle). scripts/aws-app.sh up scales to 1 on demand.
    app_desired_count = 0

    allowed_ingress_cidrs = ["0.0.0.0/0"]

    log_retention_days = 14

    bedrock_model_arns = ["arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-2-sonic-v1:0"]

    # Edge auth: Cognito Hosted UI + Google/Microsoft federation in front of the app.
    auth_domain_name      = "meet.sc.tt"
    cognito_domain_prefix = "auto-bot-dev-meet"
    # Access gate: only these emails/domains may use the app after login. Any
    # allowed user can host or join; whoever creates a meeting becomes its host.
    allowed_emails        = "scott@moore.cloud,somoore2025@gmail.com"
    allowed_email_domains = "moore.cloud"
  }
}
