SHELL := /usr/bin/env bash

.PHONY: dev docker-up docker-down test eval lint security boundary actions precommit tidy hooks \
	aws-secrets aws-deploy aws-up aws-down aws-status aws-logs

dev:
	scripts/local-up.sh

docker-up:
	scripts/local-compose.sh up --build -d

docker-down:
	scripts/local-down.sh

# --- AWS dev deploy (LiveKit Cloud, ECS Fargate ARM64, scale-to-zero) ---
# One-time: AWS_REGION=us-east-1 LIVEKIT_CLOUD_URL=wss://... LIVEKIT_API_KEY=... \
#   LIVEKIT_API_SECRET=... make aws-secrets, then source .env.aws.local.
aws-secrets:
	AWS_REGION=us-east-1 scripts/aws-upsert-secrets.sh

# Build linux/arm64, cosign-sign, and terragrunt apply the new image. Fast loop.
aws-deploy:
	scripts/aws-app.sh deploy

aws-up:
	scripts/aws-app.sh up

aws-down:
	scripts/aws-app.sh down

aws-status:
	scripts/aws-app.sh status

aws-logs:
	scripts/aws-app.sh logs

test:
	go test ./...

eval:
	go test ./evaluation

tidy:
	go mod tidy

boundary:
	scripts/check-import-boundaries.sh

actions:
	scripts/check-github-actions-pinning.sh

lint: boundary actions
	go vet ./...
	if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed; skipping"; fi

security:
	go mod verify
	if command -v govulncheck >/dev/null 2>&1; then govulncheck ./...; else echo "govulncheck not installed; skipping"; fi
	if command -v gosec >/dev/null 2>&1; then gosec ./...; else echo "gosec not installed; skipping"; fi

precommit:
	scripts/pre-commit

hooks:
	scripts/install-hooks.sh
