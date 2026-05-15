#!/usr/bin/env bash
set -euo pipefail

CONFIG_PATH="${1:-${JIRA_CONFIG_PATH:-}}"
ISSUE_KEY="${2:-}"

if [ -z "$CONFIG_PATH" ]; then
  echo "usage: scripts/jira-validate-workflow-config.sh /path/to/jira.json [issue-key]" >&2
  echo "or set JIRA_CONFIG_PATH and optionally pass [issue-key]" >&2
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
PROJECT_KEY="$(jq -r '.project_key // empty' "$CONFIG_PATH")"
TOKEN_FILE="$(jq -r '.api_token_file // empty' "$CONFIG_PATH")"
TOKEN_ENV="$(jq -r '.api_token_env // empty' "$CONFIG_PATH")"
INLINE_TOKEN="$(jq -r '.api_token // empty' "$CONFIG_PATH")"

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

if [ -z "$BASE_URL" ] || [ -z "$EMAIL" ] || [ -z "$PROJECT_KEY" ] || [ -z "$TOKEN" ]; then
  echo "config must provide base_url, email, project_key, and an API token via api_token_file, api_token_env, JIRA_API_TOKEN, or api_token" >&2
  exit 64
fi
if [ -n "$ISSUE_KEY" ] && [[ "${ISSUE_KEY%%-*}" != "$PROJECT_KEY" ]]; then
  echo "Refusing to validate issue $ISSUE_KEY because it is outside configured project $PROJECT_KEY." >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

STATUS_CODE="$(
  curl -sS \
    -u "$EMAIL:$TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/rest/api/3/project/$PROJECT_KEY/statuses" \
    -o "$TMP_DIR/statuses.json" \
    -w "%{http_code}"
)"
if [ "$STATUS_CODE" != "200" ]; then
  echo "Jira project status metadata failed: HTTP $STATUS_CODE" >&2
  cat "$TMP_DIR/statuses.json" >&2
  exit 1
fi

MISSING_STATUSES=()
while IFS= read -r JIRA_STATUS; do
  if ! jq -e --arg name "$JIRA_STATUS" '[.[].statuses[]?.name] | index($name) != null' "$TMP_DIR/statuses.json" >/dev/null; then
    MISSING_STATUSES+=("$JIRA_STATUS")
  fi
done < <(jq -r '.status_mappings // {} | keys[]' "$CONFIG_PATH")

if [ "${#MISSING_STATUSES[@]}" -gt 0 ]; then
  echo "Configured Jira status mapping(s) missing from project $PROJECT_KEY: ${MISSING_STATUSES[*]}" >&2
  exit 1
fi

if [ -z "$ISSUE_KEY" ]; then
  SEARCH_BODY="$(jq -n --arg jql "project = $PROJECT_KEY ORDER BY updated DESC" '{jql:$jql,maxResults:1,fields:["summary","status"]}')"
  STATUS_CODE="$(
    curl -sS \
      -u "$EMAIL:$TOKEN" \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      -X POST \
      --data "$SEARCH_BODY" \
      "$BASE_URL/rest/api/3/search/jql" \
      -o "$TMP_DIR/search.json" \
      -w "%{http_code}"
  )"
  if [ "$STATUS_CODE" != "200" ]; then
    echo "Jira issue search failed: HTTP $STATUS_CODE" >&2
    cat "$TMP_DIR/search.json" >&2
    exit 1
  fi
  ISSUE_KEY="$(jq -r '.issues[0].key // empty' "$TMP_DIR/search.json")"
fi

if [ -z "$ISSUE_KEY" ]; then
  echo "No issue found in project $PROJECT_KEY; pass a known issue key to validate transitions." >&2
  exit 1
fi
if [[ "${ISSUE_KEY%%-*}" != "$PROJECT_KEY" ]]; then
  echo "Refusing to validate issue $ISSUE_KEY because it is outside configured project $PROJECT_KEY." >&2
  exit 1
fi

STATUS_CODE="$(
  curl -sS \
    -u "$EMAIL:$TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/rest/api/3/issue/$ISSUE_KEY/transitions" \
    -o "$TMP_DIR/transitions.json" \
    -w "%{http_code}"
)"
if [ "$STATUS_CODE" != "200" ]; then
  echo "Jira issue transition metadata failed for $ISSUE_KEY: HTTP $STATUS_CODE" >&2
  cat "$TMP_DIR/transitions.json" >&2
  exit 1
fi

MISSING_TRANSITIONS=()
while IFS= read -r TRANSITION; do
  NAME="${TRANSITION%%	*}"
  ID="${TRANSITION#*	}"
  if ! jq -e --arg id "$ID" '.transitions | map(.id) | index($id) != null' "$TMP_DIR/transitions.json" >/dev/null; then
    MISSING_TRANSITIONS+=("$NAME=$ID")
  fi
done < <(
  jq -r '
    (.transitions // {} | to_entries[] | select(.key != "Deleted") | [.key, .value] | @tsv),
    (select(.delete_transition? != null) | ["delete_transition", .delete_transition] | @tsv)
  ' "$CONFIG_PATH"
)

if [ "${#MISSING_TRANSITIONS[@]}" -gt 0 ]; then
  echo "Configured transition(s) not currently available from $ISSUE_KEY: ${MISSING_TRANSITIONS[*]}" >&2
  echo "If this workflow has conditional transitions, validate again with an issue currently in the source status for each transition." >&2
  exit 1
fi

echo "Jira workflow config validation: OK"
echo "project: $PROJECT_KEY"
echo "transition sample issue: $ISSUE_KEY"
echo "configured statuses:"
jq -r '.status_mappings // {} | to_entries[] | "  - \(.key) -> \(.value)"' "$CONFIG_PATH"
echo "configured transitions available from $ISSUE_KEY:"
jq -r '.transitions // {} | to_entries[] | "  - \(.key) -> \(.value)"' "$CONFIG_PATH"
