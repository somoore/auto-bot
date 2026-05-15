locals {
  name_prefix = "auto-bot-dev"

  inputs = {
    aws_region               = "us-east-1"
    name_prefix              = local.name_prefix
    vpc_cidr                 = "10.20.0.0/16"
    voice_provider           = "nova-sonic"
    app_desired_count        = 1
    livekit_desired_count    = 2
    livekit_deployment_mode  = "self-hosted"
    allowed_ingress_cidrs    = ["0.0.0.0/0"]
    livekit_udp_port         = 7882
    livekit_turn_enabled     = true
    livekit_turn_udp_enabled = true
    livekit_turn_tls_enabled = false
    log_retention_days       = 14
    bedrock_model_arns       = ["arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-sonic-v1:0"]
  }
}
