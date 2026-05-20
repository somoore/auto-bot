package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

var auditLogMu sync.Mutex

type auditEvent struct {
	Timestamp      string         `json:"timestamp"`
	Event          string         `json:"event"`
	Source         string         `json:"source"`
	Tool           string         `json:"tool,omitempty"`
	SequenceNumber int64          `json:"sequence_number"`
	BoardUpdatedAt string         `json:"board_updated_at,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
}

func auditBoardMutation(source string, toolName string, result map[string]any, state kanbanBoardState) {
	event := auditEvent{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		Event:          "board_mutation",
		Source:         source,
		Tool:           toolName,
		SequenceNumber: state.SequenceNumber,
		BoardUpdatedAt: state.UpdatedAt,
		Result:         sanitizedToolResult(result),
	}
	writeAuditEvent(event)
}

func auditBoardRefresh(source string, state kanbanBoardState) {
	event := auditEvent{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		Event:          "board_refresh",
		Source:         source,
		SequenceNumber: state.SequenceNumber,
		BoardUpdatedAt: state.UpdatedAt,
	}
	writeAuditEvent(event)
}

func writeAuditEvent(event auditEvent) {
	raw, err := json.Marshal(event)
	if err != nil {
		log.Errorf("Audit encode failed: %v", err)
		return
	}
	log.Infof("audit=%s", string(raw))

	path := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH"))
	if path == "" {
		return
	}

	auditLogMu.Lock()
	defer auditLogMu.Unlock()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 G703 -- audit log path is operator-controlled deployment configuration.
	if err != nil {
		log.Errorf("Audit log open failed: %v", err)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Errorf("Audit log close failed: %v", err)
		}
	}()

	if _, err := file.Write(append(raw, '\n')); err != nil {
		log.Errorf("Audit log write failed: %v", err)
	}
}

func sanitizedToolResult(result map[string]any) map[string]any {
	if result == nil {
		return nil
	}

	sanitized := map[string]any{}
	for _, key := range []string{"ok", "created", "moved", "updated", "deleted", "tags_added", "card_id", "status", "audit_event_id", "external_action_status", "external_action_confirmed", "api_confirmation_summary"} {
		if value, ok := result[key]; ok {
			sanitized[key] = value
		}
	}
	if status, ok := result["jira_sync"].(map[string]any); ok {
		sanitized["jira_sync"] = status
	}
	if confirmations, ok := result["external_confirmations"].([]externalActionConfirmation); ok {
		sanitized["external_confirmations"] = confirmations
	}
	if card, ok := result["card"].(kanbanCard); ok {
		sanitized["card"] = map[string]any{
			"id":     card.ID,
			"status": card.Status,
			"tags":   card.Tags,
		}
	}
	return sanitized
}
