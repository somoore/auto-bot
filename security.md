# Security Policy

## Supported Versions

This project is pre-1.0. Security fixes are applied to the main development branch until formal releases are created.

## Reporting A Vulnerability

Do not open a public issue for sensitive vulnerabilities. Use GitHub's private vulnerability reporting flow for the repository when available, for example `https://github.com/somoore/auto-bot/security/advisories/new` for the maintainer repo. If that route is not available in your fork, contact the repository owner privately through GitHub and request a secure disclosure channel before sharing exploit details.

Expected response window: initial acknowledgement within 5 business days, with remediation priority based on exploitability, affected deployment surface, and whether secrets or external write paths are exposed.

Include:

- Affected commit or release.
- Reproduction steps.
- Impact and exploitability.
- Logs or screenshots with secrets removed.
- Suggested fix if known.

## Security Principles

- Secrets must stay out of the repo. Local secrets use macOS Keychain; AWS deployment uses Secrets Manager.
- Jira, GitHub, meeting transcripts, task text, PR text, comments, labels, and user profiles are untrusted data.
- The agent may not claim Jira/GitHub success unless the server-side API confirms the write.
- Risky actions require confirmation.
- External writes must produce audit/replay evidence.
- GitHub access should use a GitHub App with least privilege and short-lived installation tokens.
- AWS IAM should be least privilege and private by default. The deployment region is `us-east-1`; Bedrock permissions are narrowed to explicit inference-profile/backing model ARNs across the required AWS US regions instead of broad wildcard model access.

## CI Expectations

Pull requests should pass the repository quality gate:

```bash
scripts/pre-commit
```

This includes Go tests, dependency hygiene, import-boundary checks, vulnerability scanning when tools are installed, Docker image pinning, Terraform/Terragrunt formatting, CDN SRI checks, and secret scanning.
