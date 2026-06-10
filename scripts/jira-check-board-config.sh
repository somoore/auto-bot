#!/usr/bin/env bash
set -euo pipefail

CONFIG_PATH="${1:-${JIRA_CONFIG_PATH:-}}"
BOARD_ID="${2:-}"

if [ -z "$CONFIG_PATH" ]; then
  echo "usage: scripts/jira-check-board-config.sh /path/to/jira.json [board-id]" >&2
  echo "or set JIRA_CONFIG_PATH and optionally pass [board-id]" >&2
  exit 64
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to read Jira config JSON" >&2
  exit 69
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to call Jira" >&2
  exit 69
fi

BASE_URL="$(jq -r '.base_url // empty' "$CONFIG_PATH" | sed 's:/*$::')"
EMAIL="$(jq -r '.email // empty' "$CONFIG_PATH")"
TOKEN_FILE="$(jq -r '.api_token_file // empty' "$CONFIG_PATH")"
TOKEN_ENV="$(jq -r '.api_token_env // empty' "$CONFIG_PATH")"
INLINE_TOKEN="$(jq -r '.api_token // empty' "$CONFIG_PATH")"

if [ -z "$BOARD_ID" ]; then
  BOARD_ID="$(jq -r '.board_id // empty' "$CONFIG_PATH")"
fi
if [ -z "$BOARD_ID" ]; then
  BOARD_ID="1"
fi

TOKEN=""
if [ -n "$TOKEN_FILE" ]; then
  TOKEN="$(tr -d '\r\n' < "$TOKEN_FILE")"
elif [ -n "$TOKEN_ENV" ]; then
  TOKEN="${!TOKEN_ENV:-}"
elif [ -n "${JIRA_API_TOKEN:-}" ]; then
  TOKEN="$JIRA_API_TOKEN"
elif [ -n "$INLINE_TOKEN" ]; then
  TOKEN="$INLINE_TOKEN"
fi

if [ -z "$BASE_URL" ] || [ -z "$EMAIL" ] || [ -z "$TOKEN" ]; then
  echo "config must provide base_url, email, and an API token via api_token_file, api_token_env, JIRA_API_TOKEN, or api_token" >&2
  exit 64
fi

TMP_RESPONSE="$(mktemp)"
trap 'rm -f "$TMP_RESPONSE"' EXIT

STATUS="$(
  curl -sS \
    -u "$EMAIL:$TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/rest/agile/1.0/board/$BOARD_ID/configuration" \
    -o "$TMP_RESPONSE" \
    -w "%{http_code}"
)"

if [ "$STATUS" = "200" ]; then
  echo "Jira Agile board configuration access: OK"
  jq -r '
    "board: \(.name) (#\(.id))",
    "columns:",
    (.columnConfig.columns[]? | "  - \(.name): " + ([.statuses[]?.name] | join(", ")))
  ' "$TMP_RESPONSE"
  exit 0
fi

MESSAGE="$(jq -r '.message // (.errorMessages // [] | join("; ")) // empty' "$TMP_RESPONSE" 2>/dev/null || true)"
echo "Jira Agile board configuration access: FAILED ($STATUS)"
if echo "$MESSAGE" | grep -qi "scope does not match"; then
  echo "root cause: Jira Software Agile board APIs rejected this token with a scope-mismatch response."
  echo "if the token already includes read:board-scope:jira-software and read:issue-details:jira, this is an Atlassian scoped-token support gap for /rest/agile/1.0."
  echo "fix for current app sync: use Jira Platform APIs instead; run scripts/jira-validate-workflow-config.sh to validate status and transition mappings."
  echo "fix if Agile board metadata is mandatory: use a classic/unscoped Jira API token on the site URL, or move this path to OAuth/Forge once Atlassian supports the needed token mode."
else
  echo "response:"
  cat "$TMP_RESPONSE"
fi
exit 1
