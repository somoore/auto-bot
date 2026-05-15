#!/bin/sh
# Compatibility wrapper. Local macOS development should use Keychain-backed
# secrets through dc-up-keychain.sh instead of project .env files.
set -e

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

if [ "$(uname -s)" = "Darwin" ] && [ -z "${APP_API_TOKEN:-}" ]; then
  exec "$ROOT_DIR/scripts/dc-up-keychain.sh" "$@"
fi

CREDS=$(granted credential-process --profile "test_AccountA/AdministratorAccess")

export AWS_ACCESS_KEY_ID=$(echo "$CREDS" | python3 -c "import sys,json; print(json.load(sys.stdin)['AccessKeyId'])")
export AWS_SECRET_ACCESS_KEY=$(echo "$CREDS" | python3 -c "import sys,json; print(json.load(sys.stdin)['SecretAccessKey'])")
export AWS_SESSION_TOKEN=$(echo "$CREDS" | python3 -c "import sys,json; print(json.load(sys.stdin)['SessionToken'])")

exec docker compose up "$@"
