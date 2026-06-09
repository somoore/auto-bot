#!/usr/bin/env bash
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"
NAME_PREFIX="${NAME_PREFIX:-auto-bot-dev}"
ENV_FILE="${ENV_FILE:-.env.aws.local}"

random_hex() {
  openssl rand -hex "${1:-24}"
}

write_export() {
  local key="$1"
  local value="$2"
  printf 'export %s=%q\n' "$key" "$value"
}

upsert_secret() {
  name="$1"
  value="$2"

  if arn=$(aws secretsmanager describe-secret \
    --region "$REGION" \
    --secret-id "$name" \
    --query ARN \
    --output text 2>/dev/null); then
    aws secretsmanager put-secret-value \
      --region "$REGION" \
      --secret-id "$name" \
      --secret-string "$value" >/dev/null
    printf '%s\n' "$arn"
  else
    aws secretsmanager create-secret \
      --region "$REGION" \
      --name "$name" \
      --secret-string "$value" \
      --query ARN \
      --output text
  fi
}

APP_API_TOKEN_VALUE="${APP_API_TOKEN:-$(random_hex 32)}"

# LiveKit Cloud project credentials. These must match the real LiveKit Cloud
# project, so we do NOT auto-generate them. Fall back to a clear placeholder if
# unset (keeping the script non-fatal) and warn the user.
if [ "${LIVEKIT_API_KEY:-}" = "" ]; then
  printf 'WARNING: LIVEKIT_API_KEY is unset. Using a placeholder.\n' >&2
  printf '         Set it to your LiveKit Cloud project API key before deploy.\n' >&2
  LIVEKIT_API_KEY_VALUE="REPLACE_WITH_LIVEKIT_CLOUD_API_KEY"
else
  LIVEKIT_API_KEY_VALUE="$LIVEKIT_API_KEY"
fi

if [ "${LIVEKIT_API_SECRET:-}" = "" ]; then
  printf 'WARNING: LIVEKIT_API_SECRET is unset. Using a placeholder.\n' >&2
  printf '         Set it to your LiveKit Cloud project API secret before deploy.\n' >&2
  LIVEKIT_API_SECRET_VALUE="REPLACE_WITH_LIVEKIT_CLOUD_API_SECRET"
else
  LIVEKIT_API_SECRET_VALUE="$LIVEKIT_API_SECRET"
fi

LIVEKIT_CLOUD_URL_VALUE="${LIVEKIT_CLOUD_URL:-}"
if [ "$LIVEKIT_CLOUD_URL_VALUE" = "" ]; then
  printf 'WARNING: LIVEKIT_CLOUD_URL is unset. LiveKit Cloud mode requires it\n' >&2
  printf '         (e.g. wss://your-project.livekit.cloud) before deploy.\n' >&2
fi

HOSTED_ZONE_ID_VALUE="${HOSTED_ZONE_ID:-}"
APP_DOMAIN_NAME_VALUE="${APP_DOMAIN_NAME:-}"
APP_CERTIFICATE_ARN_VALUE="${APP_CERTIFICATE_ARN:-}"

APP_API_TOKEN_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/app-api-token" "$APP_API_TOKEN_VALUE")"
LIVEKIT_API_KEY_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/livekit-api-key" "$LIVEKIT_API_KEY_VALUE")"
LIVEKIT_API_SECRET_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/livekit-api-secret" "$LIVEKIT_API_SECRET_VALUE")"

OPENAI_API_KEY_SECRET_ARN=""
if [ "${OPENAI_API_KEY:-}" != "" ]; then
  OPENAI_API_KEY_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/openai-api-key" "$OPENAI_API_KEY")"
fi

JIRA_API_TOKEN_SECRET_ARN=""
if [ "${JIRA_API_TOKEN:-}" != "" ]; then
  JIRA_API_TOKEN_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/jira-api-token" "$JIRA_API_TOKEN")"
fi

JIRA_CONFIG_JSON_VALUE="${JIRA_CONFIG_JSON:-}"
if [ "${JIRA_CONFIG_JSON_FILE:-}" != "" ]; then
  JIRA_CONFIG_JSON_VALUE="$(cat "$JIRA_CONFIG_JSON_FILE")"
