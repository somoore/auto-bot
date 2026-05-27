package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testKID1 = "k1"
	testKID2 = "k2"
)

// Two distinct 32-byte keys used across the table-driven tests. The
// second key drives the kid-rotation case (token signed under k1 must
// still verify against a verifier that knows both kids, but a token
// signed under k1 must NOT verify under a verifier that only knows k2).
var (
	testKey1 = func() []byte {
		raw := make([]byte, 32)
		for i := range raw {
			raw[i] = byte(i + 1)
		}
		return raw
	}()
	testKey2 = func() []byte {
		raw := make([]byte, 32)
		for i := range raw {
			raw[i] = byte(0xff - i)
		}
		return raw
	}()
)

func newTestIssuer(t *testing.T) *Issuer {
	t.Helper()
	iss, err := NewIssuer([]SigningKey{{KeyID: testKID1, Key: testKey1}, {KeyID: testKID2, Key: testKey2}})
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return iss
}

func newTestVerifier(t *testing.T) *Verifier {
	t.Helper()
	v, err := NewVerifier([]SigningKey{{KeyID: testKID1, Key: testKey1}, {KeyID: testKID2, Key: testKey2}}, NewMemoryReplayTracker())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestIssueAndVerifyRoundtrip(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)

	token, claims, err := iss.Issue("agent:claude-code", "tenant-a", []string{ScopeBoardRead, ScopeCardWrite}, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if claims.JTI == "" {
		t.Errorf("Issue returned empty JTI")
	}
	if claims.IssuedAt == 0 || claims.ExpiresAt == 0 {
		t.Errorf("iat/exp not populated: %+v", claims)
	}

	got, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "agent:claude-code" {
		t.Errorf("sub = %q, want agent:claude-code", got.Subject)
	}
	if got.TenantID != "tenant-a" {
		t.Errorf("tenant_id = %q, want tenant-a", got.TenantID)
	}
	if got.Issuer != tokenIssuer {
		t.Errorf("iss = %q, want %q", got.Issuer, tokenIssuer)
	}
	if got.Audience != tokenAudMCP {
		t.Errorf("aud = %q, want %q", got.Audience, tokenAudMCP)
	}
	if !got.HasScope(ScopeBoardRead) || !got.HasScope(ScopeCardWrite) {
		t.Errorf("expected scopes %v in %v", []string{ScopeBoardRead, ScopeCardWrite}, got.Scopes)
	}
	if got.HasScope(ScopeRunsStart) {
		t.Errorf("unexpected scope %q in %v", ScopeRunsStart, got.Scopes)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)
	v.clock = func() time.Time { return time.Unix(2_000_000_000, 0) } // year 2033
	iss.clock = func() time.Time { return time.Unix(2_000_000_000-3600, 0) }

	token, _, err := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 60*time.Second)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := v.Verify(token); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("got err = %v, want ErrTokenExpired", err)
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)
	token, _, err := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Flip a bit in the signature segment.
	parts := strings.Split(token, ".")
	sig := []byte(parts[2])
	if len(sig) > 0 {
		sig[0] ^= 0x01
	}
	tampered := parts[0] + "." + parts[1] + "." + string(sig)
	if _, err := v.Verify(tampered); !errors.Is(err, ErrBadSignature) && !errors.Is(err, ErrMalformedToken) {
		t.Errorf("got err = %v, want ErrBadSignature or ErrMalformedToken", err)
	}
}

func TestVerifyRejectsWrongIssuer(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)
	token, _, err := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Rewrite the payload to claim a different issuer and re-sign with
	// the same key, then truncate the signature to break it. The point
	// is that even if an attacker re-signs with a key, the issuer check
	// kicks in. We simulate the re-sign by manually constructing a
	// payload with iss="evil-issuer" — the existing signature won't
	// match, so this also exercises ErrBadSignature ordering, but the
	// audience/issuer logic is exercised by the explicit constructed
	// payload below.
	parts := strings.Split(token, ".")
	rawPayload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c Claims
	_ = json.Unmarshal(rawPayload, &c)
	c.Issuer = "evil-issuer"
	muted, _ := json.Marshal(c)
	newPayload := base64.RawURLEncoding.EncodeToString(muted)
	// Sign correctly under k1 so we isolate the issuer check.
	sig := hmacSHA256(testKey1, []byte(parts[0]+"."+newPayload))
	mutated := parts[0] + "." + newPayload + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, err := v.Verify(mutated); !errors.Is(err, ErrWrongIssuer) {
		t.Errorf("got err = %v, want ErrWrongIssuer", err)
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)
	token, _, _ := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 5*time.Minute)
	parts := strings.Split(token, ".")
	rawPayload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c Claims
	_ = json.Unmarshal(rawPayload, &c)
	c.Audience = "phishing"
	muted, _ := json.Marshal(c)
	newPayload := base64.RawURLEncoding.EncodeToString(muted)
	sig := hmacSHA256(testKey1, []byte(parts[0]+"."+newPayload))
	mutated := parts[0] + "." + newPayload + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, err := v.Verify(mutated); !errors.Is(err, ErrWrongAudience) {
		t.Errorf("got err = %v, want ErrWrongAudience", err)
	}
}

