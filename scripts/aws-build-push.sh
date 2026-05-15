#!/usr/bin/env bash
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"
TG_DIR="${TG_DIR:-infra/live/dev}"
TAG="${TAG:-latest}"
ENV_FILE="${ENV_FILE:-.env.aws.local}"

if ! command -v terragrunt >/dev/null 2>&1; then
  echo "terragrunt is required" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

REPO_URL="$(cd "$TG_DIR" && terragrunt output -raw ecr_repository_url)"
REGISTRY="${REPO_URL%/*}"
IMAGE="${REPO_URL}:${TAG}"

aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "$REGISTRY"
docker build -t "$IMAGE" .
docker push "$IMAGE"

if [ -f "$ENV_FILE" ]; then
  if grep -q '^export APP_IMAGE=' "$ENV_FILE"; then
    perl -0pi -e "s#^export APP_IMAGE=.*\$#export APP_IMAGE='$IMAGE'#m" "$ENV_FILE"
  else
    printf "export APP_IMAGE='%s'\n" "$IMAGE" >> "$ENV_FILE"
  fi
else
  printf "export APP_IMAGE='%s'\n" "$IMAGE" > "$ENV_FILE"
fi

printf 'Pushed %s\n' "$IMAGE"
printf 'Updated %s with APP_IMAGE\n' "$ENV_FILE"
