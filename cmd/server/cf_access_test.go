package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newCFRequestWithCookie builds a request carrying the CF Access JWT in the
// CF_Authorization cookie.
func newCFRequestWithCookie(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: cfAccessCookie, Value: token})
	return r
}

// makeCFToken hand-builds an RS256 compact JWS the way Cloudflare Access does,
// signed by the given RSA key.
func makeCFToken(t *testing.T, key *rsa.PrivateKey, header, payload map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := enc(header) + "." + enc(payload)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// withCFAccessTestKey injects a synthetic RSA key into the kid cache and sets the
// team domain + aud, restoring them on cleanup.
func withCFAccessTestKey(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	prevCache := cfAccessKeyCache
	prevDomain := cfAccessTeamDomain
	prevAUD := cfAccessAUD
	prevEnabled := cfAccessEnabled
	cfAccessKeyCache = newCFAccessKeyCache()
	cfAccessKeyCache.keys[kid] = cachedCFAccessKey{key: &key.PublicKey, fetched: time.Now()}
	cfAccessTeamDomain = "https://team.cloudflareaccess.com"
	cfAccessAUD = "test-aud-123"
	cfAccessEnabled = true
	t.Cleanup(func() {
		cfAccessKeyCache = prevCache
		cfAccessTeamDomain = prevDomain
		cfAccessAUD = prevAUD
		cfAccessEnabled = prevEnabled
	})
	return key
}

func TestVerifyCFAccessJWT_Synthetic(t *testing.T) {
	const kid = "cf-test-kid"
	key := withCFAccessTestKey(t, kid)

	header := map[string]any{"alg": "RS256", "kid": kid}
	payload := map[string]any{
		"email": "scott@moore.cloud",
		"sub":   "abc",
		"aud":   []string{"test-aud-123"},
		"iss":   "https://team.cloudflareaccess.com",
		"exp":   int64(1 << 62),
	}
	token := makeCFToken(t, key, header, payload)

	claims, err := verifyCFAccessJWT(token)
	if err != nil {
		t.Fatalf("verify failed on a valid synthetic token: %v", err)
	}
	if claims.Email != "scott@moore.cloud" {
		t.Errorf("email mismatch: %+v", claims)
	}

	// Tampered signature must fail.
	bad := token[:len(token)-4] + "AAAA"
	if _, err := verifyCFAccessJWT(bad); err == nil {
		t.Error("expected verification failure on tampered signature")
	}

	// Wrong key must fail.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	cfAccessKeyCache.keys[kid] = cachedCFAccessKey{key: &other.PublicKey, fetched: time.Now()}
	if _, err := verifyCFAccessJWT(token); err == nil {
		t.Error("expected verification failure with wrong public key")
	}
}

func TestVerifyCFAccessJWT_RejectsWrongAudAndIssAndExpiry(t *testing.T) {
	const kid = "cf-test-kid"
	key := withCFAccessTestKey(t, kid)
	header := map[string]any{"alg": "RS256", "kid": kid}

	base := map[string]any{
		"email": "scott@moore.cloud",
		"sub":   "abc",
		"aud":   []string{"test-aud-123"},
		"iss":   "https://team.cloudflareaccess.com",
		"exp":   int64(1 << 62),
	}

	clone := func(over map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		return m
	}

	// Wrong audience (token for a different CF app) must fail.
	if _, err := verifyCFAccessJWT(makeCFToken(t, key, header, clone(map[string]any{"aud": []string{"other-app"}}))); err == nil {
		t.Error("expected failure on wrong audience")
	}
	// Wrong issuer must fail.
	if _, err := verifyCFAccessJWT(makeCFToken(t, key, header, clone(map[string]any{"iss": "https://evil.cloudflareaccess.com"}))); err == nil {
		t.Error("expected failure on wrong issuer")
	}
	// Expired token must fail.
	if _, err := verifyCFAccessJWT(makeCFToken(t, key, header, clone(map[string]any{"exp": int64(1)}))); err == nil {
		t.Error("expected failure on expired token")
	}
	// Wrong alg must fail.
	if _, err := verifyCFAccessJWT(makeCFToken(t, key, map[string]any{"alg": "HS256", "kid": kid}, base)); err == nil {
		t.Error("expected failure on non-RS256 alg")
	}
	// String (non-array) aud is accepted when it matches.
	if _, err := verifyCFAccessJWT(makeCFToken(t, key, header, clone(map[string]any{"aud": "test-aud-123"}))); err != nil {
		t.Errorf("expected string aud to be accepted: %v", err)
	}
}

func TestCFAccessContextDeniesNonAllowlistedEmail(t *testing.T) {
	const kid = "cf-test-kid"
	key := withCFAccessTestKey(t, kid)

	prevEmails := allowedEmails
	prevDomains := allowedDomains
	allowedEmails = map[string]struct{}{}
	allowedDomains = map[string]struct{}{"moore.cloud": {}}
	t.Cleanup(func() { allowedEmails = prevEmails; allowedDomains = prevDomains })

	header := map[string]any{"alg": "RS256", "kid": kid}
	mk := func(email string) string {
		return makeCFToken(t, key, header, map[string]any{
			"email": email, "sub": "x", "aud": []string{"test-aud-123"},
			"iss": "https://team.cloudflareaccess.com", "exp": int64(1 << 62),
		})
	}

	// Allowlisted domain -> identity derived from email local-part.
	r := newCFRequestWithCookie(mk("scott@moore.cloud"))
	ctx, ok := cfAccessContext(r)
	if !ok {
		t.Fatal("expected allowlisted user to authenticate")
	}
	if ctx.Identity != "scott" || ctx.Email != "scott@moore.cloud" {
		t.Errorf("unexpected ctx: identity=%q email=%q", ctx.Identity, ctx.Email)
	}

	// Non-allowlisted -> denied.
	if _, ok := cfAccessContext(newCFRequestWithCookie(mk("stranger@gmail.com"))); ok {
		t.Error("expected non-allowlisted user to be denied")
	}
}

func TestConfigureCFAccessAuthFailsClosed(t *testing.T) {
	prevEnabled := cfAccessEnabled
	prevEmails := allowedEmails
	prevDomains := allowedDomains
	t.Cleanup(func() {
		cfAccessEnabled = prevEnabled
		allowedEmails = prevEmails
		allowedDomains = prevDomains
	})
	allowedEmails = map[string]struct{}{}
	allowedDomains = map[string]struct{}{"moore.cloud": {}}

	// Enabled but no team domain -> error.
	t.Setenv("APP_CF_ACCESS_AUTH", "1")
	t.Setenv("CF_ACCESS_TEAM_DOMAIN", "")
	t.Setenv("CF_ACCESS_AUD", "aud123")
	if err := configureCFAccessAuth(); err == nil {
		t.Error("expected error when CF_ACCESS_TEAM_DOMAIN is empty")
	}

	// Enabled, domain set, but no aud -> error.
	t.Setenv("CF_ACCESS_TEAM_DOMAIN", "https://team.cloudflareaccess.com")
	t.Setenv("CF_ACCESS_AUD", "")
	if err := configureCFAccessAuth(); err == nil {
		t.Error("expected error when CF_ACCESS_AUD is empty")
	}

	// Enabled, domain + aud set, but no allowlist -> error (no open access).
	allowedEmails = map[string]struct{}{}
	allowedDomains = map[string]struct{}{}
	t.Setenv("CF_ACCESS_AUD", "aud123")
	if err := configureCFAccessAuth(); err == nil {
		t.Error("expected error when no allowlist set under CF Access")
	}
}

func TestSanitizeDisplayName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Scott Moore", "Scott Moore"},
		{"  spaced  ", "spaced"},
		{"line\nbreak", "linebreak"},
		{"tab\tsep", "tabsep"},
		{"ctrl\x00\x07byte", "ctrlbyte"},
		{"", ""},
		{"   ", ""},
		{"José O'Brien-Smith", "José O'Brien-Smith"},
	}
	for _, c := range cases {
		if got := sanitizeDisplayName(c.in); got != c.want {
			t.Errorf("sanitizeDisplayName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Length cap: 100 runes -> 64.
	long := ""
	for i := 0; i < 100; i++ {
		long += "x"
	}
	if got := sanitizeDisplayName(long); len([]rune(got)) != 64 {
		t.Errorf("sanitizeDisplayName(100 chars) length = %d, want 64", len([]rune(got)))
	}
}

func TestBearerAllowedForHost(t *testing.T) {
	prev := adminBearerHosts
	t.Cleanup(func() { adminBearerHosts = prev })

	// Empty allowlist -> honored on any host (legacy / no-SSO).
	adminBearerHosts = map[string]struct{}{}
	if !bearerAllowedForHost(httptest.NewRequest(http.MethodGet, "https://plan.sc.tt/", nil)) {
		t.Error("empty allowlist should honor bearer on any host")
	}

	// Allowlist set -> only the listed (Tailscale admin) host is honored.
	adminBearerHosts = parseHostSet("k3s-node.taila31b1.ts.net")
	tailReq := httptest.NewRequest(http.MethodGet, "http://k3s-node.taila31b1.ts.net/", nil)
	if !bearerAllowedForHost(tailReq) {
		t.Error("expected bearer honored on the Tailscale admin host")
	}
	for _, publicHost := range []string{"https://plan.sc.tt/", "https://plan-stage.sc.tt/"} {
		if bearerAllowedForHost(httptest.NewRequest(http.MethodGet, publicHost, nil)) {
			t.Errorf("expected bearer REFUSED (fail closed) on public host %s", publicHost)
		}
	}
}

// TestCFAccessFailsClosedOnPublicHost is the end-to-end security property: when
// CF Access is the front door and the injected bearer is confined to the admin
// host, a public request that fails CF validation (no JWT) must NOT fall back to
// the shared bearer — it must be unauthorized, not silently downgraded to the
// generic "api-token" identity.
func TestBearerFailsClosedOnPublicHostWithoutCFToken(t *testing.T) {
	prevToken := apiToken
	prevMode := appAuthMode
	prevAdmin := adminBearerHosts
	prevRoom := appRoomID
	prevBoard := appBoardID
	t.Cleanup(func() {
		apiToken = prevToken
		appAuthMode = prevMode
		adminBearerHosts = prevAdmin
		appRoomID = prevRoom
		appBoardID = prevBoard
	})

	apiToken = "shared-injected-token"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	adminBearerHosts = parseHostSet("k3s-node.taila31b1.ts.net")

	// Public host, injected bearer present, but NO CF JWT -> must fail closed.
	req := httptest.NewRequest(http.MethodGet, "https://plan.sc.tt/auth/session?room_id=kanban-meeting&board_id=default", nil)
	req.Header.Set("Authorization", "Bearer shared-injected-token")
	if _, ok := authorizeBaseRequest(req); ok {
		t.Fatal("public request with injected bearer but no CF JWT must NOT authorize (fail closed)")
	}

	// Same request on the Tailscale admin host -> bearer honored.
	adminReq := httptest.NewRequest(http.MethodGet, "http://k3s-node.taila31b1.ts.net/auth/session?room_id=kanban-meeting&board_id=default", nil)
	adminReq.Header.Set("Authorization", "Bearer shared-injected-token")
	if _, ok := authorizeBaseRequest(adminReq); !ok {
		t.Fatal("Tailscale admin request with injected bearer must authorize")
	}
}

func TestCFAccessDisabledWhenFlagOff(t *testing.T) {
	prevEnabled := cfAccessEnabled
	t.Cleanup(func() { cfAccessEnabled = prevEnabled })
	t.Setenv("APP_CF_ACCESS_AUTH", "")
	if err := configureCFAccessAuth(); err != nil {
		t.Fatalf("configureCFAccessAuth: %v", err)
	}
	if cfAccessEnabled {
		t.Fatal("expected cfAccessEnabled false when flag unset")
	}
}
