#!/usr/bin/env bash
# Stores the Google OAuth client ID + secret in the macOS Keychain for auto-bot.
# Prompts interactively (hidden input for the secret). Nothing is echoed or
# written to shell history.
set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This script is for macOS Keychain." >&2
  exit 1
fi

ACCOUNT="${USER}"

printf 'Paste the Google OAuth Client ID, then press Enter:\n> '
IFS= read -r CLIENT_ID
CLIENT_ID="$(printf '%s' "$CLIENT_ID" | tr -d '[:space:]')"
if [ -z "$CLIENT_ID" ]; then
  echo "Client ID was empty; nothing stored." >&2
  exit 1
fi

printf 'Paste the Google OAuth Client Secret, then press Enter (input hidden):\n> '
IFS= read -rs CLIENT_SECRET
printf '\n'
CLIENT_SECRET="$(printf '%s' "$CLIENT_SECRET" | tr -d '[:space:]')"
if [ -z "$CLIENT_SECRET" ]; then
  echo "Client Secret was empty; nothing stored." >&2
  exit 1
fi

/usr/bin/security add-generic-password -U -s auto-bot/google-client-id -a "$ACCOUNT" -w "$CLIENT_ID" >/dev/null
/usr/bin/security add-generic-password -U -s auto-bot/google-client-secret -a "$ACCOUNT" -w "$CLIENT_SECRET" >/dev/null

echo "Stored auto-bot/google-client-id (${#CLIENT_ID} chars) and auto-bot/google-client-secret (${#CLIENT_SECRET} chars) for $ACCOUNT."
