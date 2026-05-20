# Pre-Commit And Quality-Gate Research

This repo uses a shell quality gate plus optional support for the Python `pre-commit` framework.

## Sources Checked

- [pre-commit documentation](https://pre-commit.com/) describes `.pre-commit-config.yaml`, pinned repository revisions, `pre-commit install`, `pre-commit run --all-files`, and meta hooks such as `check-hooks-apply`.
- [pre-commit-hooks](https://github.com/pre-commit/pre-commit-hooks) provides maintained hooks for trailing whitespace, EOF normalization, YAML/JSON/TOML checks, merge-conflict checks, large-file checks, and private-key detection.
- [Go Modules Reference](https://go.dev/ref/mod) documents `go mod verify` and module checksum verification.
- [Managing dependencies](https://go.dev/doc/modules/managing-dependencies) recommends `go mod tidy` to remove unused modules and add missing module requirements.
- [govulncheck tutorial](https://go.dev/doc/tutorial/govulncheck) and the [Go govulncheck release post](https://go.dev/blog/govulncheck) describe scanning Go code for known reachable vulnerabilities.
- [golangci-lint quick start](https://golangci-lint.run/docs/welcome/quick-start/) documents `golangci-lint run` and the default linters, including `errcheck`, `govet`, `staticcheck`, `ineffassign`, and `unused`.
- [golangci-lint local install docs](https://golangci-lint.run/docs/welcome/install/local/) recommend version-pinned installs and isolating tool dependencies from project dependencies.
- [Staticcheck docs](https://staticcheck.dev/docs/) describe static analysis for bugs, performance, simplification, and style with low false-positive intent.
- [goimports docs](https://pkg.go.dev/golang.org/x/tools/cmd/goimports) describe formatting Go code while adding missing imports and removing unreferenced imports.
- [gosec docs](https://github.com/securego/gosec) describe Go security rules for hardcoded credentials, injection risks, file/path issues, crypto/TLS, blocklisted imports, and Go-specific checks.
- [GitHub Actions docs for Go](https://docs.github.com/en/actions/how-tos/writing-workflows/building-and-testing/building-and-testing-go) show the official `actions/checkout` and `actions/setup-go` path for Go CI.
- [GitHub code scanning SARIF docs](https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/uploading-a-sarif-file-to-github) describe the official `github/codeql-action/upload-sarif` family for code-scanning upload workflows.

## Implemented Gates

- `.pre-commit-config.yaml` pins `pre-commit/pre-commit-hooks` to `v6.0.0`.
- `scripts/pre-commit` runs the project gate without requiring Python `pre-commit`.
- `scripts/check-go-dependencies.sh` catches unresolved imports, stale module files, and checksum problems.
- `scripts/check-import-boundaries.sh` prevents provider/runtime dependencies from leaking into `internal/core`.
- Optional installed tools are used when present: `golangci-lint`, `govulncheck`, `gosec`, and `goimports`.
- Docker image digest pinning, Terraform/Terragrunt formatting, CDN SRI checks, and basic staged-secret scanning remain in the local gate.
- GitHub Actions workflows use official actions pinned to immutable commit SHAs rather than mutable major-version tags.

## Why This Helps

The combination of `go mod tidy`, `go list -deps`, `go mod verify`, and `goimports` catches ghost dependencies, unresolved imports, stale module requirements, checksum drift, and import clutter. The boundary script catches architectural drift that a normal linter would not understand.
