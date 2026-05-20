SHELL := /usr/bin/env bash

.PHONY: dev docker-up docker-down test eval lint security boundary actions precommit tidy hooks

dev:
	scripts/local-up.sh

docker-up:
	scripts/local-compose.sh up --build -d

docker-down:
	scripts/local-down.sh

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
