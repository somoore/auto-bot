package intake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// SlackReplayWindow is the maximum drift between the Slack-provided
// timestamp and the server clock. Slack's documented guidance is
// 5 minutes; requests older than that are rejected as replays.
const SlackReplayWindow = 5 * time.Minute

// ErrSlackSecretMissing is returned by VerifySlackSignature when the
// caller passes an empty signing secret. Safe defaults reject — the
// adapter must be explicitly opted in via the SLACK_SIGNING_SECRET env.
var ErrSlackSecretMissing = errors.New("intake/slack: signing secret is required")

// ErrSlackSignatureMismatch is returned when the HMAC comparison fails.
// Returned with no detail about which side mismatched to avoid leaking
// the expected signature to an attacker.
var ErrSlackSignatureMismatch = errors.New("intake/slack: signature mismatch")

// ErrSlackTimestampStale is returned when the Slack timestamp drifts
// further than SlackReplayWindow from now. Both directions are checked
// (future timestamps reject too) so a clock-skew attacker can't replay
// stale requests by tagging them with a future time.
var ErrSlackTimestampStale = errors.New("intake/slack: timestamp outside replay window")

// ErrSlackTimestampInvalid is returned when the timestamp header is
// missing or non-numeric. Treated as a hard reject rather than a soft
// "trust the clock" — Slack always provides a Unix timestamp.
var ErrSlackTimestampInvalid = errors.New("intake/slack: timestamp header missing or invalid")

// VerifySlackSignature validates a Slack webhook signature using HMAC-
// SHA256 over the canonical "v0:<timestamp>:<body>" base string. Returns
// nil on success and a sentinel error on failure.
//
// signature is the value of the X-Slack-Signature header (form
// "v0=<hex>"). timestamp is the X-Slack-Request-Timestamp header (Unix
// seconds as a string). body is the raw request body bytes.
//
// Constant-time comparison via hmac.Equal is used to avoid timing
// side channels.
func VerifySlackSignature(secret string, timestamp string, body []byte, signature string, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return ErrSlackSecretMissing
	}
	if strings.TrimSpace(timestamp) == "" {
		return ErrSlackTimestampInvalid
	}
	ts, err := parseUnixSeconds(timestamp)
	if err != nil {
		return ErrSlackTimestampInvalid
	}
	delta := now.Sub(ts)
	if delta < 0 {
		delta = -delta
	}
	if delta > SlackReplayWindow {
		return ErrSlackTimestampStale
	}

	const prefix = "v0="
	signature = strings.TrimSpace(signature)
	if !strings.HasPrefix(signature, prefix) {
		return ErrSlackSignatureMismatch
	}
	providedHex := signature[len(prefix):]
	provided, err := hex.DecodeString(providedHex)
	if err != nil {
		return ErrSlackSignatureMismatch
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(":"))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, provided) {
		return ErrSlackSignatureMismatch
	}
	return nil
}

// ComputeSlackSignature mirrors what Slack does on the sending side. It
// exists primarily for tests but is exported so cmd/server's tests can
// construct valid signatures without re-implementing the algorithm.
func ComputeSlackSignature(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func parseUnixSeconds(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ErrSlackTimestampInvalid
	}
	var seconds int64
	var negative bool
	if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return time.Time{}, ErrSlackTimestampInvalid
		}
		seconds = seconds*10 + int64(r-'0')
	}
	if negative {
		seconds = -seconds
	}
	return time.Unix(seconds, 0), nil
}
