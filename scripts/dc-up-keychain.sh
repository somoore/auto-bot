#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

keychain_get_optional() {
  local service="$1"
  local account="$2"
  /usr/bin/security find-generic-password -s "$service" -a "$account" -w 2>/dev/null || true
}

keychain_get_required() {
  local service="$1"
  local account="$2"
  local value
  value="$(keychain_get_optional "$service" "$account")"
  if [ -z "$value" ]; then
    cat >&2 <<EOF
Missing macOS Keychain secret:
  service: $service
  account: $account

Store it with:
  scripts/keychain-store-secret.sh "$service" "$account"
EOF
    exit 1
  fi
  printf '%s' "$value"
}

resolve_aws_credentials() {
  if [ "${AUTO_BOT_SKIP_AWS:-}" = "1" ]; then
    export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-}"
    export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-}"
    export AWS_SESSION_TOKEN="${AWS_SESSION_TOKEN:-}"
    return
  fi

  if [ -n "${AWS_ACCESS_KEY_ID:-}" ] && [ -n "${AWS_SECRET_ACCESS_KEY:-}" ]; then
    export AWS_SESSION_TOKEN="${AWS_SESSION_TOKEN:-}"
    return
  fi

  if command -v granted >/dev/null 2>&1; then
    local profile="${AWS_PROFILE:-test_AccountA/AdministratorAccess}"
    local creds
    creds="$(granted credential-process --profile "$profile")"
    export AWS_ACCESS_KEY_ID
    AWS_ACCESS_KEY_ID="$(printf '%s' "$creds" | python3 -c "import sys,json; print(json.load(sys.stdin)['AccessKeyId'])")"
    export AWS_SECRET_ACCESS_KEY
    AWS_SECRET_ACCESS_KEY="$(printf '%s' "$creds" | python3 -c "import sys,json; print(json.load(sys.stdin)['SecretAccessKey'])")"
    export AWS_SESSION_TOKEN
    AWS_SESSION_TOKEN="$(printf '%s' "$creds" | python3 -c "import sys,json; print(json.load(sys.stdin).get('SessionToken',''))")"
    return
  fi

  echo "Warning: AWS credentials were not found and granted is not installed; Nova Sonic will not connect." >&2
  export AWS_ACCESS_KEY_ID=""
  export AWS_SECRET_ACCESS_KEY=""
  export AWS_SESSION_TOKEN=""
}

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This launcher expects macOS Keychain. Use docker compose with exported secrets on non-macOS hosts." >&2
  exit 1
fi

APP_TOKEN_ACCOUNT="${AUTO_BOT_APP_TOKEN_ACCOUNT:-$USER}"
LOCAL_LOGIN_ACCOUNT="${AUTO_BOT_LOCAL_LOGIN_ACCOUNT:-$USER}"
JIRA_TOKEN_ACCOUNT="${AUTO_BOT_JIRA_TOKEN_ACCOUNT:-somoore2025@gmail.com}"
WEBHOOK_SECRET_ACCOUNT="${AUTO_BOT_WEBHOOK_SECRET_ACCOUNT:-$USER}"
GITHUB_ACCOUNT="${AUTO_BOT_GITHUB_APP_ACCOUNT:-$USER}"
OPENAI_ACCOUNT="${AUTO_BOT_OPENAI_ACCOUNT:-$USER}"

export AWS_REGION="${AWS_REGION:-us-east-1}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-$AWS_REGION}"
export APP_API_TOKEN
APP_API_TOKEN="$(keychain_get_required "${AUTO_BOT_APP_TOKEN_SERVICE:-auto-bot/app-api-token}" "$APP_TOKEN_ACCOUNT")"
export APP_LOCAL_LOGIN_TOKEN
APP_LOCAL_LOGIN_TOKEN="$(keychain_get_required "${AUTO_BOT_LOCAL_LOGIN_SERVICE:-auto-bot/local-login-token}" "$LOCAL_LOGIN_ACCOUNT")"

# MCP signing keys (#58). Auto-mint on first run and stash in Keychain so
# operators never paste secrets into shell history or .env files. Both
# cmd/server (issuer) and cmd/mcpd (verifier) read the same env var.
# Format matches internal/mcp.ParseSigningKeys: "kid1:base64-32-byte-key".
MCP_SIGNING_KEYS_VALUE="$(keychain_get_optional "${AUTO_BOT_MCP_SIGNING_KEYS_SERVICE:-auto-bot/mcp-signing-keys}" "$APP_TOKEN_ACCOUNT")"
if [ -z "$MCP_SIGNING_KEYS_VALUE" ]; then
  MCP_SIGNING_KEYS_VALUE="k1:$(openssl rand -base64 32)"
  /usr/bin/security add-generic-password -U \
    -s "${AUTO_BOT_MCP_SIGNING_KEYS_SERVICE:-auto-bot/mcp-signing-keys}" \
    -a "$APP_TOKEN_ACCOUNT" \
    -w "$MCP_SIGNING_KEYS_VALUE" >/dev/null
