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

  # LiveKit Cloud: required, non-secret.
  livekit_cloud_url = get_env("LIVEKIT_CLOUD_URL", "")

  # Optional app ALB domain/cert.
  hosted_zone_id      = get_env("HOSTED_ZONE_ID", "")
  app_domain_name     = get_env("APP_DOMAIN_NAME", "")
  app_certificate_arn = get_env("APP_CERTIFICATE_ARN", "")
  app_base_url        = get_env("APP_BASE_URL", "")

  # Edge auth: Cognito + Google federation in front of the app.
  auth_domain_name        = get_env("AUTH_DOMAIN_NAME", local.env.locals.inputs.auth_domain_name)
  cognito_domain_prefix   = get_env("COGNITO_DOMAIN_PREFIX", local.env.locals.inputs.cognito_domain_prefix)
  google_client_id        = get_env("GOOGLE_CLIENT_ID", "")
  google_client_secret    = get_env("GOOGLE_CLIENT_SECRET", "")
  host_emails             = get_env("HOST_EMAILS", local.env.locals.inputs.host_emails)
  acm_wait_for_validation = get_env("ACM_WAIT_FOR_VALIDATION", "false") == "true"
  enable_alb_access_logs  = get_env("ENABLE_ALB_ACCESS_LOGS", "false") == "true"
  verbose_logging         = get_env("VERBOSE_LOGGING", "false") == "true"

  # Secrets Manager ARNs (created by scripts/aws-upsert-secrets.sh).
  app_api_token_secret_arn      = get_env("APP_API_TOKEN_SECRET_ARN", "")
  livekit_api_key_secret_arn    = get_env("LIVEKIT_API_KEY_SECRET_ARN", "")
  livekit_api_secret_secret_arn = get_env("LIVEKIT_API_SECRET_SECRET_ARN", "")

  openai_api_key_secret_arn      = get_env("OPENAI_API_KEY_SECRET_ARN", "")
  jira_api_token_secret_arn      = get_env("JIRA_API_TOKEN_SECRET_ARN", "")
  jira_config_json_secret_arn    = get_env("JIRA_CONFIG_JSON_SECRET_ARN", "")
  jira_webhook_secret_secret_arn = get_env("JIRA_WEBHOOK_SECRET_SECRET_ARN", "")

  github_app_id_secret_arn              = get_env("GITHUB_APP_ID_SECRET_ARN", "")
  github_app_installation_id_secret_arn = get_env("GITHUB_APP_INSTALLATION_ID_SECRET_ARN", "")
  github_app_private_key_secret_arn     = get_env("GITHUB_APP_PRIVATE_KEY_SECRET_ARN", "")
  github_default_repo                   = get_env("GITHUB_DEFAULT_REPO", "")
  github_allowed_repos                  = get_env("GITHUB_ALLOWED_REPOS", "")
  github_pr_comments_enabled            = get_env("GITHUB_PR_COMMENTS_ENABLED", "false") == "true"
  agent_pm_model                        = get_env("AGENT_PM_MODEL", "us.anthropic.claude-haiku-4-5-20251001-v1:0")
  agent_review_model                    = get_env("AGENT_REVIEW_MODEL", "us.anthropic.claude-sonnet-4-6")
})
