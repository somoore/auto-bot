package main

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ALB-level Cognito authentication.
//
// When the app runs behind an Application Load Balancer with an
// authenticate-cognito action, the ALB performs the full OIDC login (Cognito
// Hosted UI + Google federation) BEFORE any request reaches this task. On every
// authenticated request the ALB injects two headers:
//
//   - X-Amzn-Oidc-Identity: the bare Cognito subject (NOT trusted on its own).
//   - X-Amzn-Oidc-Data:     a JWS (ES256) whose claims carry the user's email,
//                           signed by the ALB. This is the trustworthy source.
//
// SECURITY — this trust model is load-bearing on TWO independent controls, and
// BOTH must hold. If either regresses, identity can be spoofed:
//
//  1. We verify the X-Amzn-Oidc-Data signature against the ALB regional public
//     key (https://public-keys.auth.elb.<region>.amazonaws.com/<kid>) and only
//     read identity from the verified claims. We NEVER trust X-Amzn-Oidc-Identity
//     or unverified header values for identity.
//  2. The app task security group admits inbound ONLY from the ALB security
//     group (see infra/modules/auto-bot/security.tf). Nothing else can reach the
//     task to inject forged X-Amzn-Oidc-* headers. Do not widen that SG.
//
// Identity is derived per-request directly from the verified header — we do NOT
// mint a parallel auto_bot_session cookie. That keeps auth stateless (the task is
// scale-to-zero and restarts often, which would drop any in-memory session) and
// avoids a second, independently-expiring session lifetime alongside the ALB's
// own session cookie.

// albOIDCDataHeader carries the ALB-signed JWS we verify. We intentionally do
// NOT read X-Amzn-Oidc-Identity (an unsigned header) for identity.
const albOIDCDataHeader = "X-Amzn-Oidc-Data"

var (
	// albAuthEnabled is set from APP_ALB_OIDC_AUTH=1 (production behind the ALB).
	albAuthEnabled bool
	// hostEmails is the allowlist of email claims granted the host role. Everyone
	// else who authenticates (and is allowed in) becomes a participant.
	hostEmails = map[string]struct{}{}
	// allowedEmails / allowedDomains gate ACCESS: an authenticated Google user
	// whose email is not allowed is denied by the app even though the ALB let
	// them log in. When BOTH are empty, access is open to any authenticated user
	// (set at least one for a non-public deployment).
	allowedEmails  = map[string]struct{}{}
	allowedDomains = map[string]struct{}{}

	albKeyCache = newALBKeyCache()
)

// cognitoLogoutURL is the full Cognito Hosted UI /logout URL (with client_id +
// logout_uri), set from COGNITO_LOGOUT_URL. Empty when ALB auth is off.
var cognitoLogoutURL string

// expectedSignerARN, when set (APP_ALB_ARN), is the ALB ARN whose signature we
// require on X-Amzn-Oidc-Data. This ensures the JWS was signed by OUR ALB and
// not merely by some ALB key in the region.
var expectedSignerARN string

func configureALBAuth() error {
	albAuthEnabled = strings.TrimSpace(os.Getenv("APP_ALB_OIDC_AUTH")) == "1"
	cognitoLogoutURL = strings.TrimSpace(os.Getenv("COGNITO_LOGOUT_URL"))
	expectedSignerARN = strings.TrimSpace(os.Getenv("APP_ALB_ARN"))
	hostEmails = parseEmailSet(os.Getenv("HOST_EMAILS"))
	allowedEmails = parseEmailSet(os.Getenv("ALLOWED_EMAILS"))
	allowedDomains = map[string]struct{}{}
	for _, raw := range strings.Split(os.Getenv("ALLOWED_EMAIL_DOMAINS"), ",") {
		d := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "@")))
		if d != "" {
			allowedDomains[d] = struct{}{}
		}
	}
	if albAuthEnabled {
		// Bind the trusted JWS to OUR ALB, not just any ALB key in the region.
		if expectedSignerARN == "" {
			return fmt.Errorf("APP_ALB_ARN is required when APP_ALB_OIDC_AUTH=1 (binds the OIDC signature to this ALB)")
		}
		// Refuse open access: an empty allowlist behind SSO would admit any
		// authenticated Google account on the internet.
		if len(allowedEmails) == 0 && len(allowedDomains) == 0 {
			return fmt.Errorf("ALLOWED_EMAILS or ALLOWED_EMAIL_DOMAINS must be set when APP_ALB_OIDC_AUTH=1")
		}
	}
	return nil
}

func parseEmailSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		email := strings.ToLower(strings.TrimSpace(part))
		if email != "" {
			out[email] = struct{}{}
		}
	}
	return out
}

