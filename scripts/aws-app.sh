#!/usr/bin/env bash
set -euo pipefail

# aws-app.sh - fast-iteration + lifecycle helper for the AWS dev deploy.
#
# The ECS service runs on Fargate ARM64 with scale-to-zero in the existing VPC.
# Its Terraform definition uses `lifecycle { ignore_changes = [desired_count] }`,
# so scaling via the AWS API (up/down) is the intended control path and Terraform
# will not fight it on the next apply.

REGION="${AWS_REGION:-us-east-1}"
TG_DIR="${TG_DIR:-infra/live/dev}"
ENV_FILE="${ENV_FILE:-.env.aws.local}"

# Read a single -raw terragrunt output from the live dev dir.
tg_output() {
  (cd "$TG_DIR" && terragrunt output -raw "$1")
}

# Source ENV_FILE (exports) if present.
source_env() {
  if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
  fi
}

usage() {
  cat <<'EOF'
Usage: scripts/aws-app.sh <command>

Commands:
  deploy     Build + sign + push a new image, cosign-verify it, then run the
             terragrunt apply path so the task definition is updated to the new
             digest. This is the full "ship a new build" path.
  redeploy   Force a new ECS deployment of the CURRENT task def/image (bounce
             the service). Does NOT pull a new image build.
  up         Scale the service to 1 task.
  down       Scale the service to 0 tasks (idle, near-zero cost).
  status     Show desired/running/pending counts, task def, and rollout state.
  url        Print the app ALB URL.
  logs       Tail the app log group (follow, last 10m).

Env overrides: AWS_REGION (default us-east-1), TG_DIR (infra/live/dev),
ENV_FILE (.env.aws.local).
EOF
}

cmd_deploy() {
  # NOTE: A bare `aws ecs update-service --force-new-deployment` is NOT enough to
  # ship a new image here. force-new-deployment reuses the CURRENT task
  # definition; it only rolls a new image if the task def points at a mutable
  # tag. Our task def is driven by APP_IMAGE (a pinned reference), so the new
  # build would never be picked up by a force-new-deployment alone. The task
  # definition itself must be updated to the new APP_IMAGE, which is what the
  # terragrunt apply path does. Hence: build-push, verify, then apply.
  echo "==> Building, signing, and pushing a new image..."
  scripts/aws-build-push.sh

  # Re-source ENV_FILE so we pick up the freshly-written APP_IMAGE.
  source_env

  if [ -z "${APP_IMAGE:-}" ]; then
    echo "APP_IMAGE not set after build-push; cannot verify." >&2
    exit 1
  fi

  local kms_arn
  kms_arn="$(tg_output cosign_kms_key_arn)"

  echo "==> Verifying image signature with cosign (KMS)..."
  echo "    image: $APP_IMAGE"
  cosign verify --key "awskms:///${kms_arn}" "$APP_IMAGE"

  echo "==> Applying terragrunt so the task definition uses the new image..."
  exec scripts/aws-deploy-dev.sh -auto-approve
}

cmd_redeploy() {
  local cluster service
  cluster="$(tg_output ecs_cluster_name)"
  service="$(tg_output app_service_name)"

  echo "==> Forcing a new deployment of the current task def on $service (this does NOT pull a new image build)..."
  aws ecs update-service \
    --region "$REGION" \
    --cluster "$cluster" \
    --service "$service" \
    --force-new-deployment
}

cmd_up() {
  local cluster service
  cluster="$(tg_output ecs_cluster_name)"
  service="$(tg_output app_service_name)"

  echo "==> Scaling $service up to 1 task..."
  aws ecs update-service \
    --region "$REGION" \
    --cluster "$cluster" \
    --service "$service" \
    --desired-count 1
}

cmd_down() {
  local cluster service
  cluster="$(tg_output ecs_cluster_name)"
  service="$(tg_output app_service_name)"

  echo "==> Scaling $service down to 0 tasks (idle, near-zero cost)..."
  aws ecs update-service \
    --region "$REGION" \
    --cluster "$cluster" \
    --service "$service" \
    --desired-count 0
}

cmd_status() {
  local cluster service
  cluster="$(tg_output ecs_cluster_name)"
  service="$(tg_output app_service_name)"

  echo "==> Service status for $service:"
  aws ecs describe-services \
    --region "$REGION" \
    --cluster "$cluster" \
    --services "$service" \
    --query 'services[0].{desired:desiredCount,running:runningCount,pending:pendingCount,taskDef:taskDefinition}'

  echo "==> Latest deployment rollout state:"
  aws ecs describe-services \
    --region "$REGION" \
    --cluster "$cluster" \
    --services "$service" \
    --query 'services[0].deployments[0].rolloutState' \
    --output text
}

cmd_url() {
  echo "http://$(tg_output app_alb_dns_name)"
}

cmd_logs() {
  # The log group name uses the module name_prefix, which equals the cluster name.
  local cluster
  cluster="$(tg_output ecs_cluster_name)"

  echo "==> Tailing /ecs/${cluster}/app (follow, last 10m)..."
  aws logs tail "/ecs/${cluster}/app" \
    --region "$REGION" \
    --follow \
    --since 10m
}

main() {
  local subcommand="${1:-}"
  case "$subcommand" in
    deploy)   cmd_deploy ;;
    redeploy) cmd_redeploy ;;
    up)       cmd_up ;;
    down)     cmd_down ;;
    status)   cmd_status ;;
    url)      cmd_url ;;
    logs)     cmd_logs ;;
    ""|-h|--help|help)
      usage ;;
    *)
      echo "Unknown command: $subcommand" >&2
      echo >&2
      usage >&2
      exit 1 ;;
  esac
}

main "$@"
