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

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This launcher expects macOS Keychain." >&2
  exit 1
fi

APP_TOKEN_ACCOUNT="${AUTO_BOT_APP_TOKEN_ACCOUNT:-$USER}"
OPENAI_ACCOUNT="${AUTO_BOT_OPENAI_ACCOUNT:-$USER}"
JIRA_TOKEN_ACCOUNT="${AUTO_BOT_JIRA_TOKEN_ACCOUNT:-somoore2025@gmail.com}"

export APP_ENV=local
export VOICE_PROVIDER=openai
export OPENAI_REALTIME_MODEL="${OPENAI_REALTIME_MODEL:-gpt-realtime-2}"
export OPENAI_REALTIME_TRANSCRIPTION_MODEL="${OPENAI_REALTIME_TRANSCRIPTION_MODEL:-gpt-realtime-whisper}"
export CONFERENCE_LOOPBACK_ONLY="${CONFERENCE_LOOPBACK_ONLY:-1}"

export APP_API_TOKEN
APP_API_TOKEN="$(keychain_get_required "${AUTO_BOT_APP_TOKEN_SERVICE:-auto-bot/app-api-token}" "$APP_TOKEN_ACCOUNT")"

export OPENAI_API_KEY
OPENAI_API_KEY="$(keychain_get_optional "${AUTO_BOT_OPENAI_SERVICE:-auto-bot/openai-api-key}" "$OPENAI_ACCOUNT")"
if [ -z "$OPENAI_API_KEY" ] && [ -z "${AUTO_BOT_OPENAI_SERVICE:-}" ]; then
  OPENAI_API_KEY="$(keychain_get_optional "argus-openai-api-key" "$OPENAI_ACCOUNT")"
fi
if [ -z "$OPENAI_API_KEY" ] && [ -z "${AUTO_BOT_OPENAI_SERVICE:-}" ]; then
  OPENAI_API_KEY="$(keychain_get_optional "argus-cli" "OPENAI_API_KEY")"
fi
if [ -z "$OPENAI_API_KEY" ] && [ -z "${AUTO_BOT_OPENAI_SERVICE:-}" ]; then
  OPENAI_API_KEY="$(keychain_get_optional "argus-cli" "openai_api_key")"
fi
if [ -z "$OPENAI_API_KEY" ]; then
  keychain_get_required "${AUTO_BOT_OPENAI_SERVICE:-auto-bot/openai-api-key}" "$OPENAI_ACCOUNT" >/dev/null
fi

JIRA_TOKEN="$(keychain_get_optional "${AUTO_BOT_JIRA_TOKEN_SERVICE:-auto-bot/jira-api-token}" "$JIRA_TOKEN_ACCOUNT")"
if [ -n "$JIRA_TOKEN" ]; then
  export JIRA_API_TOKEN="$JIRA_TOKEN"
  if [ -z "${JIRA_CONFIG_PATH:-}" ] && [ -f "$ROOT_DIR/config/jira.local.json" ]; then
    export JIRA_CONFIG_PATH="$ROOT_DIR/config/jira.local.json"
  fi
fi

cd "$ROOT_DIR"
exec go run ./cmd/server/