// emailAllowed reports whether an authenticated email may access the app. When
// no allowlist is configured (both sets empty), access is open. Otherwise the
// email must match ALLOWED_EMAILS or its domain must match ALLOWED_EMAIL_DOMAINS.
func emailAllowed(email string) bool {
	if len(allowedEmails) == 0 && len(allowedDomains) == 0 {
		return true
	}
	if email == "" {
		return false
	}
	if _, ok := allowedEmails[email]; ok {
		return true
	}
	if at := strings.LastIndexByte(email, '@'); at >= 0 && at < len(email)-1 {
		if _, ok := allowedDomains[email[at+1:]]; ok {
			return true
		}
	}
	return false
}

// albLogoutHandler performs a full sign-out: it clears the app session cookie
// and the ALB's own auth session cookies, then redirects to the Cognito Hosted
// UI /logout endpoint so the federated (Google) session is ended too. After
// Cognito logout the user is bounced back and must sign in again.
func albLogoutHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if cookie, err := r.Cookie(authCookieName); err == nil {
		authStore.delete(cookie.Value)
	}
	clearSessionCookie(w, r)

	// Expire the ALB's auth session cookies so it does not silently re-auth.
	// These cookies only exist behind the HTTPS ALB, so Secure is always set.
	for _, name := range []string{"AWSELBAuthSessionCookie-0", "AWSELBAuthSessionCookie-1"} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
		})
	}

	if albAuthEnabled && cognitoLogoutURL != "" {
		http.Redirect(w, r, cognitoLogoutURL, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// albOIDCClaims are the subset of Cognito/ALB claims we consume.
type albOIDCClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
	Name          string `json:"name"`
	Username      string `json:"username"`
	Subject       string `json:"sub"`
	Exp           int64  `json:"exp"`
	Iss           string `json:"iss"`
}

// albOIDCContext resolves an authenticated request from the ALB OIDC headers.
// Returns (ctx, true) only when a valid, signature-verified X-Amzn-Oidc-Data is
// present. role is host when the verified email is in HOST_EMAILS.
func albOIDCContext(r *http.Request) (requestAuthContext, bool) {
	if !albAuthEnabled {
		return requestAuthContext{}, false
	}
	rawToken := strings.TrimSpace(r.Header.Get(albOIDCDataHeader))
	if rawToken == "" {
		// No ALB OIDC header — unauthenticated path (e.g. local dev or a request
		// the ALB did not authenticate). Fall through to other auth methods.
		return requestAuthContext{}, false
	}
	claims, err := verifyALBOIDCData(rawToken)
	if err != nil {
		log.Errorf("Rejected ALB OIDC data header: %v", err)
		return requestAuthContext{}, false
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if !emailAllowed(email) {
		log.Errorf("OIDC auth: denied user not on access allowlist (email=%q)", email)
		return requestAuthContext{}, false
	}

	identity := identityFromEmail(email, claims)
	role := meetingRoleParticipant
	if _, ok := hostEmails[email]; ok && email != "" {
		role = meetingRoleHost
	}

	ctx := defaultAuthContext(identity)
	ctx.Role = role
	ctx.Email = email
	ctx.DisplayName = strings.TrimSpace(claims.Name)
	if ctx.DisplayName == "" {
		ctx.DisplayName = email
	}
	return ctx, true
}

// identityFromEmail builds a LiveKit-safe participant identity from the email
// local-part (validIdentityRe allows [a-zA-Z0-9_-]{1,64}). Falls back to the
// Cognito sub/username when the email yields nothing usable.
func identityFromEmail(email string, claims albOIDCClaims) string {
	candidate := email
	if at := strings.IndexByte(candidate, '@'); at > 0 {
		candidate = candidate[:at]
	}
	if id := sanitizeIdentity(candidate); id != "" {
		return id
	}
	if id := sanitizeIdentity(claims.Username); id != "" {
		return id
	}
	if id := sanitizeIdentity(claims.Subject); id != "" {
		return id
	}
	return "participant"
}

// sanitizeIdentity maps an arbitrary string to a LiveKit-safe identity by
// replacing characters outside [a-zA-Z0-9_-] with '_' (matching the front-end's
// cleanIdentity), capped at 64 chars. Returns "" only for empty input.
func sanitizeIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

type albOIDCHeader struct {
	Alg    string `json:"alg"`
	Kid    string `json:"kid"`
	Signer string `json:"signer"`
}

// decodeJWTSegment base64-decodes a JWS segment, tolerating both URL-safe and
// standard alphabets with or without padding. The ALB's X-Amzn-Oidc-Data
// segments are not strictly base64url (they can contain padding / standard
// chars), so a single RawURLEncoding decode fails ("illegal base64 data").
func decodeJWTSegment(seg string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if b, err := enc.DecodeString(seg); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("illegal base64 segment")
}

// verifyALBOIDCData verifies the ALB-signed X-Amzn-Oidc-Data JWS and returns its
// claims. It performs ES256 verification MANUALLY over the original compact-JWS
// segment bytes (header "." payload) rather than via a JOSE library: AWS's
// protected header field set/ordering does not round-trip through go-jose's
// canonical re-serialization, so go-jose recomputes a different signing input
// and ecdsa.Verify fails ("error in cryptographic primitive") even on a valid
// token. Verifying over the raw bytes — the same way PyJWT does — is correct and
// is full signature verification, not a downgrade.
func verifyALBOIDCData(token string) (albOIDCClaims, error) {
	var claims albOIDCClaims

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("oidc data is not a compact JWS (%d segments)", len(parts))
	}

	hdrBytes, err := decodeJWTSegment(parts[0])
	if err != nil {
		return claims, fmt.Errorf("decode oidc header: %w", err)
	}
	var hdr albOIDCHeader
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return claims, fmt.Errorf("parse oidc header: %w", err)
	}

	if strings.TrimSpace(hdr.Alg) != "ES256" {
		return claims, fmt.Errorf("unexpected oidc signing alg %q (want ES256)", hdr.Alg)
	}
	kid := strings.TrimSpace(hdr.Kid)
	if kid == "" {
		return claims, fmt.Errorf("missing kid in oidc data header")
	}
	if expectedSignerARN != "" && !strings.EqualFold(strings.TrimSpace(hdr.Signer), expectedSignerARN) {
		return claims, fmt.Errorf("unexpected oidc signer %q", hdr.Signer)
	}

	pubKey, err := albKeyCache.get(kid)
	if err != nil {
		return claims, err
	}
	ecKey, ok := pubKey.(*ecdsa.PublicKey)
	if !ok {
		return claims, fmt.Errorf("oidc signing key is not ECDSA")
	}

	sigBytes, err := decodeJWTSegment(parts[2])
	if err != nil {
		return claims, fmt.Errorf("decode oidc signature: %w", err)
	}
	if len(sigBytes) != 64 {
		return claims, fmt.Errorf("unexpected ES256 signature length %d (want 64)", len(sigBytes))
	}
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	if !ecdsa.Verify(ecKey, digest[:], r, s) {
		return claims, fmt.Errorf("oidc data signature verification failed")
	}

	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		return claims, fmt.Errorf("decode oidc payload: %w", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, fmt.Errorf("decode oidc claims: %w", err)
	}
	if claims.Exp != 0 && time.Now().Unix() >= claims.Exp {
		return claims, fmt.Errorf("oidc data token expired")
	}
	if strings.TrimSpace(claims.Email) == "" {
		return claims, fmt.Errorf("oidc data missing email claim")
	}
	return claims, nil
}

