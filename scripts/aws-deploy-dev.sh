#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${ENV_FILE:-.env.aws.local}"
TG_DIR="${TG_DIR:-infra/live/dev}"

if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

# Supply-chain gate: verify the cosign signature on APP_IMAGE before applying,
# so EVERY apply path enforces the signature (not just aws-app.sh deploy). ECS
# has no native cosign admission, so this script is the chokepoint.
if [ -n "${APP_IMAGE:-}" ] && command -v cosign >/dev/null 2>&1; then
  KMS_ARN="$(cd "$TG_DIR" && terragrunt output -raw cosign_kms_key_arn 2>/dev/null || true)"
  if [ -n "$KMS_ARN" ]; then
    echo "Verifying cosign signature on $APP_IMAGE ..."
    if ! cosign verify --key "awskms:///${KMS_ARN}" "$APP_IMAGE" >/dev/null 2>&1; then
      echo "ERROR: cosign signature verification failed for $APP_IMAGE; refusing to deploy." >&2
      exit 1
    fi
    echo "cosign signature verified."
  fi
fi

cd "$TG_DIR"
terragrunt apply "$@"
