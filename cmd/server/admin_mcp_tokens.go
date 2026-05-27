package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/somoore/auto-bot/internal/mcp"
)

// MCP signing keys are loaded at startup from MCP_SIGNING_KEYS. The same
// env var must be set on cmd/mcpd so the verifier and issuer agree.
//
// Format (matches internal/mcp.ParseSigningKeys):
//
//	MCP_SIGNING_KEYS=kid1:base64-32-byte-key[,kid2:base64-key...]
//
// The first key is the active signer (used by Issue); the rest are
// verifier-only — i.e. previously-active keys that are still accepted
// while clients rotate to tokens minted under the new active key.
//
// Operators bootstrap by generating a key with `mcp.GenerateSigningKey`
// (or `openssl rand -base64 32`) and setting `kid1:<key>`.
var (
	mcpIssuer     *mcp.Issuer
	mcpIssuerOnce sync.Once
	mcpIssuerErr  error
)

// loadMCPIssuer parses MCP_SIGNING_KEYS once at first call and caches
// the result. Returns an error if the env var is missing or invalid;
// callers (the admin handler) translate this into HTTP 503.
func loadMCPIssuer() (*mcp.Issuer, error) {
	mcpIssuerOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("MCP_SIGNING_KEYS"))
		if raw == "" {
			mcpIssuerErr = errors.New("MCP_SIGNING_KEYS is not set on this server; token issuance is disabled")
			return
		}
		keys, err := mcp.ParseSigningKeys(raw)
		if err != nil {
			mcpIssuerErr = fmt.Errorf("MCP_SIGNING_KEYS invalid: %w", err)
			return
		}
		mcpIssuer, mcpIssuerErr = mcp.NewIssuer(keys)
	})
	return mcpIssuer, mcpIssuerErr
}

// mcpTokenIssueRequest is the POST /admin/mcp-tokens body. Subject is
// the identity the token represents (e.g. "agent:claude-code"); Scopes
// must be a non-empty subset of mcp.AllToolScopes(); TTLSeconds caps at
// 24h. TenantID defaults to the active appBoardID's tenant when omitted.
type mcpTokenIssueRequest struct {
	Subject    string   `json:"subject"`
	TenantID   string   `json:"tenant_id"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int      `json:"ttl_seconds"`
}

// mcpTokenIssueResponse is the success body. Token is the signed bearer
// to hand to the MCP client; ExpiresAt is the RFC3339 wall-clock expiry
// so the operator can stash it alongside; JTI is the unique ID the
// replay tracker will reject on second use.
type mcpTokenIssueResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	JTI       string    `json:"jti"`
	Subject   string    `json:"subject"`
	TenantID  string    `json:"tenant_id"`
	Scopes    []string  `json:"scopes"`
}

// adminMCPTokensHandler mints MCP bearer tokens. The endpoint is gated
// by the same APP_API_TOKEN as the rest of the /internal/* surface
// because the operator (cmd/server administrator) is the only party
// allowed to issue tokens — the MCP clients themselves cannot escalate.
//
// Validation:
//   - Method must be POST
//   - Bearer must match APP_API_TOKEN (authorizeBaseRequest)
//   - Subject + Scopes required; Scopes must be a subset of the known
//     tool scopes (no wildcards, no unknown scope strings — fail loud
//     so operators can't accidentally grant phantom privileges)
//   - TTLSeconds defaults to 900 (15 min); capped at 86400 (24 h)
func adminMCPTokensHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	issuer, err := loadMCPIssuer()
	if err != nil {
		log.Errorf("admin mcp-tokens: issuer unavailable: %v", err)
		writeDispatchError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	var req mcpTokenIssueRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	if req.Subject == "" {
		writeDispatchError(w, http.StatusBadRequest, "subject is required")
		return
	}
	if req.TenantID == "" {
		req.TenantID = "default"
	}
	if len(req.Scopes) == 0 {
		writeDispatchError(w, http.StatusBadRequest, "at least one scope is required")
		return
	}

	known := map[string]bool{}
	for _, s := range mcp.AllToolScopes() {
		known[s] = true
	}
	for _, s := range req.Scopes {
		s = strings.TrimSpace(s)
		if !known[s] {
			writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("unknown scope %q (known: %v)", s, mcp.AllToolScopes()))
			return
		}
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if ttl > 24*time.Hour {
		ttl = 24 * time.Hour
	}

	token, claims, err := issuer.Issue(req.Subject, req.TenantID, req.Scopes, ttl)
	if err != nil {
		writeDispatchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeDispatchJSON(w, http.StatusOK, mcpTokenIssueResponse{
		Token:     token,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(),
		JTI:       claims.JTI,
		Subject:   claims.Subject,
		TenantID:  claims.TenantID,
		Scopes:    claims.Scopes,
	})
}
