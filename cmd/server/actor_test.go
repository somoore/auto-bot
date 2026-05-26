package main

import (
	"encoding/json"
	"testing"
)

// TestActorRoundTripHuman verifies that a Card with a human Actor
// assignee survives a JSON round-trip without losing identity fields.
func TestActorRoundTripHuman(t *testing.T) {
	card := kanbanCard{
		ID:    "KAN-1",
		Title: "Round-trip a human actor",
		Assignee: &kanbanActor{
			Kind:        kanbanActorKindHuman,
			ID:          "account-123",
			DisplayName: "Scott Moore",
			Email:       "scott@example.com",
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded kanbanCard
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Assignee == nil {
		t.Fatalf("decoded assignee = nil, want human actor")
	}
	if decoded.Assignee.Kind != kanbanActorKindHuman {
		t.Fatalf("Kind = %q, want %q", decoded.Assignee.Kind, kanbanActorKindHuman)
	}
	if decoded.Assignee.ID != "account-123" {
		t.Fatalf("ID = %q, want account-123", decoded.Assignee.ID)
	}
	if decoded.Assignee.DisplayName != "Scott Moore" {
		t.Fatalf("DisplayName = %q, want Scott Moore", decoded.Assignee.DisplayName)
	}
	if decoded.Assignee.Email != "scott@example.com" {
		t.Fatalf("Email = %q, want scott@example.com", decoded.Assignee.Email)
	}
}

// TestActorRoundTripAgent verifies that a Card owned by an agent Actor
// survives a JSON round-trip with its agent-only fields intact.
func TestActorRoundTripAgent(t *testing.T) {
	card := kanbanCard{
		ID: "KAN-2",
		Assignee: &kanbanActor{
			Kind:         kanbanActorKindAgent,
			ID:           "agent:swe-1:tenant-alpha",
			DisplayName:  "swe-1",
			AgentProfile: "swe-1",
			OwnerHumanID: "account-123",
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded kanbanCard
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Assignee == nil || decoded.Assignee.Kind != kanbanActorKindAgent {
		t.Fatalf("decoded assignee = %+v, want agent actor", decoded.Assignee)
	}
	if decoded.Assignee.AgentProfile != "swe-1" {
		t.Fatalf("AgentProfile = %q, want swe-1", decoded.Assignee.AgentProfile)
	}
	if decoded.Assignee.OwnerHumanID != "account-123" {
		t.Fatalf("OwnerHumanID = %q, want account-123", decoded.Assignee.OwnerHumanID)
	}
	if decoded.Assignee.ID != "agent:swe-1:tenant-alpha" {
		t.Fatalf("ID = %q, want agent:swe-1:tenant-alpha", decoded.Assignee.ID)
	}
}

// TestActorUnmarshalLegacyUserShape verifies that pre-Actor snapshots
// load cleanly: a card whose assignee was serialized as a User
// (accountId / displayName / emailAddress, no kind discriminator) must
// unmarshal as an Actor{Kind:Human}.
func TestActorUnmarshalLegacyUserShape(t *testing.T) {
	legacy := []byte(`{
		"id": "KAN-3",
		"status": "Backlog",
		"title": "Legacy snapshot",
		"notes": "",
		"tags": null,
		"assignee": {
			"accountId": "account-legacy",
			"displayName": "Legacy User",
			"emailAddress": "legacy@example.com",
			"active": true
		}
	}`)
	var card kanbanCard
	if err := json.Unmarshal(legacy, &card); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if card.Assignee == nil {
		t.Fatalf("legacy assignee = nil, want promoted human actor")
	}
	if card.Assignee.Kind != kanbanActorKindHuman {
		t.Fatalf("Kind = %q, want %q (legacy User must promote to Human)", card.Assignee.Kind, kanbanActorKindHuman)
	}
	if card.Assignee.ID != "account-legacy" {
		t.Fatalf("ID = %q, want account-legacy (must adopt accountId)", card.Assignee.ID)
	}
	if card.Assignee.DisplayName != "Legacy User" {
		t.Fatalf("DisplayName = %q, want Legacy User", card.Assignee.DisplayName)
	}
	if card.Assignee.Email != "legacy@example.com" {
		t.Fatalf("Email = %q, want legacy@example.com (must adopt emailAddress)", card.Assignee.Email)
	}
}

// TestActorUnmarshalEmptyKindDefaultsHuman verifies that an Actor
// snapshot with kind=="" still loads as Human rather than producing an
// untyped actor.
func TestActorUnmarshalEmptyKindDefaultsHuman(t *testing.T) {
	raw := []byte(`{"kind":"","id":"account-x","displayName":"X"}`)
	var actor kanbanActor
	if err := json.Unmarshal(raw, &actor); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if actor.Kind != kanbanActorKindHuman {
		t.Fatalf("Kind = %q, want %q for empty-kind fallback", actor.Kind, kanbanActorKindHuman)
	}
}
