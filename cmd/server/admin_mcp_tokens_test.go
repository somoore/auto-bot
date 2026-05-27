package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/mcp"
)

// resetMCPIssuer wipes the lazy singleton between tests so each subtest
// can install its own MCP_SIGNING_KEYS without leaking state.
func resetMCPIssuer() {
	mcpIssuer = nil
	mcpIssuerErr = nil
	mcpIssuerOnce = sync.Once{}
}

func setMCPSigningKeysForTest(t *testing.T) []mcp.SigningKey {
	t.Helper()
	key1 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i + 1)
	}
	raw := "k1:" + base64.StdEncoding.EncodeToString(key1)
	t.Setenv("MCP_SIGNING_KEYS", raw)
	resetMCPIssuer()
	return []mcp.SigningKey{{KeyID: "k1", Key: key1}}
}

func TestAdminMCPTokensRejectsMissingBearer(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	apiToken = "admin-token"
	appAuthMode = "token"
	setMCPSigningKeysForTest(t)

	body := []byte(`{"subject":"agent:claude-code","scopes":["board:read"]}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/mcp-tokens", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adminMCPTokensHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminMCPTokensIssuesValidToken(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	apiToken = "admin-token"
	appAuthMode = "token"
	keys := setMCPSigningKeysForTest(t)

	body := []byte(`{"subject":"agent:claude-code","tenant_id":"default","scopes":["board:read","card:write"],"ttl_seconds":300}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/mcp-tokens", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMCPTokensHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp mcpTokenIssueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, rec.Body.String())
	}
	if resp.Token == "" {
		t.Fatalf("issued empty token")
	}
	if resp.Subject != "agent:claude-code" {
		t.Errorf("subject = %q, want agent:claude-code", resp.Subject)
	}
	if !strings.Contains(strings.Join(resp.Scopes, ","), "board:read") {
		t.Errorf("scopes = %v, want board:read in set", resp.Scopes)
	}
	if resp.JTI == "" {
		t.Errorf("JTI empty")
	}
	if time.Until(resp.ExpiresAt) < 4*time.Minute || time.Until(resp.ExpiresAt) > 6*time.Minute {
		t.Errorf("expires_at = %v, expected ~5min from now", resp.ExpiresAt)
	}

	// And verify the token round-trips through Verifier.
	v, err := mcp.NewVerifier(keys, mcp.NewMemoryReplayTracker())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	claims, err := v.Verify(resp.Token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !claims.HasScope("board:read") || !claims.HasScope("card:write") {
		t.Errorf("verified scopes = %v missing required", claims.Scopes)
	}
}

func TestAdminMCPTokensRejectsUnknownScope(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	apiToken = "admin-token"
	appAuthMode = "token"
	setMCPSigningKeysForTest(t)

	body := []byte(`{"subject":"agent:claude-code","scopes":["god:mode"]}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/mcp-tokens", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	adminMCPTokensHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (unknown scope must fail loud)", rec.Code)
	}
}

func TestAdminMCPTokensRejectsMissingSigningKeys(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	apiToken = "admin-token"
	appAuthMode = "token"
	t.Setenv("MCP_SIGNING_KEYS", "")
	resetMCPIssuer()

	body := []byte(`{"subject":"agent:claude-code","scopes":["board:read"]}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/mcp-tokens", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	adminMCPTokensHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no signing keys configured)", rec.Code)
	}
}

func TestAdminMCPTokensClampsTTL(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	apiToken = "admin-token"
	appAuthMode = "token"
	setMCPSigningKeysForTest(t)

	// 48 hours (over the 24h cap).
	body := []byte(`{"subject":"agent:x","scopes":["board:read"],"ttl_seconds":172800}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/mcp-tokens", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	adminMCPTokensHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp mcpTokenIssueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if time.Until(resp.ExpiresAt) > 24*time.Hour+time.Minute {
		t.Errorf("TTL not clamped: expires %v from now (>24h)", time.Until(resp.ExpiresAt))
	}
}
