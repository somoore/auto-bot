locals {
  name_prefix = "auto-bot-dev"

  inputs = {
    aws_region            = "us-east-1"
    name_prefix           = local.name_prefix
    voice_provider        = "nova-sonic"
    app_desired_count     = 1
    livekit_desired_count = 1
    allowed_ingress_cidrs = ["0.0.0.0/0"]
    livekit_udp_port      = 7882
    log_retention_days    = 14
    bedrock_model_arns    = ["*"]
  }
}