fi

JIRA_CONFIG_JSON_SECRET_ARN=""
if [ "$JIRA_CONFIG_JSON_VALUE" != "" ]; then
  JIRA_CONFIG_JSON_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/jira-config-json" "$JIRA_CONFIG_JSON_VALUE")"
fi

JIRA_WEBHOOK_SECRET_SECRET_ARN=""
if [ "${JIRA_WEBHOOK_SECRET:-}" != "" ]; then
  JIRA_WEBHOOK_SECRET_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/jira-webhook-secret" "$JIRA_WEBHOOK_SECRET")"
fi

GITHUB_APP_ID_SECRET_ARN=""
if [ "${GITHUB_APP_ID:-}" != "" ]; then
  GITHUB_APP_ID_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/github-app-id" "$GITHUB_APP_ID")"
fi

GITHUB_APP_INSTALLATION_ID_SECRET_ARN=""
if [ "${GITHUB_APP_INSTALLATION_ID:-}" != "" ]; then
  GITHUB_APP_INSTALLATION_ID_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/github-app-installation-id" "$GITHUB_APP_INSTALLATION_ID")"
fi

GITHUB_APP_PRIVATE_KEY_SECRET_ARN=""
if [ "${GITHUB_APP_PRIVATE_KEY:-}" != "" ]; then
  GITHUB_APP_PRIVATE_KEY_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/github-app-private-key" "$GITHUB_APP_PRIVATE_KEY")"
fi

{
  write_export AWS_REGION "$REGION"
  write_export APP_API_TOKEN_SECRET_ARN "$APP_API_TOKEN_SECRET_ARN"
  write_export LIVEKIT_API_KEY_SECRET_ARN "$LIVEKIT_API_KEY_SECRET_ARN"
  write_export LIVEKIT_API_SECRET_SECRET_ARN "$LIVEKIT_API_SECRET_SECRET_ARN"
  write_export OPENAI_API_KEY_SECRET_ARN "$OPENAI_API_KEY_SECRET_ARN"
  write_export JIRA_API_TOKEN_SECRET_ARN "$JIRA_API_TOKEN_SECRET_ARN"
  write_export JIRA_CONFIG_JSON_SECRET_ARN "$JIRA_CONFIG_JSON_SECRET_ARN"
  write_export JIRA_WEBHOOK_SECRET_SECRET_ARN "$JIRA_WEBHOOK_SECRET_SECRET_ARN"
  write_export GITHUB_APP_ID_SECRET_ARN "$GITHUB_APP_ID_SECRET_ARN"
  write_export GITHUB_APP_INSTALLATION_ID_SECRET_ARN "$GITHUB_APP_INSTALLATION_ID_SECRET_ARN"
  write_export GITHUB_APP_PRIVATE_KEY_SECRET_ARN "$GITHUB_APP_PRIVATE_KEY_SECRET_ARN"
  write_export GITHUB_DEFAULT_REPO "${GITHUB_DEFAULT_REPO:-}"
  write_export GITHUB_ALLOWED_REPOS "${GITHUB_ALLOWED_REPOS:-}"
  write_export GITHUB_PR_COMMENTS_ENABLED "${GITHUB_PR_COMMENTS_ENABLED:-false}"
  write_export AGENT_PM_MODEL "${AGENT_PM_MODEL:-us.anthropic.claude-haiku-4-5-20251001-v1:0}"
  write_export AGENT_REVIEW_MODEL "${AGENT_REVIEW_MODEL:-us.anthropic.claude-sonnet-4-6}"
  write_export LIVEKIT_CLOUD_URL "$LIVEKIT_CLOUD_URL_VALUE"
  write_export HOSTED_ZONE_ID "$HOSTED_ZONE_ID_VALUE"
  write_export APP_DOMAIN_NAME "$APP_DOMAIN_NAME_VALUE"
  write_export APP_CERTIFICATE_ARN "$APP_CERTIFICATE_ARN_VALUE"
} > "$ENV_FILE"

printf 'Wrote Terraform/Terragrunt secret environment exports to %s\n' "$ENV_FILE"
printf 'Source it before running Terragrunt:\n'
printf '  set -a; source %s; set +a\n' "$ENV_FILE"
