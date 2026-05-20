package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxJiraWebhookBytes = 256 * 1024

type jiraWebhookPayload struct {
	WebhookEvent string `json:"webhookEvent"`
	Issue        *struct {
		Key string `json:"key"`
	} `json:"issue"`
}

func jiraWebhookHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if jiraSync == nil {
		http.Error(w, "jira sync is not configured", http.StatusNotFound)
		return
	}
	secret := strings.TrimSpace(jiraSync.config.WebhookSecret)
	if secret == "" {
		http.Error(w, "jira webhook secret is not configured", http.StatusNotFound)
		return
	}
	if !validJiraWebhookSecret(r, secret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload jiraWebhookPayload
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJiraWebhookBytes))
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if payload.Issue != nil && strings.TrimSpace(payload.Issue.Key) != "" {
		if err := jiraSync.client.validateIssueKey(payload.Issue.Key); err != nil {
			log.Errorf("Rejected Jira webhook outside project: %v", err)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := jiraSync.RefreshFromJira(ctx, "jira-webhook"); err != nil {
		log.Errorf("Jira webhook refresh failed: %v", err)
		http.Error(w, "refresh failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func validJiraWebhookSecret(r *http.Request, secret string) bool {
	if secureTokenEqual(r.Header.Get("X-Auto-Bot-Jira-Webhook-Secret"), secret) {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return secureTokenEqual(strings.TrimSpace(auth[len("Bearer "):]), secret)
	}
	return false
}
