# Threat Model

## Scope

Auto Bot connects live meeting audio, browser participants, a scrum-master agent, Jira, GitHub, AWS Bedrock, LiveKit, local storage, and AWS infrastructure. This document covers the application and integration trust boundaries.

## Assets

- Jira API tokens, Jira issue data, workflow metadata, and board state.
- GitHub App private key, installation tokens, repository diffs, and PR comments.
- AWS credentials, Bedrock access, LiveKit credentials, and deployment secrets.
- Meeting codes, session cookies, participant identity, transcripts, audio/video tracks, and meeting reports.
- Audit/replay ledger and persisted board state.

## Trust Boundaries

- Browser to Go server over HTTP/WebSocket.
- Browser to LiveKit media plane.
- Go server to AWS Bedrock/Nova Sonic.
- Go server to Jira Cloud.
- Go server to GitHub App APIs.
- Go server to SQLite on a persistent volume.
- Go server to AWS Bedrock (via IAM Roles Anywhere short-lived credentials).

## Primary Threats And Mitigations

| Threat | Impact | Mitigation |
| --- | --- | --- |
| Prompt injection in Jira task text or PR content | Agent performs attacker-controlled action | Treat external text as untrusted data; guard mutating tool args; only live user speech can authorize actions |
| Agent claims success without API confirmation | Users trust a Jira/GitHub state that never changed | Store local mutation and external confirmation separately; assistant instructions forbid success claims without API proof |
| Cross-board Jira write | Wrong project or board is modified | Enforce configured Jira `project_key`; reject hydrated or mutated issues outside project |
| Stolen local token or browser session | Unauthorized meeting or board access | HttpOnly cookies, no token in served HTML; front the ingress with SSO (e.g. an identity-aware proxy) for public deployment |
| GitHub over-permission | Agent can mutate code or repos beyond scope | GitHub App least privilege, repo allowlist, short-lived installation tokens, PR write only when comments are enabled |
| LiveKit media failure | Participants cannot hear/see agent or each other | Voice reliability dashboard, `/voice/status`, participant-audio tracking, LiveKit hardening checklist |
| AWS credential leakage | Cloud compromise | IAM Roles Anywhere short-lived STS credentials (no long-lived keys in-cluster), least-privilege IAM scoped to specific Bedrock model ARNs |
| Supply-chain drift | CI/build pulls unexpected code | Docker digest pinning, pinned GitHub Action SHAs, `go mod verify`, pre-commit hooks, dependency review |
| Audit loss after restart | Cannot prove why an action happened | SQLite action replay ledger persists mutation replay records |

## Residual Risks

- Real 2-4 participant LiveKit proof still must be run from realistic networks before public demos.
- Public multi-tenant hosting needs workspace-scoped secrets and per-connector install records. The app supports per-user SSO identity (Cloudflare Access / ALB OIDC, deriving identity from a verified email); workspace-level multi-tenancy is the remaining gap.
