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

type meetingReportStore interface {
	SaveMeetingReport(ctx context.Context, report meetingIntelligenceReport) error
	LoadMeetingReport(ctx context.Context, boardID string, meetingID string) (meetingIntelligenceReport, bool, error)
	ListMeetingReports(ctx context.Context, boardID string, limit int) ([]meetingReportSummary, error)
}

type agentRunStore interface {
	SaveAgentRun(ctx context.Context, boardID string, run agentRun) error
	LoadAgentRun(ctx context.Context, boardID string, runID string) (agentRun, bool, error)
	ListAgentRuns(ctx context.Context, boardID string, limit int) ([]agentRun, error)
}

type boardEventRecord struct {
	BoardID        string         `json:"board_id"`
	EventID        string         `json:"event_id,omitempty"`
	OccurredAt     string         `json:"occurred_at"`
	ToolName       string         `json:"tool_name"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
	SequenceNumber int64          `json:"sequence_number"`
	Source         string         `json:"source,omitempty"`
	Actor          string         `json:"actor,omitempty"`
	RiskLevel      string         `json:"risk_level,omitempty"`
	ConfirmationID string         `json:"confirmation_id,omitempty"`
	UndoOf         string         `json:"undo_of,omitempty"`
	Summary        string         `json:"summary,omitempty"`
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
		`CREATE TABLE IF NOT EXISTS meeting_reports (
				board_id TEXT NOT NULL,
				meeting_id TEXT NOT NULL,
				ended_at TEXT NOT NULL,
				generated_at TEXT NOT NULL,
				report_json TEXT NOT NULL,
				PRIMARY KEY(board_id, meeting_id)
			)`,
		`CREATE INDEX IF NOT EXISTS meeting_reports_board_id_ended_at ON meeting_reports(board_id, ended_at DESC)`,
		`CREATE TABLE IF NOT EXISTS agent_runs (
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
		`CREATE INDEX IF NOT EXISTS agent_runs_board_id_updated_at ON agent_runs(board_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS agent_runs_board_id_card_id ON agent_runs(board_id, card_id)`,
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

func (store *sqliteBoardStore) SaveMeetingReport(ctx context.Context, report meetingIntelligenceReport) error {
	if report.BoardID == "" || report.MeetingID == "" {
		return fmt.Errorf("meeting report requires board_id and meeting_id")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	endedAt := report.EndedAt
	if endedAt == "" {
		endedAt = report.GeneratedAt
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO meeting_reports(board_id, meeting_id, ended_at, generated_at, report_json)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(board_id, meeting_id) DO UPDATE SET
			ended_at = excluded.ended_at,
			generated_at = excluded.generated_at,
			report_json = excluded.report_json
	`, report.BoardID, report.MeetingID, endedAt, report.GeneratedAt, string(raw))
	return err
}

func (store *sqliteBoardStore) LoadMeetingReport(ctx context.Context, boardID string, meetingID string) (meetingIntelligenceReport, bool, error) {
	var raw string
	err := store.db.QueryRowContext(ctx, `
		SELECT report_json
		FROM meeting_reports
		WHERE board_id = ? AND meeting_id = ?
	`, boardID, meetingID).Scan(&raw)
	if err == sql.ErrNoRows {
		return meetingIntelligenceReport{}, false, nil
	}
	if err != nil {
		return meetingIntelligenceReport{}, false, err
	}
	var report meetingIntelligenceReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return meetingIntelligenceReport{}, false, fmt.Errorf("decode meeting report: %w", err)
	}
	return report, true, nil
}

func (store *sqliteBoardStore) ListMeetingReports(ctx context.Context, boardID string, limit int) ([]meetingReportSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT report_json
		FROM meeting_reports
		WHERE board_id = ?
		ORDER BY ended_at DESC, generated_at DESC
		LIMIT ?
	`, boardID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]meetingReportSummary, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var report meetingIntelligenceReport
		if err := json.Unmarshal([]byte(raw), &report); err != nil {
			return nil, fmt.Errorf("decode meeting report summary: %w", err)
		}
		summaries = append(summaries, report.SummaryView())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summaries, nil
}

func (store *sqliteBoardStore) SaveAgentRun(ctx context.Context, boardID string, run agentRun) error {
	if run.RunID == "" {
		return fmt.Errorf("agent run requires run_id")
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	updatedAt := run.UpdatedAt
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	createdAt := run.CreatedAt
	if createdAt == "" {
		createdAt = updatedAt
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO agent_runs(board_id, run_id, card_id, status, specialist, created_at, updated_at, run_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(board_id, run_id) DO UPDATE SET
			card_id = excluded.card_id,
			status = excluded.status,
			specialist = excluded.specialist,
			updated_at = excluded.updated_at,
			run_json = excluded.run_json
	`, boardID, run.RunID, run.CardID, string(run.Status), run.Specialist, createdAt, updatedAt, string(raw))
	return err
}

func (store *sqliteBoardStore) LoadAgentRun(ctx context.Context, boardID string, runID string) (agentRun, bool, error) {
	var raw string
	err := store.db.QueryRowContext(ctx, `
		SELECT run_json
		FROM agent_runs
		WHERE board_id = ? AND run_id = ?
	`, boardID, runID).Scan(&raw)
	if err == sql.ErrNoRows {
		return agentRun{}, false, nil
	}
	if err != nil {
		return agentRun{}, false, err
	}
	var run agentRun
	if err := json.Unmarshal([]byte(raw), &run); err != nil {
		return agentRun{}, false, fmt.Errorf("decode agent run: %w", err)
	}
	return run, true, nil
}

func (store *sqliteBoardStore) ListAgentRuns(ctx context.Context, boardID string, limit int) ([]agentRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT run_json
		FROM agent_runs
		WHERE board_id = ?
		ORDER BY updated_at DESC
		LIMIT ?
	`, boardID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]agentRun, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var run agentRun
		if err := json.Unmarshal([]byte(raw), &run); err != nil {
			return nil, fmt.Errorf("decode agent run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func (store *sqliteBoardStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}
