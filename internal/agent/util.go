package agent

import (
	"strings"
	"time"
)

// nowRFC3339Nano returns the current UTC time formatted in RFC3339 with
// nanosecond precision. Used to stamp Checkpoint timestamps so the timeline
// sorts deterministically regardless of host clock skew.
func nowRFC3339Nano() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// truncateString trims surrounding whitespace and clamps the value to limit
// bytes. A non-positive limit disables clamping. The trim+clamp pair keeps
// Run timeline fields (step, message, repo, branch) bounded for both UI
// presentation and durable storage.
func truncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit > 0 && len(value) > limit {
		return value[:limit]
	}
	return value
}
