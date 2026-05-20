#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

keychain_get_optional() {
  local service="$1"
  local account="$2"
  /usr/bin/security find-generic-password -s "$service" -a "$account" -w 2>/dev/null || true
}

if [ "$(uname -s)" != "Darwin" ]; then
  echo "scripts/local-compose.sh is for local macOS development with Keychain." >&2
  exit 1
fi

APP_TOKEN="$(keychain_get_optional "${AUTO_BOT_APP_TOKEN_SERVICE:-auto-bot/app-api-token}" "${AUTO_BOT_APP_TOKEN_ACCOUNT:-$USER}")"
if [ -z "$APP_TOKEN" ]; then
  echo "Missing local app token. Run scripts/local-up.sh first." >&2
  exit 1
fi
LOCAL_LOGIN_TOKEN="$(keychain_get_optional "${AUTO_BOT_LOCAL_LOGIN_SERVICE:-auto-bot/local-login-token}" "${AUTO_BOT_LOCAL_LOGIN_ACCOUNT:-$USER}")"

export APP_API_TOKEN="$APP_TOKEN"
export APP_LOCAL_LOGIN_TOKEN="$LOCAL_LOGIN_TOKEN"
export COMPOSE_DISABLE_ENV_FILE="${COMPOSE_DISABLE_ENV_FILE:-1}"
export AWS_REGION="${AWS_REGION:-us-east-1}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-$AWS_REGION}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-}"
export AWS_SESSION_TOKEN="${AWS_SESSION_TOKEN:-}"

JIRA_TOKEN="$(keychain_get_optional "${AUTO_BOT_JIRA_TOKEN_SERVICE:-auto-bot/jira-api-token}" "${AUTO_BOT_JIRA_TOKEN_ACCOUNT:-somoore2025@gmail.com}")"
if [ -n "$JIRA_TOKEN" ]; then
  export JIRA_API_TOKEN="$JIRA_TOKEN"
  if [ -z "${JIRA_CONFIG_PATH:-}" ] && [ -f "$ROOT_DIR/config/jira.local.json" ]; then
    export JIRA_CONFIG_PATH="/srv/config/jira.local.json"
  fi
fi

WEBHOOK_SECRET="$(keychain_get_optional "${AUTO_BOT_WEBHOOK_SECRET_SERVICE:-auto-bot/jira-webhook-secret}" "${AUTO_BOT_WEBHOOK_SECRET_ACCOUNT:-$USER}")"
if [ -n "$WEBHOOK_SECRET" ]; then
  export JIRA_WEBHOOK_SECRET="$WEBHOOK_SECRET"
fi

GITHUB_ACCOUNT="${AUTO_BOT_GITHUB_APP_ACCOUNT:-$USER}"
GITHUB_APP_ID_VALUE="$(keychain_get_optional "${AUTO_BOT_GITHUB_APP_ID_SERVICE:-auto-bot/github-app-id}" "$GITHUB_ACCOUNT")"
GITHUB_APP_INSTALLATION_ID_VALUE="$(keychain_get_optional "${AUTO_BOT_GITHUB_APP_INSTALLATION_ID_SERVICE:-auto-bot/github-app-installation-id}" "$GITHUB_ACCOUNT")"
GITHUB_APP_PRIVATE_KEY_VALUE="$(keychain_get_optional "${AUTO_BOT_GITHUB_APP_PRIVATE_KEY_SERVICE:-auto-bot/github-app-private-key}" "$GITHUB_ACCOUNT")"
if [ -n "$GITHUB_APP_ID_VALUE" ]; then export GITHUB_APP_ID="$GITHUB_APP_ID_VALUE"; fi
if [ -n "$GITHUB_APP_INSTALLATION_ID_VALUE" ]; then export GITHUB_APP_INSTALLATION_ID="$GITHUB_APP_INSTALLATION_ID_VALUE"; fi
if [ -n "$GITHUB_APP_PRIVATE_KEY_VALUE" ]; then export GITHUB_APP_PRIVATE_KEY="$GITHUB_APP_PRIVATE_KEY_VALUE"; fi

cd "$ROOT_DIR"
exec docker compose "$@"
