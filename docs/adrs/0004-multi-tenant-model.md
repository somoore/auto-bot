# ADR 0004: Multi-Tenant Model

## Status

Proposed (Sprint 0 first slice, Sprint 5 hosted control plane).

## Context

The product is moving from a single-tenant local tool to a hosted multi-tenant offering. Every board row, Run, conflict, MCP token, and meeting transcript must be tenant-scoped without leakage. Per-tenant secrets (Jira, GitHub, LiveKit) must be isolated from each other and from the application binary.

## Decision

Thread a `TenantID string` through every request and storage path:

- `requestAuthContext` carries `TenantID`; every authenticated handler reads it.
- Every SQLite table gains a `tenant_id` column with a composite primary key.
- `kanbanBoard`, `boardStore`, `agentRun`, `jiraSyncer` are all tenant-scoped values, not globals.
- Default tenant is `"default"` for local single-tenant installs — no behavior change for self-hosters.

For hosted deployments:

- Per-tenant secrets live in AWS Secrets Manager, fetched via the existing `aws_refresh.go` path; the application binary never holds plaintext credentials at rest.
- Each tenant gets a separate SQLite DB file (or PG schema if we migrate). No shared rows across tenants.
- A control plane (`cmd/control`) owns tenant CRUD, credential rotation, and LiveKit project provisioning. Runtime servers route requests through `internal/tenant/router.go`.

## Consequences

Positive:

- Single-tenant installs are unaffected — the `"default"` tenant keeps the local-first story intact.
- Tenant leakage is a schema-enforceable invariant (composite keys), not a code-review invariant.
- KMS-backed secrets remove the worst-class credential-on-disk risk for hosted users.
- Per-tenant DB files give us a sharp blast radius for restore and forensics.

Tradeoff:

- SQLite contention at scale may force a PG migration; ADR deferred until measurable pain.
- Per-tenant LiveKit projects increase per-tenant operational cost; acceptable for the foundation, revisit if pricing breaks.
- The migration of existing tables to composite keys is a one-shot data move with downtime; plan a maintenance window.
