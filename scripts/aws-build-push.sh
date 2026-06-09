#!/usr/bin/env bash
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"
TG_DIR="${TG_DIR:-infra/live/dev}"
TAG="${TAG:-$(git rev-parse --short=12 HEAD)}"
ENV_FILE="${ENV_FILE:-.env.aws.local}"

if [ "$TAG" = "latest" ]; then
  echo "TAG=latest is not allowed; the ECR repo is immutable, use a release or git SHA tag." >&2
  exit 1
fi

if ! command -v terragrunt >/dev/null 2>&1; then
  echo "terragrunt is required" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

if ! command -v cosign >/dev/null 2>&1; then
  echo "cosign is required" >&2
  exit 1
fi

REPO_URL="$(cd "$TG_DIR" && terragrunt output -raw ecr_repository_url)"
REGISTRY="${REPO_URL%/*}"
REPO_NAME="$(basename "$REPO_URL")"
IMAGE="${REPO_URL}:${TAG}"

KMS_KEY_ARN="$(cd "$TG_DIR" && terragrunt output -raw cosign_kms_key_arn)"

# Authenticate Docker against the private ECR registry.
aws ecr get-login-password --region "$REGION" | docker login --username AWS --password-stdin "$REGISTRY"

# Build natively for linux/arm64 (Apple Silicon, no emulation) and push directly.
# Use a one-shot buildx builder so this works on a fresh machine.
BUILDER_NAME="auto-bot-arm64"
if ! docker buildx inspect "$BUILDER_NAME" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER_NAME" --driver docker-container --use >/dev/null
else
  docker buildx use "$BUILDER_NAME"
fi
docker buildx inspect --bootstrap >/dev/null

# buildx --push pushes directly; do NOT also run `docker push`.
docker buildx build --platform linux/arm64 --provenance=false -t "$IMAGE" --push .

# Resolve the immutable digest so cosign pins the content, not the mutable tag.
DIGEST="$(aws ecr describe-images \
  --region "$REGION" \
  --repository-name "$REPO_NAME" \
  --image-ids imageTag="$TAG" \
  --query 'imageDetails[0].imageDigest' \
  --output text)"

if [ -z "$DIGEST" ] || [ "$DIGEST" = "None" ]; then
  echo "Failed to resolve image digest for ${REPO_NAME}:${TAG}" >&2
  exit 1
fi

IMAGE_REF="${REPO_URL}@${DIGEST}"

# Sign and verify the digest non-interactively using the KMS key.
COSIGN_YES=true cosign sign --key "awskms:///${KMS_KEY_ARN}" "$IMAGE_REF"
cosign verify --key "awskms:///${KMS_KEY_ARN}" "$IMAGE_REF" >/dev/null

# Pin APP_IMAGE to the immutable digest reference for downstream deploys.
# Strip any existing APP_IMAGE line, then append the new one. Avoid sed/perl
# substitution here: IMAGE_REF contains '@', '/' and ':' which break regex
# replacement interpolation (a prior perl version silently dropped '@sha256').
if [ -f "$ENV_FILE" ]; then
  grep -v '^export APP_IMAGE=' "$ENV_FILE" > "${ENV_FILE}.tmp" || true
  mv "${ENV_FILE}.tmp" "$ENV_FILE"
fi
printf "export APP_IMAGE='%s'\n" "$IMAGE_REF" >> "$ENV_FILE"

printf 'Pushed %s\n' "$IMAGE"
printf 'Digest %s\n' "$IMAGE_REF"
printf 'Signed and verified with KMS key %s\n' "$KMS_KEY_ARN"
printf 'Updated %s with APP_IMAGE\n' "$ENV_FILE"
