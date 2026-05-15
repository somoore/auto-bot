package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteBoardStorePersistsSnapshotsAndEvents(t *testing.T) {
	store, err := newSQLiteBoardStore(filepath.Join(t.TempDir(), "board.sqlite"))
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error: %v", err)
	}
	defer store.Close()

	board, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("newPersistentKanbanBoard returned error: %v", err)
	}
	result, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Persisted ticket","notes":"Stored in SQLite","tags":["db"],"status":"In Progress"}`)
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mark board changed")
	}
	created := result["card"].(kanbanCard)

	reloaded, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("reload board returned error: %v", err)
	}
	state := reloaded.SnapshotState()
	if state.SequenceNumber != board.SnapshotState().SequenceNumber {
		t.Fatalf("reloaded sequence = %d, want %d", state.SequenceNumber, board.SnapshotState().SequenceNumber)
	}
	var found bool
	for _, card := range state.Cards {
		if card.ID == created.ID && card.Title == "Persisted ticket" && card.Status == kanbanStatusInProgress {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reloaded state did not include created card: %+v", state.Cards)
	}

	var eventCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM board_events WHERE board_id = ?`, "team-board").Scan(&eventCount); err != nil {
		t.Fatalf("count board events: %v", err)
	}
	if eventCount < 2 {
		t.Fatalf("eventCount = %d, want at least initial snapshot and create event", eventCount)
	}
}
