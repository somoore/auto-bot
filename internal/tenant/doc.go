// Package tenant owns tenant identity, per-tenant secret storage, and the
// routing layer that scopes a request to one tenant's board, agents, and
// connectors.
//
// Auto-bot was single-tenant through 2026 Q2. Sprint 0 threads TenantID
// through auth, board, store, and agent runs. The hosted control plane in
// Sprint 5 builds on this foundation.
//
// Sprint 0 status: skeleton. Default tenant ID "default" is used everywhere
// until the hosted control plane lands.
package tenant
