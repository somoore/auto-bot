package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Cloudflare Access authentication.
//
// When the app runs behind a Cloudflare Access application, Cloudflare performs
// the full SSO login (Google, etc.) at the edge BEFORE any request reaches the
// app. On every authenticated request Cloudflare injects a JWT:
//
//   - As the `CF_Authorization` cookie, AND
//   - As the `Cf-Access-Jwt-Assertion` request header.
//
// The JWT is an RS256 JWS whose claims carry the user's email, signed by
// Cloudflare and validated against the team's public JWKS.
//
// SECURITY — like the ALB path, this trust model is load-bearing on signature
// verification. We verify the JWT signature against the team's published RSA
// keys (https://<team>.cloudflareaccess.com/cdn-cgi/access/certs) and only read
// identity from the verified claims. We additionally bind the token to OUR
// application by requiring the configured `aud` (Application Audience tag) and
// issuer. A request that reaches the pod via a NON-Cloudflare path (e.g. the
// Tailscale admin ingress) simply carries no valid CF JWT and falls through to
// the next auth method — it cannot forge one without the team's private key.
//
// Identity is derived per-request directly from the verified token — we do NOT
// mint a parallel auto_bot_session cookie, matching the ALB path. This keeps the
// public auth path stateless across pod restarts.

const (
	// cfAccessJWTHeader carries the CF Access JWS as a request header. The same
	// token is also available as the CF_Authorization cookie; we accept either.
	cfAccessJWTHeader = "Cf-Access-Jwt-Assertion"
	cfAccessCookie    = "CF_Authorization"
)

var (
	// cfAccessEnabled is set from APP_CF_ACCESS_AUTH=1 (behind Cloudflare Access).
	cfAccessEnabled bool
	// cfAccessTeamDomain is the team domain that issues + signs tokens, e.g.
	// https://bert-landing-pages.cloudflareaccess.com (from CF_ACCESS_TEAM_DOMAIN).
	cfAccessTeamDomain string
	// cfAccessAUD is the Application Audience tag the token must be scoped to
	// (from CF_ACCESS_AUD). Binds a token to THIS application, not any app in the
	// team.
	cfAccessAUD string

	cfAccessKeyCache = newCFAccessKeyCache()
)

// configureCFAccessAuth reads CF Access settings from the environment. It reuses
// the shared ALLOWED_EMAILS / ALLOWED_EMAIL_DOMAINS allowlist (parsed by
// configureALBAuth) as the access gate.
func configureCFAccessAuth() error {
	cfAccessEnabled = strings.TrimSpace(getEnvDefault("APP_CF_ACCESS_AUTH", "")) == "1"
	cfAccessTeamDomain = strings.TrimRight(strings.TrimSpace(getEnvDefault("CF_ACCESS_TEAM_DOMAIN", "")), "/")
	cfAccessAUD = strings.TrimSpace(getEnvDefault("CF_ACCESS_AUD", ""))
	if !cfAccessEnabled {
		return nil
	}
	if cfAccessTeamDomain == "" {
		return fmt.Errorf("CF_ACCESS_TEAM_DOMAIN is required when APP_CF_ACCESS_AUTH=1")
	}
	if !strings.HasPrefix(cfAccessTeamDomain, "https://") {
		return fmt.Errorf("CF_ACCESS_TEAM_DOMAIN must be an https URL (e.g. https://<team>.cloudflareaccess.com)")
	}
	if cfAccessAUD == "" {
		return fmt.Errorf("CF_ACCESS_AUD is required when APP_CF_ACCESS_AUTH=1 (binds the token to this application)")
	}
	// Reuse the shared email allowlist. Refuse open access behind SSO: an empty
	// allowlist would admit any account Cloudflare Access lets through.
	if len(allowedEmails) == 0 && len(allowedDomains) == 0 {
		return fmt.Errorf("ALLOWED_EMAILS or ALLOWED_EMAIL_DOMAINS must be set when APP_CF_ACCESS_AUTH=1")
	}
	return nil
}

// cfAccessClaims are the subset of CF Access JWT claims we consume.
type cfAccessClaims struct {
	Email   string `json:"email"`
	Subject string `json:"sub"`
	Aud     audSlice
	Exp     int64  `json:"exp"`
	Iss     string `json:"iss"`
}

// audSlice tolerates the `aud` claim being either a JSON string or a string
// array (CF Access emits an array; the JWT spec permits both).
type audSlice []string