fi
export MCP_SIGNING_KEYS="$MCP_SIGNING_KEYS_VALUE"

export COMPOSE_DISABLE_ENV_FILE="${COMPOSE_DISABLE_ENV_FILE:-1}"

OPENAI_KEY="$(keychain_get_optional "${AUTO_BOT_OPENAI_SERVICE:-auto-bot/openai-api-key}" "$OPENAI_ACCOUNT")"
if [ -z "$OPENAI_KEY" ] && [ -z "${AUTO_BOT_OPENAI_SERVICE:-}" ]; then
  OPENAI_KEY="$(keychain_get_optional "argus-openai-api-key" "$OPENAI_ACCOUNT")"
fi
if [ -z "$OPENAI_KEY" ] && [ -z "${AUTO_BOT_OPENAI_SERVICE:-}" ]; then
  OPENAI_KEY="$(keychain_get_optional "argus-cli" "OPENAI_API_KEY")"
fi
if [ -z "$OPENAI_KEY" ] && [ -z "${AUTO_BOT_OPENAI_SERVICE:-}" ]; then
  OPENAI_KEY="$(keychain_get_optional "argus-cli" "openai_api_key")"
fi
if [ -n "$OPENAI_KEY" ]; then
  export OPENAI_API_KEY="$OPENAI_KEY"
fi
export OPENAI_REALTIME_MODEL="${OPENAI_REALTIME_MODEL:-gpt-realtime-2}"
export OPENAI_REALTIME_TRANSCRIPTION_MODEL="${OPENAI_REALTIME_TRANSCRIPTION_MODEL:-gpt-realtime-whisper}"
export OPENAI_REALTIME_TRANSLATION_MODEL="${OPENAI_REALTIME_TRANSLATION_MODEL:-gpt-realtime-translate}"
export OPENAI_REALTIME_TRANSLATION_TARGET_LANGUAGE="${OPENAI_REALTIME_TRANSLATION_TARGET_LANGUAGE:-en}"

JIRA_TOKEN="$(keychain_get_optional "${AUTO_BOT_JIRA_TOKEN_SERVICE:-auto-bot/jira-api-token}" "$JIRA_TOKEN_ACCOUNT")"
if [ -n "$JIRA_TOKEN" ]; then
  export JIRA_API_TOKEN="$JIRA_TOKEN"
  if [ -z "${JIRA_CONFIG_PATH:-}" ] && [ -f "$ROOT_DIR/config/jira.local.json" ]; then
    export JIRA_CONFIG_PATH="/srv/config/jira.local.json"
  fi
fi

WEBHOOK_SECRET="$(keychain_get_optional "${AUTO_BOT_WEBHOOK_SECRET_SERVICE:-auto-bot/jira-webhook-secret}" "$WEBHOOK_SECRET_ACCOUNT")"
if [ -n "$WEBHOOK_SECRET" ]; then
  export JIRA_WEBHOOK_SECRET="$WEBHOOK_SECRET"
fi

GITHUB_APP_ID_VALUE="$(keychain_get_optional "${AUTO_BOT_GITHUB_APP_ID_SERVICE:-auto-bot/github-app-id}" "$GITHUB_ACCOUNT")"
GITHUB_APP_INSTALLATION_ID_VALUE="$(keychain_get_optional "${AUTO_BOT_GITHUB_APP_INSTALLATION_ID_SERVICE:-auto-bot/github-app-installation-id}" "$GITHUB_ACCOUNT")"
GITHUB_APP_PRIVATE_KEY_VALUE="$(keychain_get_optional "${AUTO_BOT_GITHUB_APP_PRIVATE_KEY_SERVICE:-auto-bot/github-app-private-key}" "$GITHUB_ACCOUNT")"
if [ -n "$GITHUB_APP_ID_VALUE" ]; then export GITHUB_APP_ID="$GITHUB_APP_ID_VALUE"; fi
if [ -n "$GITHUB_APP_INSTALLATION_ID_VALUE" ]; then export GITHUB_APP_INSTALLATION_ID="$GITHUB_APP_INSTALLATION_ID_VALUE"; fi
if [ -n "$GITHUB_APP_PRIVATE_KEY_VALUE" ]; then export GITHUB_APP_PRIVATE_KEY="$GITHUB_APP_PRIVATE_KEY_VALUE"; fi

resolve_aws_credentials

cd "$ROOT_DIR"
exec docker compose up "$@"
