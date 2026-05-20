#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${AUTO_BOT_BASE_URL:-http://localhost:3001}"
TOKEN="${AUTO_BOT_ACCESS_TOKEN:-${APP_API_TOKEN:-}}"

if [ -z "$TOKEN" ]; then
  printf 'AUTO_BOT_ACCESS_TOKEN or APP_API_TOKEN is required for authenticated preflight\n' >&2
  exit 2
fi

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

curl_json() {
  local path="$1"
  local out="$tmpdir/$(echo "$path" | tr '/?' '__').json"
  curl --fail --silent --show-error \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL$path" > "$out"
  if ! python3 - "$path" "$out" <<'PY'
import json
import sys

path, output_path = sys.argv[1], sys.argv[2]
raw = open(output_path, encoding="utf-8").read()
try:
    json.loads(raw)
except json.JSONDecodeError as exc:
    print(f"{path} did not return JSON: {exc}", file=sys.stderr)
    print(raw[:500], file=sys.stderr)
    raise SystemExit(1)
PY
  then
    return 1
  fi
  printf '%s\n' "$out"
}

printf 'checking %s\n' "$BASE_URL"
curl --fail --silent --show-error "$BASE_URL/healthz" >/dev/null

setup_json="$(curl_json /setup/status)" || exit $?
voice_json="$(curl_json /voice/providers)" || exit $?
workspace_json="$(curl_json /workspace/status)" || exit $?
identity_json="$(curl_json /identity/status)" || exit $?
observability_json="$(curl_json /observability/status)" || exit $?

python3 - "$setup_json" "$voice_json" "$workspace_json" "$identity_json" "$observability_json" <<'PY'
import json
import sys

setup, voice, workspace, identity, observability = [json.load(open(path, encoding="utf-8")) for path in sys.argv[1:]]

def require(condition, message):
    if not condition:
        raise SystemExit(message)

require(setup.get("ok") is True, "setup status did not return ok=true")
require(voice.get("ok") is True and voice.get("providers"), "voice providers are not registered")
require(workspace.get("ok") is True, "workspace status did not return ok=true")
require(workspace.get("workspace", {}).get("workspace_id"), "workspace_id is missing")
require(identity.get("ok") is True, "identity status did not return ok=true")
require(observability.get("ok") is True, "observability status did not return ok=true")

checks = setup.get("setup", {}).get("checks", [])
required = {item.get("name"): item for item in checks if item.get("required")}
missing = [name for name, item in required.items() if not item.get("ok")]
if missing:
    print("required setup checks currently not ready: " + ", ".join(missing))

print("golden demo preflight passed")
PY
