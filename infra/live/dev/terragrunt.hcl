include "root" {
  path = find_in_parent_folders("terragrunt.hcl")
}

terraform {
  source = "../../modules/auto-bot"
}

locals {
  env = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

inputs = merge(local.env.locals.inputs, {
  app_image = get_env("APP_IMAGE", "")

  fck_nat_ami_id = get_env("FCK_NAT_AMI_ID", "")

  livekit_deployment_mode  = get_env("LIVEKIT_DEPLOYMENT_MODE", local.env.locals.inputs.livekit_deployment_mode)
  livekit_cloud_url        = get_env("LIVEKIT_CLOUD_URL", "")
  livekit_url_override     = get_env("LIVEKIT_URL_OVERRIDE", "")
  livekit_domain_name      = get_env("LIVEKIT_DOMAIN_NAME", "")
  livekit_turn_domain_name = get_env("LIVEKIT_TURN_DOMAIN_NAME", "")
  hosted_zone_id           = get_env("HOSTED_ZONE_ID", "")
  app_domain_name          = get_env("APP_DOMAIN_NAME", "")

  app_api_token_secret_arn      = get_env("APP_API_TOKEN_SECRET_ARN", "")
  livekit_api_key_secret_arn    = get_env("LIVEKIT_API_KEY_SECRET_ARN", "")
  livekit_api_secret_secret_arn = get_env("LIVEKIT_API_SECRET_SECRET_ARN", "")
  livekit_config_secret_arn     = get_env("LIVEKIT_CONFIG_SECRET_ARN", "")
  livekit_keys_secret_arn       = get_env("LIVEKIT_KEYS_SECRET_ARN", "")

  openai_api_key_secret_arn      = get_env("OPENAI_API_KEY_SECRET_ARN", "")
  jira_api_token_secret_arn      = get_env("JIRA_API_TOKEN_SECRET_ARN", "")
  jira_config_json_secret_arn    = get_env("JIRA_CONFIG_JSON_SECRET_ARN", "")
  jira_webhook_secret_secret_arn = get_env("JIRA_WEBHOOK_SECRET_SECRET_ARN", "")

  app_base_url                 = get_env("APP_BASE_URL", "")
  app_certificate_arn          = get_env("APP_CERTIFICATE_ARN", "")
  livekit_certificate_arn      = get_env("LIVEKIT_CERTIFICATE_ARN", "")
  livekit_turn_certificate_arn = get_env("LIVEKIT_TURN_CERTIFICATE_ARN", "")
})