func TestVerifyRejectsAlgConfusion(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)
	token, _, _ := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 5*time.Minute)
	// Hand-craft a header claiming alg="none" with the same kid.
	noneHeader, _ := json.Marshal(struct {
		Alg string `json:"alg"`
		KID string `json:"kid"`
	}{Alg: "none", KID: testKID1})
	parts := strings.Split(token, ".")
	newHeader := base64.RawURLEncoding.EncodeToString(noneHeader)
	mutated := newHeader + "." + parts[1] + "." + parts[2]
	if _, err := v.Verify(mutated); !errors.Is(err, ErrBadSignature) {
		t.Errorf("alg=none accepted (jwt algorithm confusion); err = %v", err)
	}
}

func TestVerifyRejectsUnknownKID(t *testing.T) {
	iss := newTestIssuer(t)
	// Verifier only knows k2 — a token signed under k1 must fail.
	v, err := NewVerifier([]SigningKey{{KeyID: testKID2, Key: testKey2}}, NewMemoryReplayTracker())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, _, _ := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 5*time.Minute)
	if _, err := v.Verify(token); !errors.Is(err, ErrBadSignature) {
		t.Errorf("unknown kid accepted; err = %v", err)
	}
}

func TestVerifyRejectsReplayedJTI(t *testing.T) {
	iss := newTestIssuer(t)
	v := newTestVerifier(t)
	token, _, _ := iss.Issue("agent:x", "tenant-a", []string{ScopeBoardRead}, 5*time.Minute)
	if _, err := v.Verify(token); err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	if _, err := v.Verify(token); !errors.Is(err, ErrReplayedJTI) {
		t.Errorf("replay accepted; err = %v", err)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	v := newTestVerifier(t)
	cases := []string{
		"",
		"only.two",
		"a.b.c.d.e",
		"not-base64.also-not.also-not",
		"a..c",
	}
	for _, c := range cases {
		if _, err := v.Verify(c); !errors.Is(err, ErrMalformedToken) && !errors.Is(err, ErrBadSignature) {
			t.Errorf("Verify(%q) err = %v; want malformed/bad sig", c, err)
		}
	}
}

func TestIssueRejectsBadInputs(t *testing.T) {
	iss := newTestIssuer(t)
	cases := []struct {
		name     string
		sub      string
		tenantID string
		scopes   []string
		ttl      time.Duration
	}{
		{"empty subject", "", "tenant-a", []string{ScopeBoardRead}, time.Minute},
		{"empty tenant", "agent:x", "", []string{ScopeBoardRead}, time.Minute},
		{"empty scopes", "agent:x", "tenant-a", []string{}, time.Minute},
		{"all-whitespace scopes", "agent:x", "tenant-a", []string{"  ", ""}, time.Minute},
		{"negative ttl", "agent:x", "tenant-a", []string{ScopeBoardRead}, -1 * time.Second},
		{"ttl over 24h", "agent:x", "tenant-a", []string{ScopeBoardRead}, 25 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := iss.Issue(c.sub, c.tenantID, c.scopes, c.ttl); err == nil {
				t.Errorf("Issue(%q,%q,%v,%s) = nil; want error", c.sub, c.tenantID, c.scopes, c.ttl)
			}
		})
	}
}

func TestParseSigningKeys(t *testing.T) {
	good := testKID1 + ":" + base64.StdEncoding.EncodeToString(testKey1) + "," +
		testKID2 + ":" + base64.StdEncoding.EncodeToString(testKey2)
	keys, err := ParseSigningKeys(good)
	if err != nil {
		t.Fatalf("ParseSigningKeys(good): %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len = %d, want 2", len(keys))
	}
	if keys[0].KeyID != testKID1 || keys[1].KeyID != testKID2 {
		t.Errorf("kids = %q,%q", keys[0].KeyID, keys[1].KeyID)
	}

	bad := []string{
		"",
		"no-colon",
		":empty-kid",
		"kid:",
		testKID1 + ":" + base64.StdEncoding.EncodeToString(testKey1) + "," + testKID1 + ":" + base64.StdEncoding.EncodeToString(testKey2), // dup kid
		testKID1 + ":!!!not-base64!!!",
		testKID1 + ":" + base64.StdEncoding.EncodeToString(make([]byte, 16)), // too short
	}
	for _, in := range bad {
		if _, err := ParseSigningKeys(in); err == nil {
			t.Errorf("ParseSigningKeys(%q) = nil; want error", in)
		}
	}
}
