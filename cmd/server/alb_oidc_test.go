package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"
)

// makeALBToken hand-builds an ES256 compact JWS with a raw R||S signature, the
// way the ALB does, signed by the given key.
func makeALBToken(t *testing.T, key *ecdsa.PrivateKey, header, payload map[string]any) string {
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
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Fixed-width 32-byte R||S (P-256).
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// TestVerifyALBOIDCData_Synthetic proves the manual ES256 verification path
// (segment reconstruction + r/s split + ecdsa.Verify) without any deploy or
// real token. Injects a synthetic key into the kid cache.
func TestVerifyALBOIDCData_Synthetic(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "test-kid-123"
	prev := albKeyCache
	albKeyCache = newALBKeyCache()
	albKeyCache.keys[kid] = cachedALBKey{key: &key.PublicKey, fetched: time.Now()}
	prevSigner := expectedSignerARN
	expectedSignerARN = ""
	t.Cleanup(func() { albKeyCache = prev; expectedSignerARN = prevSigner })

	header := map[string]any{"alg": "ES256", "kid": kid, "signer": "arn:aws:elasticloadbalancing:us-east-1:111:loadbalancer/app/x/y"}
	payload := map[string]any{"email": "Scott@Moore.Cloud", "name": "Scott Moore", "sub": "abc", "exp": int64(1 << 62)}
	token := makeALBToken(t, key, header, payload)

	claims, err := verifyALBOIDCData(token)
	if err != nil {
		t.Fatalf("verify failed on a valid synthetic token: %v", err)
	}
	if claims.Email != "Scott@Moore.Cloud" || claims.Name != "Scott Moore" {
		t.Errorf("claims mismatch: %+v", claims)
	}

	// Tampered payload must fail.
	bad := token[:len(token)-4] + "AAAA"
	if _, err := verifyALBOIDCData(bad); err == nil {
		t.Error("expected verification failure on tampered signature")
	}

	// Wrong key must fail.
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	albKeyCache.keys[kid] = cachedALBKey{key: &other.PublicKey, fetched: time.Now()}
	if _, err := verifyALBOIDCData(token); err == nil {
		t.Error("expected verification failure with wrong public key")
	}
	_ = big.NewInt(0)
}

func TestIdentityFromEmail(t *testing.T) {
	cases := []struct {
		email  string
		claims albOIDCClaims
		want   string
	}{
		{"scott@moore.cloud", albOIDCClaims{}, "scott"},
		{"first.last@example.com", albOIDCClaims{}, "first_last"},
		{"", albOIDCClaims{Username: "user-123"}, "user-123"},
		{"", albOIDCClaims{Subject: "abc"}, "abc"},
		{"", albOIDCClaims{}, "participant"},
	}
	for _, c := range cases {
		got := identityFromEmail(c.email, c.claims)
		if got != c.want {
			t.Errorf("identityFromEmail(%q,%+v) = %q, want %q", c.email, c.claims, got, c.want)
		}
	}
}

func TestHostAllowlistRole(t *testing.T) {
	prevEnabled := albAuthEnabled
	prevHosts := hostEmails
	t.Cleanup(func() {
		albAuthEnabled = prevEnabled
		hostEmails = prevHosts
	})

	t.Setenv("APP_ALB_OIDC_AUTH", "1")
	t.Setenv("APP_ALB_ARN", "arn:aws:elasticloadbalancing:us-east-1:111:loadbalancer/app/x/y")
	t.Setenv("ALLOWED_EMAIL_DOMAINS", "moore.cloud")
	t.Setenv("HOST_EMAILS", "Scott@Moore.Cloud, lead@example.com")
	if err := configureALBAuth(); err != nil {
		t.Fatalf("configureALBAuth: %v", err)
	}

	if !albAuthEnabled {
		t.Fatal("expected albAuthEnabled true")
	}
	// Allowlist match is case-insensitive.
	if _, ok := hostEmails["scott@moore.cloud"]; !ok {
		t.Error("expected scott@moore.cloud in host allowlist")
	}
	if _, ok := hostEmails["lead@example.com"]; !ok {
		t.Error("expected lead@example.com in host allowlist")
	}
	if _, ok := hostEmails["stranger@example.com"]; ok {
		t.Error("did not expect stranger in host allowlist")
	}
}

func TestEmailAllowed(t *testing.T) {
	prevEmails := allowedEmails
	prevDomains := allowedDomains
	t.Cleanup(func() {
		allowedEmails = prevEmails
		allowedDomains = prevDomains
	})

	// No allowlist configured -> open access.
	allowedEmails = map[string]struct{}{}
	allowedDomains = map[string]struct{}{}
	if !emailAllowed("anyone@gmail.com") {
		t.Error("expected open access when no allowlist configured")
	}

	// Domain allowlist.
	allowedEmails = map[string]struct{}{}
	allowedDomains = map[string]struct{}{"moore.cloud": {}}
	if !emailAllowed("scott@moore.cloud") {
		t.Error("expected moore.cloud domain allowed")
	}
	if emailAllowed("stranger@gmail.com") {
		t.Error("expected gmail.com denied under domain allowlist")
	}
	if emailAllowed("") {
		t.Error("expected empty email denied under allowlist")
	}

	// Exact email allowlist.
	allowedEmails = map[string]struct{}{"vip@example.com": {}}
	allowedDomains = map[string]struct{}{}
	if !emailAllowed("vip@example.com") {
		t.Error("expected exact email allowed")
	}
	if emailAllowed("other@example.com") {
		t.Error("expected non-listed email denied")
	}
}

func TestConfigureALBAuthFailsClosed(t *testing.T) {
	prev := albAuthEnabled
	t.Cleanup(func() { albAuthEnabled = prev })

	// OIDC enabled but no signer ARN -> error.
	t.Setenv("APP_ALB_OIDC_AUTH", "1")
	t.Setenv("APP_ALB_ARN", "")
	t.Setenv("ALLOWED_EMAIL_DOMAINS", "moore.cloud")
	if err := configureALBAuth(); err == nil {
		t.Error("expected error when APP_ALB_ARN is empty under OIDC")
	}

	// OIDC enabled, signer set, but no allowlist -> error (no open access).
	t.Setenv("APP_ALB_ARN", "arn:aws:elasticloadbalancing:us-east-1:111:loadbalancer/app/x/y")
	t.Setenv("ALLOWED_EMAILS", "")
	t.Setenv("ALLOWED_EMAIL_DOMAINS", "")
	t.Setenv("HOST_EMAILS", "")
	if err := configureALBAuth(); err == nil {
		t.Error("expected error when no allowlist is set under OIDC")
	}
}

func TestALBOIDCDisabledWhenFlagOff(t *testing.T) {
	prevEnabled := albAuthEnabled
	t.Cleanup(func() { albAuthEnabled = prevEnabled })

	t.Setenv("APP_ALB_OIDC_AUTH", "")
	if err := configureALBAuth(); err != nil {
		t.Fatalf("configureALBAuth: %v", err)
	}
	if albAuthEnabled {
		t.Fatal("expected albAuthEnabled false when flag unset")
	}
}
