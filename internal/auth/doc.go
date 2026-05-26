// Package auth owns session identity, request authentication, and the
// requestAuthContext type that flows through every authenticated handler.
//
// Identity is tenant-scoped: every authenticated subject carries a TenantID.
// Subjects can be human users, agent runs, or MCP clients holding a scoped
// token. The package does not own connector credentials — those live in
// internal/tenant/secrets.
//
// Sprint 0 status: skeleton. cmd/server/auth.go moves here in Sprint 0.4
// together with the TenantID threading.
package auth
