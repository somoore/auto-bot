// Package http will host HTTP handlers extracted from cmd/server. It
// depends on internal/board, internal/agent, internal/auth, and
// internal/tenant; cmd/server depends on this package to wire the
// router.
//
// Sprint 0 status: skeleton. Handlers move out of cmd/server/main.go in
// later sprints, after board and agent are stable.
package http
