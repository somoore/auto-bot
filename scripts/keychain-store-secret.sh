#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/keychain-store-secret.sh <service> [account]

Stores one secret in the macOS login Keychain. The secret is read from a
hidden terminal prompt, never from argv.

Examples:
  scripts/keychain-store-secret.sh auto-bot/app-api-token "$USER"
  scripts/keychain-store-secret.sh auto-bot/jira-api-token somoore2025@gmail.com
  scripts/keychain-store-secret.sh auto-bot/openai-api-key "$USER"
USAGE
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  usage
  exit 2
fi

if [ "$(uname -s)" != "Darwin" ]; then
  echo "macOS Keychain is only available on Darwin/macOS." >&2
  exit 1
fi

SERVICE="$1"
ACCOUNT="${2:-$USER}"

if [ -z "$SERVICE" ] || [ -z "$ACCOUNT" ]; then
  usage
  exit 2
fi

printf "Secret for %s (%s): " "$SERVICE" "$ACCOUNT" >&2
IFS= read -r -s SECRET
printf "\n" >&2

if [ -z "$SECRET" ]; then
  echo "Refusing to store an empty secret." >&2
  exit 1
fi

/usr/bin/security add-generic-password \
  -U \
  -s "$SERVICE" \
  -a "$ACCOUNT" \
  -w "$SECRET" >/dev/null

echo "Stored $SERVICE in macOS Keychain for account $ACCOUNT."
