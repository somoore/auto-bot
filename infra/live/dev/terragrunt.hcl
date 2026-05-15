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

  app_api_token_secret_arn      = get_env("APP_API_TOKEN_SECRET_ARN", "")
  livekit_api_key_secret_arn    = get_env("LIVEKIT_API_KEY_SECRET_ARN", "")
  livekit_api_secret_secret_arn = get_env("LIVEKIT_API_SECRET_SECRET_ARN", "")
  livekit_config_secret_arn     = get_env("LIVEKIT_CONFIG_SECRET_ARN", "")

  openai_api_key_secret_arn   = get_env("OPENAI_API_KEY_SECRET_ARN", "")
  jira_api_token_secret_arn   = get_env("JIRA_API_TOKEN_SECRET_ARN", "")
  jira_config_json_secret_arn = get_env("JIRA_CONFIG_JSON_SECRET_ARN", "")

  app_base_url            = get_env("APP_BASE_URL", "")
  livekit_url_override    = get_env("LIVEKIT_URL_OVERRIDE", "")
  app_certificate_arn     = get_env("APP_CERTIFICATE_ARN", "")
  livekit_certificate_arn = get_env("LIVEKIT_CERTIFICATE_ARN", "")
})
