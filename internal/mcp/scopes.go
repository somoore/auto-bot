package mcp

// Scope catalog for MCP tokens. Scopes are compared verbatim — no
// wildcard expansion. `admin:issue` is the special scope granting the
// holder permission to mint new tokens through the issuer endpoint;
// it does *not* satisfy any tool-side scope, so an admin token cannot
// accidentally drive board mutations.
const (
	ScopeBoardRead  = "board:read"
	ScopeCardWrite  = "card:write"
	ScopeRunsStart  = "runs:start"
	ScopeAdminIssue = "admin:issue"
)

// ToolScopes is the mapping from MCP tool name to the scope a caller
// must hold. A tool absent from this map has no scope requirement
// (only board.list_cards and board.get_card are read-only enough to be
// gated by the lightweight board:read; mutating tools always require a
// write-class scope). The middleware in server.go consults this map
// before dispatching tools/call, so handlers cannot accidentally
// forget to enforce.
var ToolScopes = map[string]string{
	"board.list_cards": ScopeBoardRead,
	"board.get_card":   ScopeBoardRead,
	"card.create":      ScopeCardWrite,
	"card.update":      ScopeCardWrite,
	"card.comment":     ScopeCardWrite,
	"runs.start":       ScopeRunsStart,
}

// AllToolScopes returns the deduplicated set of scopes the operator
// would issue to a full-access MCP client. Used by docs and admin
// tooling so the list stays in sync with ToolScopes.
func AllToolScopes() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range ToolScopes {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
