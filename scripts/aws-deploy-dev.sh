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

cd "$TG_DIR"
terragrunt apply "$@"
