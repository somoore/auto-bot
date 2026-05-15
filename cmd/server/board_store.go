package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type boardStore interface {
	LoadBoard(ctx context.Context, boardID string) (kanbanBoardState, bool, error)
	SaveSnapshot(ctx context.Context, boardID string, state kanbanBoardState) error
	AppendEvent(ctx context.Context, boardID string, event boardEventRecord, state kanbanBoardState) error
	Close() error
}

type boardEventRecord struct {
	BoardID        string         `json:"board_id"`
	OccurredAt     string         `json:"occurred_at"`
	ToolName       string         `json:"tool_name"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
	SequenceNumber int64          `json:"sequence_number"`
}

type sqliteBoardStore struct {
	db *sql.DB
}

func setupBoardStore() (boardStore, error) {
	path := strings.TrimSpace(os.Getenv("BOARD_SQLITE_PATH"))
	if path == "" {
		return nil, nil
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create board sqlite dir: %w", err)
		}
	}
	return newSQLiteBoardStore(path)
}

func newSQLiteBoardStore(path string) (*sqliteBoardStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &sqliteBoardStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *sqliteBoardStore) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS board_snapshots (
			board_id TEXT PRIMARY KEY,
			state_json TEXT NOT NULL,
			sequence_number INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			saved_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS board_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			board_id TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			event_json TEXT NOT NULL,
			state_json TEXT NOT NULL,
			sequence_number INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS board_events_board_id_id ON board_events(board_id, id)`,
	}
	for _, statement := range statements {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize board sqlite store: %w", err)
		}
	}
	return nil
}

func (store *sqliteBoardStore) LoadBoard(ctx context.Context, boardID string) (kanbanBoardState, bool, error) {
	var raw string
	err := store.db.QueryRowContext(ctx, `SELECT state_json FROM board_snapshots WHERE board_id = ?`, boardID).Scan(&raw)
	if err == sql.ErrNoRows {
		return kanbanBoardState{}, false, nil
	}
	if err != nil {
		return kanbanBoardState{}, false, err
	}
	var state kanbanBoardState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return kanbanBoardState{}, false, fmt.Errorf("decode board snapshot: %w", err)
	}
	return state, true, nil
}

func (store *sqliteBoardStore) SaveSnapshot(ctx context.Context, boardID string, state kanbanBoardState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	updatedAt := state.UpdatedAt
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO board_snapshots(board_id, state_json, sequence_number, updated_at, saved_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(board_id) DO UPDATE SET
			state_json = excluded.state_json,
			sequence_number = excluded.sequence_number,
			updated_at = excluded.updated_at,
			saved_at = excluded.saved_at
	`, boardID, string(stateJSON), state.SequenceNumber, updatedAt, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (store *sqliteBoardStore) AppendEvent(ctx context.Context, boardID string, event boardEventRecord, state kanbanBoardState) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO board_events(board_id, occurred_at, tool_name, event_json, state_json, sequence_number)
		VALUES (?, ?, ?, ?, ?, ?)
	`, boardID, event.OccurredAt, event.ToolName, string(eventJSON), string(stateJSON), state.SequenceNumber)
	return err
}

func (store *sqliteBoardStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}
