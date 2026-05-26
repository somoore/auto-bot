package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/intake"
)

// intakeSlackHandler accepts a Slack-formatted POST and turns it into an
// async-standup intake. Two body shapes are supported:
//
//   - application/x-www-form-urlencoded — standard Slack slash command
//     payload. The "text" field carries the standup body; "user_name"
//     becomes the submitter.
//   - application/json — Slack event-subscriptions / Block Kit responses.
//     The "text" or "event.text" field carries the body.
//
// Verification: X-Slack-Signature + X-Slack-Request-Timestamp are checked
// against slackSigningSecret. A missing secret rejects every request
// (safe default per the task constraint). A stale timestamp (>5 min drift)
// or a HMAC mismatch returns 401.
//
// On success the parsed Intake is stored via intakeStore and the same
// runIntakeFollowups seam fires as the JSON endpoint, so the Slack path
// produces the same agent-driven outputs as the voice / form path.
func intakeSlackHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeIntakeError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}

	if err := intake.VerifySlackSignature(
		slackSigningSecret,
		r.Header.Get("X-Slack-Request-Timestamp"),
		body,
		r.Header.Get("X-Slack-Signature"),
		time.Now(),
	); err != nil {
		// Map verification failures to 401. The error sentinel is logged
		// internally so operators can distinguish secret-missing /
		// timestamp-stale / signature-mismatch via server logs without
		// returning that distinction to the caller (an attacker probing
		// the endpoint should learn nothing).
		log.Errorf("intake/slack: signature verification failed: %v", err)
		w.Header().Set("WWW-Authenticate", "Slack-Signature")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	submitter, text, parseErr := extractSlackPayload(r.Header.Get("Content-Type"), body)
	if parseErr != nil {
		writeIntakeError(w, http.StatusBadRequest, parseErr.Error())
		return
	}
	if strings.TrimSpace(text) == "" {
		writeIntakeError(w, http.StatusBadRequest, "missing text field")
		return
	}

	parsed := intake.ParseSlackTemplate(text)
	parsed.Submitter = submitter
	parsed.Source = intake.SourceSlack
	// The Slack adapter is single-tenant for now; cmd/server runs under
	// the default tenant. Sprint 5 (hosted control plane) is where the
	// per-workspace tenant binding gets sourced from the Slack workspace
	// team_id. Note that for the gatekeeper's read of GET /intake/standup,
	// these intakes must still be scoped to the active tenant, so we
	// pin to defaultTenantID + appBoardID here.
	parsed.TenantID = defaultTenantID
	parsed.BoardID = appBoardID

	normalized, err := intake.Normalize(parsed, time.Now())
	if err != nil {
		writeIntakeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stored := intakeStore.Put(normalized)
	followups := runIntakeFollowups(stored)

	writeIntakeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"intake":   stored,
		"created":  followups.CreatedCards,
		"comments": followups.PostedComments,
	})
}

// extractSlackPayload pulls (submitter, text) out of a Slack body. The
// form-encoded shape (slash-command POSTs) carries user_name + text;
// the JSON shape carries either top-level text or event.text.
func extractSlackPayload(contentType string, body []byte) (string, string, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if ct == "application/x-www-form-urlencoded" || ct == "" {
		values, err := url.ParseQuery(string(body))
		if err == nil {
			submitter := strings.TrimSpace(values.Get("user_name"))
			if submitter == "" {
				submitter = strings.TrimSpace(values.Get("user_id"))
			}
			text := values.Get("text")
			if text != "" || submitter != "" {
				return submitter, text, nil
			}
		}
		if ct != "" {
			return "", "", fmt.Errorf("decode form body: %v", err)
		}
		// Fall through to JSON when Content-Type was blank.
	}

	var envelope struct {
		Text     string `json:"text"`
		UserName string `json:"user_name"`
		UserID   string `json:"user_id"`
		Event    *struct {
			Text string `json:"text"`
			User string `json:"user"`
		} `json:"event,omitempty"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", "", fmt.Errorf("decode slack body: %v", err)
	}
	submitter := strings.TrimSpace(envelope.UserName)
	if submitter == "" {
		submitter = strings.TrimSpace(envelope.UserID)
	}
	text := envelope.Text
	if envelope.Event != nil {
		if text == "" {
			text = envelope.Event.Text
		}
		if submitter == "" {
			submitter = strings.TrimSpace(envelope.Event.User)
		}
	}
	return submitter, text, nil
}