func (a *audSlice) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*a = []string{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

// cfAccessContext resolves an authenticated request from the Cloudflare Access
// JWT. Returns (ctx, true) only when a valid, signature-verified token bound to
// this app's audience is present. Role is left unset — it is decided by meeting
// action (the /meeting/setup creator becomes host), matching the ALB path.
func cfAccessContext(r *http.Request) (requestAuthContext, bool) {
	if !cfAccessEnabled {
		return requestAuthContext{}, false
	}
	rawToken := cfAccessRawToken(r)
	if rawToken == "" {
		// No CF Access token — e.g. the Tailscale admin path or local dev. Fall
		// through to other auth methods.
		return requestAuthContext{}, false
	}
	claims, err := verifyCFAccessJWT(rawToken)
	if err != nil {
		log.Errorf("Rejected Cloudflare Access token: %v", err)
		return requestAuthContext{}, false
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if !emailAllowed(email) {
		log.Errorf("CF Access: denied user not on access allowlist (email=%q)", email)
		return requestAuthContext{}, false
	}

	identity := identityFromEmail(email, albOIDCClaims{Subject: claims.Subject})

	ctx := defaultAuthContext(identity)
	ctx.Email = email
	// Friendly default display label: the email local-part (e.g. "somoore2025"),
	// not the full address. Cosmetic only and client-overridable at join time.
	ctx.DisplayName = email
	if at := strings.IndexByte(email, '@'); at > 0 {
		ctx.DisplayName = email[:at]
	}
	return ctx, true
}

// cfAccessRawToken returns the raw CF Access JWS from the assertion header or the
// CF_Authorization cookie, preferring the header.
func cfAccessRawToken(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get(cfAccessJWTHeader)); h != "" {
		return h
	}
	if c, err := r.Cookie(cfAccessCookie); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

type cfAccessJOSEHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// verifyCFAccessJWT verifies a Cloudflare Access RS256 JWS and returns its
// claims. It validates the signature against the team JWKS (keyed by kid), the
// audience (must include our configured AUD), the issuer (must be our team
// domain), and expiry.
func verifyCFAccessJWT(token string) (cfAccessClaims, error) {
	var claims cfAccessClaims

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("cf access token is not a compact JWS (%d segments)", len(parts))
	}

	hdrBytes, err := decodeJWTSegment(parts[0])
	if err != nil {
		return claims, fmt.Errorf("decode cf access header: %w", err)
	}
	var hdr cfAccessJOSEHeader
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return claims, fmt.Errorf("parse cf access header: %w", err)
	}
	if strings.TrimSpace(hdr.Alg) != "RS256" {
		return claims, fmt.Errorf("unexpected cf access signing alg %q (want RS256)", hdr.Alg)
	}
	kid := strings.TrimSpace(hdr.Kid)
	if kid == "" {
		return claims, fmt.Errorf("missing kid in cf access header")
	}

	pubKey, err := cfAccessKeyCache.get(kid)
	if err != nil {
		return claims, err
	}

	sigBytes, err := decodeJWTSegment(parts[2])
	if err != nil {
		return claims, fmt.Errorf("decode cf access signature: %w", err)
	}

	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, digest[:], sigBytes); err != nil {
		return claims, fmt.Errorf("cf access signature verification failed: %w", err)
	}

	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		return claims, fmt.Errorf("decode cf access payload: %w", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, fmt.Errorf("decode cf access claims: %w", err)
	}

	// Bind the token to OUR application + team, not just any CF-signed token.
	if !audContains(claims.Aud, cfAccessAUD) {
		return claims, fmt.Errorf("cf access token audience %v does not include %q", []string(claims.Aud), cfAccessAUD)
	}
	if strings.TrimRight(strings.TrimSpace(claims.Iss), "/") != cfAccessTeamDomain {
		return claims, fmt.Errorf("unexpected cf access issuer %q", claims.Iss)
	}
	if claims.Exp != 0 && time.Now().Unix() >= claims.Exp {
		return claims, fmt.Errorf("cf access token expired")
	}
	if strings.TrimSpace(claims.Email) == "" {
		return claims, fmt.Errorf("cf access token missing email claim")
	}
	return claims, nil
}

func audContains(aud audSlice, want string) bool {
	for _, a := range aud {
		if strings.TrimSpace(a) == want {
			return true
		}
	}
	return false
}

// cfAccessKeyCacheStore fetches and caches the team's RSA public keys by kid.
type cfAccessKeyCacheStore struct {
	mu   sync.Mutex
	keys map[string]cachedCFAccessKey
}

type cachedCFAccessKey struct {
	key     *rsa.PublicKey
	fetched time.Time
}

func newCFAccessKeyCache() *cfAccessKeyCacheStore {
	return &cfAccessKeyCacheStore{keys: map[string]cachedCFAccessKey{}}
}

func (c *cfAccessKeyCacheStore) get(kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	if entry, ok := c.keys[kid]; ok && time.Since(entry.fetched) < time.Hour {
		c.mu.Unlock()
		return entry.key, nil
	}
	c.mu.Unlock()

	keys, err := fetchCFAccessKeys()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	now := time.Now()
	for k, v := range keys {
		c.keys[k] = cachedCFAccessKey{key: v, fetched: now}
	}
	entry, ok := c.keys[kid]
	c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("cf access signing key %q not found in team JWKS", kid)
	}
	return entry.key, nil
}

type cfAccessJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type cfAccessJWKS struct {
	Keys []cfAccessJWK `json:"keys"`
}

// fetchCFAccessKeys retrieves the team's JWKS and parses each RSA public key.
func fetchCFAccessKeys() (map[string]*rsa.PublicKey, error) {
	if cfAccessTeamDomain == "" {
		return nil, fmt.Errorf("cf access team domain not configured")
	}
	url := cfAccessTeamDomain + "/cdn-cgi/access/certs"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- host is the configured CF team domain.
	if err != nil {
		return nil, fmt.Errorf("fetch cf access JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cf access JWKS endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<17))
	if err != nil {
		return nil, fmt.Errorf("read cf access JWKS: %w", err)
	}
	var jwks cfAccessJWKS
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("parse cf access JWKS: %w", err)
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range jwks.Keys {
		if strings.TrimSpace(k.Kty) != "RSA" || strings.TrimSpace(k.Kid) == "" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			log.Errorf("CF Access: skipping unparseable JWK kid=%q: %v", k.Kid, err)
			continue
		}
		out[k.Kid] = pub
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cf access JWKS contained no usable RSA keys")
	}
	return out, nil
}

// rsaPublicKeyFromJWK builds an *rsa.PublicKey from the base64url-encoded JWK
// modulus (n) and exponent (e).
func rsaPublicKeyFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(nB64, "="))
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(eB64, "="))
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, fmt.Errorf("empty modulus or exponent")
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() <= 0 {
		return nil, fmt.Errorf("invalid exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e.Int64()),
	}, nil
}
