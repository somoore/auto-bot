#!/usr/bin/env bash
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"
NAME_PREFIX="${NAME_PREFIX:-auto-bot-dev}"
ENV_FILE="${ENV_FILE:-.env.aws.local}"

random_hex() {
  openssl rand -hex "${1:-24}"
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
LIVEKIT_API_KEY_VALUE="${LIVEKIT_API_KEY:-lk_$(random_hex 8)}"
LIVEKIT_API_SECRET_VALUE="${LIVEKIT_API_SECRET:-$(random_hex 32)}"
LIVEKIT_SIGNAL_PORT="${LIVEKIT_SIGNAL_PORT:-7880}"
LIVEKIT_TCP_PORT="${LIVEKIT_TCP_PORT:-7881}"
LIVEKIT_UDP_PORT="${LIVEKIT_UDP_PORT:-7882}"

read -r -d '' LIVEKIT_CONFIG_VALUE <<EOF || true
port: ${LIVEKIT_SIGNAL_PORT}
bind_addresses:
  - ""
rtc:
  tcp_port: ${LIVEKIT_TCP_PORT}
  udp_port: ${LIVEKIT_UDP_PORT}
  use_external_ip: true
keys:
  ${LIVEKIT_API_KEY_VALUE}: ${LIVEKIT_API_SECRET_VALUE}
logging:
  json: true
EOF

APP_API_TOKEN_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/app-api-token" "$APP_API_TOKEN_VALUE")"
LIVEKIT_API_KEY_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/livekit-api-key" "$LIVEKIT_API_KEY_VALUE")"
LIVEKIT_API_SECRET_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/livekit-api-secret" "$LIVEKIT_API_SECRET_VALUE")"
LIVEKIT_CONFIG_SECRET_ARN="$(upsert_secret "${NAME_PREFIX}/livekit-config" "$LIVEKIT_CONFIG_VALUE")"

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

cat > "$ENV_FILE" <<EOF
export AWS_REGION='$REGION'
export APP_API_TOKEN_SECRET_ARN='$APP_API_TOKEN_SECRET_ARN'
export LIVEKIT_API_KEY_SECRET_ARN='$LIVEKIT_API_KEY_SECRET_ARN'
export LIVEKIT_API_SECRET_SECRET_ARN='$LIVEKIT_API_SECRET_SECRET_ARN'
export LIVEKIT_CONFIG_SECRET_ARN='$LIVEKIT_CONFIG_SECRET_ARN'
export OPENAI_API_KEY_SECRET_ARN='$OPENAI_API_KEY_SECRET_ARN'
export JIRA_API_TOKEN_SECRET_ARN='$JIRA_API_TOKEN_SECRET_ARN'
export JIRA_CONFIG_JSON_SECRET_ARN='$JIRA_CONFIG_JSON_SECRET_ARN'
EOF

printf 'Wrote Terraform/Terragrunt secret environment exports to %s\n' "$ENV_FILE"
printf 'Source it before running Terragrunt:\n'
printf '  set -a; source %s; set +a\n' "$ENV_FILE"
