package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNovaSonicBoardContextRefreshUsesApplicationDataRole(t *testing.T) {
	events := novaSonicBoardContextRefreshEvents(newKanbanBoard(), "prompt-1", "content-1")
	if len(events) != 3 {
		t.Fatalf("refresh event count = %d, want 3", len(events))
	}

	var start struct {
		Event map[string]struct {
			Role        string `json:"role"`
			Interactive bool   `json:"interactive"`
			Type        string `json:"type"`
		} `json:"event"`
	}
	if err := json.Unmarshal(events[0], &start); err != nil {
		t.Fatalf("unmarshal contentStart: %v", err)
	}
	contentStart, ok := start.Event["contentStart"]
	if !ok {
		t.Fatalf("first refresh event = %s, want contentStart", string(events[0]))
	}
	if contentStart.Role != "USER" {
		t.Fatalf("refresh role = %q, want USER so Bedrock does not see duplicate SYSTEM content", contentStart.Role)
	}
	if contentStart.Interactive {
		t.Fatal("refresh content is interactive; want application-supplied non-interactive data")
	}
	if contentStart.Type != "TEXT" {
		t.Fatalf("refresh type = %q, want TEXT", contentStart.Type)
	}

	var text struct {
		Event map[string]struct {
			Content string `json:"content"`
		} `json:"event"`
	}
	if err := json.Unmarshal(events[1], &text); err != nil {
		t.Fatalf("unmarshal textInput: %v", err)
	}
	content := text.Event["textInput"].Content
	for _, required := range []string{"Application-supplied", "untrusted data", "Current sanitized Kanban board JSON"} {
		if !strings.Contains(content, required) {
			t.Fatalf("refresh content missing %q: %s", required, content)
		}
	}
}
