#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${AUTO_BOT_BASE_URL:-http://localhost:3001}"
TOKEN="${AUTO_BOT_ACCESS_TOKEN:-${APP_API_TOKEN:-}}"

if [ -z "$TOKEN" ]; then
  printf 'AUTO_BOT_ACCESS_TOKEN or APP_API_TOKEN is required\n' >&2
  exit 2
fi

voice_status="$(mktemp)"
cleanup() { rm -f "$voice_status"; }
trap cleanup EXIT

curl --fail --silent --show-error \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/json" \
  "$BASE_URL/voice/status" > "$voice_status"

python3 - "$voice_status" <<'PY'
import json
import sys

status = json.load(open(sys.argv[1], encoding="utf-8"))
if not status.get("ok"):
    raise SystemExit("voice status is not ready: " + status.get("message", "unknown"))
print("voice status ready")
PY

cat <<'EOF'
Manual proof steps, using the real stack:
1. Open the host browser and create a meeting code.
2. Join with 2-4 participant browsers/devices using that exact code.
3. Validate interruption: one participant interrupts the agent; agent recovers.
4. Validate overlap: two participants speak briefly at once; transcript remains usable.
5. Validate silence: 30 seconds of silence does not end the session.
6. Validate reconnect: one participant refreshes and rejoins.
7. Validate late join: a participant joins after the standup starts.
8. Validate risky Jira confirmation: assignment/ETA/priority asks for confirmation.
9. Validate replay: audit panel shows speech -> tool -> API result.

Record pass/fail evidence in evaluation/aws-livekit-hardening-proof.md.
EOF
