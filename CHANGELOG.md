# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and this project adheres to
[Semantic Versioning](https://semver.org/).

Per-release notes (with the contributing PRs) are also generated automatically on each
GitHub Release: https://github.com/somoore/auto-bot/releases

## [0.1.1] — 2026-06-10

### Security
- Release images are now signed with cosign (keyless / GitHub OIDC) and carry SBOM +
  SLSA provenance attestations. See `docs/deployment.md` for verification commands.

## [0.1.0] — 2026-06-10

Initial public release.

### Added
- Voice-operated Kanban board with an AI scrum-master agent (Go).
- Voice via LiveKit Cloud + AWS Bedrock (Nova Sonic), or OpenAI Realtime.
- Helm chart for Kubernetes deployment (`deploy/helm/auto-bot`).
- Terraform module for AWS Bedrock auth via IAM Roles Anywhere (`deploy/terraform/roles-anywhere`).
- Local development via Docker Compose.
- CI (build, test, govulncheck, gitleaks, helm lint) and a signed release pipeline.

[0.1.1]: https://github.com/somoore/auto-bot/releases/tag/v0.1.1
[0.1.0]: https://github.com/somoore/auto-bot/releases/tag/v0.1.0
