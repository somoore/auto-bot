SHELL := /usr/bin/env bash

.PHONY: up down logs test lint security boundary actions precommit tidy hooks build

# --- Local development (Docker Compose) ---
# Copy .env.example to .env and fill in your values first.
up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f app livekit

# --- Build ---
build:
	go build ./...

# --- Quality gates ---
test:
	go test ./...

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
