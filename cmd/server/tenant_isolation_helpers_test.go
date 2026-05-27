package main

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// openLegacyPreTenantStore creates a SQLite database that mirrors the
// pre-tenant schema (no tenant_id columns, board_id-only primary keys). The
// schema text matches cmd/server/board_store.go prior to the S0.4 commit.
func openLegacyPreTenantStore(t *testing.T, path string) (*sql.DB, error) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	legacyStatements := []string{
		`CREATE TABLE board_snapshots (
			board_id TEXT PRIMARY KEY,
			state_json TEXT NOT NULL,
			sequence_number INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			saved_at TEXT NOT NULL
		)`,
		`CREATE TABLE board_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			board_id TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			event_json TEXT NOT NULL,
			state_json TEXT NOT NULL,
			sequence_number INTEGER NOT NULL
		)`,
		`CREATE INDEX board_events_board_id_id ON board_events(board_id, id)`,
		`CREATE TABLE meeting_reports (
				board_id TEXT NOT NULL,
				meeting_id TEXT NOT NULL,
				ended_at TEXT NOT NULL,
				generated_at TEXT NOT NULL,
				report_json TEXT NOT NULL,
				PRIMARY KEY(board_id, meeting_id)
			)`,
		`CREATE INDEX meeting_reports_board_id_ended_at ON meeting_reports(board_id, ended_at DESC)`,
		`CREATE TABLE agent_runs (
				board_id TEXT NOT NULL,
				run_id TEXT NOT NULL,
				card_id TEXT NOT NULL,
				status TEXT NOT NULL,
				specialist TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				run_json TEXT NOT NULL,
				PRIMARY KEY(board_id, run_id)
			)`,
		`CREATE TABLE action_replay_events (
				board_id TEXT NOT NULL,
				event_id TEXT NOT NULL,
				occurred_at TEXT NOT NULL,
				tool_name TEXT NOT NULL,
				mutation_json TEXT NOT NULL,
				state_json TEXT NOT NULL,
				sequence_number INTEGER NOT NULL,
				PRIMARY KEY(board_id, event_id)
			)`,
	}
	for _, statement := range legacyStatements {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}

// seedLegacyPreTenantRows inserts a row in every pre-tenant table so the
// migration step has something to copy.
func seedLegacyPreTenantRows(db *sql.DB) error {
	inserts := []struct {
		stmt string
		args []any
	}{
		{
			`INSERT INTO board_snapshots(board_id, state_json, sequence_number, updated_at, saved_at)
				VALUES (?, ?, ?, ?, ?)`,
			[]any{"legacy-board", `{"cards":[]}`, int64(1), "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"},
		},
		{
			`INSERT INTO board_events(board_id, occurred_at, tool_name, event_json, state_json, sequence_number)
				VALUES (?, ?, ?, ?, ?, ?)`,
			[]any{"legacy-board", "2026-01-01T00:00:00Z", "initial_board", `{}`, `{}`, int64(1)},
		},
		{
			`INSERT INTO meeting_reports(board_id, meeting_id, ended_at, generated_at, report_json)
				VALUES (?, ?, ?, ?, ?)`,
			[]any{"legacy-board", "legacy-meeting", "2026-01-01T01:00:00Z", "2026-01-01T01:00:00Z", `{}`},
		},
		{
			`INSERT INTO agent_runs(board_id, run_id, card_id, status, specialist, created_at, updated_at, run_json)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"legacy-board", "legacy-run", "card-1", "completed", "project_manager", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", `{}`},
		},
		{
			`INSERT INTO action_replay_events(board_id, event_id, occurred_at, tool_name, mutation_json, state_json, sequence_number)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
			[]any{"legacy-board", "legacy-event", "2026-01-01T00:00:00Z", "create_ticket", `{}`, `{}`, int64(1)},
		},
	}
	for _, ins := range inserts {
		if _, err := db.Exec(ins.stmt, ins.args...); err != nil {
			return err
		}
	}
	return nil
}
