package mcp

import "strings"

// extractBearerToken pulls the token portion out of an Authorization
// header value of the form "Bearer <token>" (case-insensitive scheme).
// Returns the token and true on success; empty string and false if the
// header is missing or malformed. The actual cryptographic check is
// performed by Verifier.Verify; this helper only parses.
func extractBearerToken(header string) (string, bool) {
	const prefix = "bearer "
	h := strings.TrimSpace(header)
	if len(h) < len(prefix) {
		return "", false
	}
	if !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
