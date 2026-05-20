#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AWS_PROFILE_NAME="${AWS_PROFILE:-test_AccountA/AdministratorAccess}"
AWS_REGION_VALUE="${AWS_REGION:-us-east-1}"

keychain_get_optional() {
  local service="$1"
  local account="$2"
  /usr/bin/security find-generic-password -s "$service" -a "$account" -w 2>/dev/null || true
}

keychain_store() {
  local service="$1"
  local account="$2"
  local value="$3"
  /usr/bin/security add-generic-password -U -s "$service" -a "$account" -w "$value" >/dev/null
}

ensure_generated_secret() {
  local service="$1"
  local account="$2"
  local value
  value="$(keychain_get_optional "$service" "$account")"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return
  fi
  value="$(openssl rand -hex 32)"
  keychain_store "$service" "$account" "$value"
  printf '%s' "$value"
}

ensure_jira_token() {
  local service="${AUTO_BOT_JIRA_TOKEN_SERVICE:-auto-bot/jira-api-token}"
  local account="${AUTO_BOT_JIRA_TOKEN_ACCOUNT:-somoore2025@gmail.com}"
  local value
  value="$(keychain_get_optional "$service" "$account")"
  if [ -n "$value" ]; then
    return
  fi
  if [ -n "${AUTO_BOT_JIRA_TOKEN:-}" ]; then
    keychain_store "$service" "$account" "$AUTO_BOT_JIRA_TOKEN"
    return
  fi
  if [ -t 0 ]; then
    printf "Jira API token for %s: " "$account" >&2
    IFS= read -r -s value
    printf "\n" >&2
    if [ -z "$value" ]; then
      echo "Refusing to store an empty Jira API token." >&2
      exit 1
    fi
    keychain_store "$service" "$account" "$value"
    return
  fi
  cat >&2 <<EOF
Missing Jira API token in macOS Keychain.
Store it once with:
  scripts/keychain-store-secret.sh "$service" "$account"
EOF
  exit 1
}

wait_for_docker() {
  if docker info >/dev/null 2>&1; then
    return
  fi
  if command -v open >/dev/null 2>&1; then
    open -a Docker >/dev/null 2>&1 || true
  fi
  echo "Waiting for Docker Desktop..." >&2
  for _ in $(seq 1 60); do
    if docker info >/dev/null 2>&1; then
      return
    fi
    sleep 2
  done
  echo "Docker is not responding. Start Docker Desktop and rerun scripts/local-up.sh." >&2
  exit 1
}

build_compose_command() {
  local command
  local quoted
  printf -v command '%q' "$ROOT_DIR/scripts/dc-up-keychain.sh"
  for arg in "$@"; do
    printf -v quoted '%q' "$arg"
    command+=" $quoted"
  done
  printf '%s' "$command"
}

if [ "$(uname -s)" != "Darwin" ]; then
  echo "scripts/local-up.sh is for local macOS development with Keychain." >&2
  exit 1
fi

cd "$ROOT_DIR"

export AWS_PROFILE="$AWS_PROFILE_NAME"
export AWS_REGION="$AWS_REGION_VALUE"
export AWS_DEFAULT_REGION="$AWS_REGION_VALUE"
export COMPOSE_DISABLE_ENV_FILE=1

APP_TOKEN="$(ensure_generated_secret "${AUTO_BOT_APP_TOKEN_SERVICE:-auto-bot/app-api-token}" "${AUTO_BOT_APP_TOKEN_ACCOUNT:-$USER}")"
LOCAL_LOGIN_TOKEN="$(ensure_generated_secret "${AUTO_BOT_LOCAL_LOGIN_SERVICE:-auto-bot/local-login-token}" "${AUTO_BOT_LOCAL_LOGIN_ACCOUNT:-$USER}")"
AWS_REFRESH_TOKEN="$(ensure_generated_secret "${AUTO_BOT_LOCAL_AWS_REFRESH_SERVICE:-auto-bot/local-aws-refresh-token}" "${AUTO_BOT_LOCAL_AWS_REFRESH_ACCOUNT:-$USER}")"
ensure_generated_secret "${AUTO_BOT_WEBHOOK_SECRET_SERVICE:-auto-bot/jira-webhook-secret}" "${AUTO_BOT_WEBHOOK_SECRET_ACCOUNT:-$USER}" >/dev/null
ensure_jira_token

if command -v pbcopy >/dev/null 2>&1; then
  printf '%s' "$APP_TOKEN" | pbcopy
  echo "Fallback app access token copied to clipboard." >&2
fi

wait_for_docker

export AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN="$AWS_REFRESH_TOKEN"
export AUTO_BOT_LOCAL_AWS_REFRESH_PORT="${AUTO_BOT_LOCAL_AWS_REFRESH_PORT:-38751}"
export APP_LOCAL_AWS_REFRESH_URL="${APP_LOCAL_AWS_REFRESH_URL:-http://host.docker.internal:${AUTO_BOT_LOCAL_AWS_REFRESH_PORT}/refresh}"
export APP_LOCAL_AWS_REFRESH_TOKEN="$AWS_REFRESH_TOKEN"
"$ROOT_DIR/scripts/local-aws-refresh-broker.sh" stop >/dev/null 2>&1 || true
"$ROOT_DIR/scripts/local-aws-refresh-broker.sh" start

if [ $# -eq 0 ]; then
  set -- --build -d
fi

COMPOSE_COMMAND="$(build_compose_command "$@")"

if [ "${AUTO_BOT_SKIP_AWS:-}" = "1" ]; then
  "$ROOT_DIR/scripts/dc-up-keychain.sh" "$@"
elif command -v zsh >/dev/null 2>&1; then
  export AUTO_BOT_ASSUME_EXEC="$COMPOSE_COMMAND"
  zsh -lic 'assume --confirm --region "$AWS_REGION" --exec "$AUTO_BOT_ASSUME_EXEC" "$AWS_PROFILE"'
else
  echo "Warning: zsh/assume shell integration was not available; falling back to granted credential-process in dc-up-keychain.sh." >&2
  "$ROOT_DIR/scripts/dc-up-keychain.sh" "$@"
fi

for _ in $(seq 1 60); do
  if curl -fsS http://localhost:3001/healthz >/dev/null 2>&1; then
    echo "Auto Bot is running at http://localhost:3001" >&2
    if [ "${AUTO_BOT_OPEN_BROWSER:-1}" != "0" ]; then
      identity="${AUTO_BOT_LOCAL_IDENTITY:-$USER}"
      open "http://localhost:3001/auth/local-login?token=${LOCAL_LOGIN_TOKEN}&identity=${identity}&next=/%3Fv%3Dlocal-login" >/dev/null 2>&1 || true
    fi
    exit 0
  fi
  sleep 1
done

echo "Stack started, but http://localhost:3001/healthz did not become ready within 60 seconds." >&2
echo "Check logs with: docker compose logs -f app livekit" >&2
exit 1
