package mcp

import (
	"crypto/subtle"
	"strings"
)

// checkBearer compares the Authorization header value against the configured
// token in constant time. Returns true iff the header is "Bearer <token>"
// with an exact match. We trim the scheme case-insensitively because some
// clients send "bearer" lowercase.
//
// Sprint 2.1 will replace this single-token gate with scoped per-agent
// tokens minted at agent-profile registration time; the call site will
// stay the same.
func checkBearer(header, expected string) bool {
	if expected == "" {
		return true
	}
	const prefix = "bearer "
	h := strings.TrimSpace(header)
	if len(h) < len(prefix) {
		return false
	}
	if !strings.EqualFold(h[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
