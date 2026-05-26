package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/somoore/auto-bot/internal/board"
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

// TestActorUnmarshalRejectsEmptyJSON pins the SE-1 F1 contract: an Actor
// JSON payload that carries no identifier (no id, no displayName, no email)
// must not deserialize into a fabricated Human. Before the fix `{}`
// produced Actor{Kind:Human, ID:"", DisplayName:"", Email:""} — a phantom
// human assignee with no anchor in any identity system. The fix returns
// ErrInvalidActor in that case so callers can fail closed.
func TestActorUnmarshalRejectsEmptyJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"empty object", []byte(`{}`)},
		{"kind only", []byte(`{"kind":"human"}`)},
		{"all blank strings", []byte(`{"kind":"human","id":"","displayName":"","email":""}`)},
		{"whitespace identifiers", []byte(`{"kind":"human","id":"   ","displayName":"\t","email":" \n "}`)},
		{"legacy User shape empty", []byte(`{"accountId":"","displayName":"","emailAddress":"","active":false}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var actor kanbanActor
			err := json.Unmarshal(tc.raw, &actor)
			if err == nil {
				t.Fatalf("unmarshal(%s) returned nil error; actor = %#v, want ErrInvalidActor", tc.raw, actor)
			}
			if !errors.Is(err, board.ErrInvalidActor) {
				t.Fatalf("unmarshal(%s) error = %v, want errors.Is(err, board.ErrInvalidActor)", tc.raw, err)
			}
		})
	}
}

// TestActorUnmarshalAcceptsLegacyUserShape is the inverse pin: the legacy
// User shape (no "kind" discriminator) with at least one identifier still
// promotes cleanly to Actor{Kind:Human}. SE-1 F1's empty-payload guard
// must not break the migration path for pre-Actor snapshots that have
// real users in them.
func TestActorUnmarshalAcceptsLegacyUserShape(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want kanbanActor
	}{
		{
			"accountId only",
			[]byte(`{"accountId":"account-legacy"}`),
			kanbanActor{Kind: kanbanActorKindHuman, ID: "account-legacy"},
		},
		{
			"displayName only",
			[]byte(`{"displayName":"Scott Moore"}`),
			kanbanActor{Kind: kanbanActorKindHuman, DisplayName: "Scott Moore"},
		},
		{
			"emailAddress only",
			[]byte(`{"emailAddress":"scott@example.com"}`),
			kanbanActor{Kind: kanbanActorKindHuman, Email: "scott@example.com"},
		},
		{
			"all three",
			[]byte(`{"accountId":"account-1","displayName":"Scott","emailAddress":"scott@example.com","active":true}`),
			kanbanActor{Kind: kanbanActorKindHuman, ID: "account-1", DisplayName: "Scott", Email: "scott@example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var actor kanbanActor
			if err := json.Unmarshal(tc.raw, &actor); err != nil {
				t.Fatalf("unmarshal(%s) returned error: %v", tc.raw, err)
			}
			if actor.Kind != tc.want.Kind || actor.ID != tc.want.ID || actor.DisplayName != tc.want.DisplayName || actor.Email != tc.want.Email {
				t.Fatalf("actor = %+v, want %+v", actor, tc.want)
			}
		})
	}
}