// albKeyCache fetches and caches ALB regional public keys by kid.
type albKeyCacheStore struct {
	mu   sync.Mutex
	keys map[string]cachedALBKey
}

type cachedALBKey struct {
	key     any
	fetched time.Time
}

func newALBKeyCache() *albKeyCacheStore {
	return &albKeyCacheStore{keys: map[string]cachedALBKey{}}
}

func (c *albKeyCacheStore) get(kid string) (any, error) {
	c.mu.Lock()
	if entry, ok := c.keys[kid]; ok && time.Since(entry.fetched) < time.Hour {
		c.mu.Unlock()
		return entry.key, nil
	}
	c.mu.Unlock()

	key, err := fetchALBPublicKey(kid)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.keys[kid] = cachedALBKey{key: key, fetched: time.Now()}
	c.mu.Unlock()
	return key, nil
}

// fetchALBPublicKey retrieves the PEM-encoded EC public key for a given kid from
// the ALB regional public key endpoint. The region comes from AWS_REGION.
func fetchALBPublicKey(kid string) (any, error) {
	region := strings.TrimSpace(getEnvDefault("AWS_REGION", "us-east-1"))
	// kid is constrained to a UUID-like token by the ALB; guard against any
	// path traversal in the URL just in case.
	if strings.ContainsAny(kid, "/?#") {
		return nil, fmt.Errorf("invalid kid")
	}
	url := fmt.Sprintf("https://public-keys.auth.elb.%s.amazonaws.com/%s", region, kid)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- host is a fixed AWS endpoint, kid is path-sanitized.
	if err != nil {
		return nil, fmt.Errorf("fetch alb public key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alb public key endpoint returned %d", resp.StatusCode)
	}
	pemBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("read alb public key: %w", err)
	}
	key, err := parseECPublicKeyPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse alb public key: %w", err)
	}
	return key, nil
}

// parseECPublicKeyPEM parses the PEM-encoded EC public key the ALB serves for a
// given kid. ALB OIDC data is signed with ES256, so the key is an ECDSA P-256
// public key.
func parseECPublicKeyPEM(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in alb public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("alb public key is not ECDSA")
	}
	return ecKey, nil
}
