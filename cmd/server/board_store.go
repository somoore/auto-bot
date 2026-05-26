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
	LoadBoard(ctx context.Context, tenantID string, boardID string) (kanbanBoardState, bool, error)
	SaveSnapshot(ctx context.Context, tenantID string, boardID string, state kanbanBoardState) error
	AppendEvent(ctx context.Context, tenantID string, boardID string, event boardEventRecord, state kanbanBoardState) error
	Close() error
}

type meetingReportStore interface {
	// SaveMeetingReport derives tenant scope from report.TenantID (defaults to
	// "default" when unset) so callers do not need to thread it explicitly.
	SaveMeetingReport(ctx context.Context, report meetingIntelligenceReport) error
	LoadMeetingReport(ctx context.Context, tenantID string, boardID string, meetingID string) (meetingIntelligenceReport, bool, error)
	ListMeetingReports(ctx context.Context, tenantID string, boardID string, limit int) ([]meetingReportSummary, error)
}

type agentRunStore interface {
	SaveAgentRun(ctx context.Context, tenantID string, boardID string, run agentRun) error
	LoadAgentRun(ctx context.Context, tenantID string, boardID string, runID string) (agentRun, bool, error)
	ListAgentRuns(ctx context.Context, tenantID string, boardID string, limit int) ([]agentRun, error)
}

type mutationLedgerStore interface {
	SaveMutationRecord(ctx context.Context, tenantID string, boardID string, record boardMutationRecord, state kanbanBoardState) error
	UpdateMutationRecord(ctx context.Context, tenantID string, boardID string, record boardMutationRecord) error
	ListMutationRecords(ctx context.Context, tenantID string, boardID string, limit int) ([]boardMutationRecord, error)
}

type boardEventRecord struct {
	TenantID       string         `json:"tenant_id,omitempty"`
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
		if err := os.MkdirAll(dir, 0o700); err != nil { // #nosec G703 -- SQLite path is operator-controlled deployment configuration.
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
	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	}
	for _, statement := range pragmas {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize board sqlite store: %w", err)
		}
	}
	// New (tenant-scoped) schema. CREATE TABLE IF NOT EXISTS is a no-op
	// against an existing pre-tenant database, so the migration step below
	// rewrites the legacy tables into the new shape on first open.
	creates := []string{
		`CREATE TABLE IF NOT EXISTS board_snapshots (
			tenant_id TEXT NOT NULL DEFAULT 'default',
			board_id TEXT NOT NULL,
			state_json TEXT NOT NULL,
			sequence_number INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			saved_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, board_id)
		)`,
		`CREATE TABLE IF NOT EXISTS board_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id TEXT NOT NULL DEFAULT 'default',
			board_id TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			event_json TEXT NOT NULL,
			state_json TEXT NOT NULL,
			sequence_number INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS board_events_tenant_board_occurred ON board_events(tenant_id, board_id, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS board_events_board_id_id ON board_events(board_id, id)`,
		`CREATE TABLE IF NOT EXISTS meeting_reports (
				tenant_id TEXT NOT NULL DEFAULT 'default',
				board_id TEXT NOT NULL,
				meeting_id TEXT NOT NULL,
				ended_at TEXT NOT NULL,
				generated_at TEXT NOT NULL,
				report_json TEXT NOT NULL,
				PRIMARY KEY(tenant_id, board_id, meeting_id)
			)`,
		`CREATE INDEX IF NOT EXISTS meeting_reports_tenant_board_ended_at ON meeting_reports(tenant_id, board_id, ended_at DESC)`,
		`CREATE TABLE IF NOT EXISTS agent_runs (
				tenant_id TEXT NOT NULL DEFAULT 'default',
				board_id TEXT NOT NULL,
				run_id TEXT NOT NULL,
				card_id TEXT NOT NULL,
				status TEXT NOT NULL,
				specialist TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				run_json TEXT NOT NULL,
				PRIMARY KEY(tenant_id, board_id, run_id)
			)`,
		`CREATE INDEX IF NOT EXISTS agent_runs_tenant_board_updated ON agent_runs(tenant_id, board_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS agent_runs_tenant_board_card ON agent_runs(tenant_id, board_id, card_id)`,
		`CREATE TABLE IF NOT EXISTS action_replay_events (
				tenant_id TEXT NOT NULL DEFAULT 'default',
				board_id TEXT NOT NULL,
				event_id TEXT NOT NULL,
				occurred_at TEXT NOT NULL,
				tool_name TEXT NOT NULL,
				mutation_json TEXT NOT NULL,
				state_json TEXT NOT NULL,
				sequence_number INTEGER NOT NULL,
				PRIMARY KEY(tenant_id, board_id, event_id)
			)`,
		`CREATE INDEX IF NOT EXISTS action_replay_events_tenant_board_occurred ON action_replay_events(tenant_id, board_id, occurred_at DESC)`,
	}
	// Migrate pre-tenant databases before issuing the new CREATE statements so
	// that older tables get rewritten with tenant_id='default' on every row.
	if err := store.migrateTenantSchema(ctx); err != nil {
		return fmt.Errorf("migrate board sqlite store: %w", err)
	}
	for _, statement := range creates {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize board sqlite store: %w", err)
		}
	}
	return nil
}

// migrateTenantSchema rewrites pre-tenant tables (no tenant_id column) into the
// new tenant-scoped shape. Every legacy row is assigned tenant_id='default'.
// PRAGMA user_version bumps from 0 -> 1 on success so subsequent opens skip
// the rewrite. The migration runs in one transaction; partial failure rolls
// back and the caller sees the underlying error.
func (store *sqliteBoardStore) migrateTenantSchema(ctx context.Context) error {
	var version int
	if err := store.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if version >= 1 {
		return nil
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	migrations := []struct {
		table          string
		legacyCols     string
		newSchema      string
		copySelectCols string // columns selected from legacy table, in the same order as newCols
		newCols        string // columns inserted into new table (prefix tenant_id)
	}{
		{
			table:          "board_snapshots",
			legacyCols:     "board_id, state_json, sequence_number, updated_at, saved_at",
			newSchema:      `CREATE TABLE board_snapshots (tenant_id TEXT NOT NULL DEFAULT 'default', board_id TEXT NOT NULL, state_json TEXT NOT NULL, sequence_number INTEGER NOT NULL, updated_at TEXT NOT NULL, saved_at TEXT NOT NULL, PRIMARY KEY(tenant_id, board_id))`,
			copySelectCols: "'default', board_id, state_json, sequence_number, updated_at, saved_at",
			newCols:        "tenant_id, board_id, state_json, sequence_number, updated_at, saved_at",
		},
		{
			table:          "board_events",
			legacyCols:     "id, board_id, occurred_at, tool_name, event_json, state_json, sequence_number",
			newSchema:      `CREATE TABLE board_events (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id TEXT NOT NULL DEFAULT 'default', board_id TEXT NOT NULL, occurred_at TEXT NOT NULL, tool_name TEXT NOT NULL, event_json TEXT NOT NULL, state_json TEXT NOT NULL, sequence_number INTEGER NOT NULL)`,
			copySelectCols: "id, 'default', board_id, occurred_at, tool_name, event_json, state_json, sequence_number",
			newCols:        "id, tenant_id, board_id, occurred_at, tool_name, event_json, state_json, sequence_number",
		},
		{
			table:          "meeting_reports",
			legacyCols:     "board_id, meeting_id, ended_at, generated_at, report_json",
			newSchema:      `CREATE TABLE meeting_reports (tenant_id TEXT NOT NULL DEFAULT 'default', board_id TEXT NOT NULL, meeting_id TEXT NOT NULL, ended_at TEXT NOT NULL, generated_at TEXT NOT NULL, report_json TEXT NOT NULL, PRIMARY KEY(tenant_id, board_id, meeting_id))`,
			copySelectCols: "'default', board_id, meeting_id, ended_at, generated_at, report_json",
			newCols:        "tenant_id, board_id, meeting_id, ended_at, generated_at, report_json",
		},
		{
			table:          "agent_runs",
			legacyCols:     "board_id, run_id, card_id, status, specialist, created_at, updated_at, run_json",
			newSchema:      `CREATE TABLE agent_runs (tenant_id TEXT NOT NULL DEFAULT 'default', board_id TEXT NOT NULL, run_id TEXT NOT NULL, card_id TEXT NOT NULL, status TEXT NOT NULL, specialist TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, run_json TEXT NOT NULL, PRIMARY KEY(tenant_id, board_id, run_id))`,
			copySelectCols: "'default', board_id, run_id, card_id, status, specialist, created_at, updated_at, run_json",
			newCols:        "tenant_id, board_id, run_id, card_id, status, specialist, created_at, updated_at, run_json",
		},
		{
			table:          "action_replay_events",
			legacyCols:     "board_id, event_id, occurred_at, tool_name, mutation_json, state_json, sequence_number",
			newSchema:      `CREATE TABLE action_replay_events (tenant_id TEXT NOT NULL DEFAULT 'default', board_id TEXT NOT NULL, event_id TEXT NOT NULL, occurred_at TEXT NOT NULL, tool_name TEXT NOT NULL, mutation_json TEXT NOT NULL, state_json TEXT NOT NULL, sequence_number INTEGER NOT NULL, PRIMARY KEY(tenant_id, board_id, event_id))`,
			copySelectCols: "'default', board_id, event_id, occurred_at, tool_name, mutation_json, state_json, sequence_number",
			newCols:        "tenant_id, board_id, event_id, occurred_at, tool_name, mutation_json, state_json, sequence_number",
		},
	}
	for _, m := range migrations {
		hasTenant, err := txTableHasColumn(ctx, tx, m.table, "tenant_id")
		if err != nil {
			return err
		}
		if hasTenant {
			continue
		}
		exists, err := txTableExists(ctx, tx, m.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		legacyName := m.table + "_legacy_pretenant"
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, m.table, legacyName)); err != nil {
			return fmt.Errorf("rename legacy %s: %w", m.table, err)
		}
		if _, err := tx.ExecContext(ctx, m.newSchema); err != nil {
			return fmt.Errorf("create new %s: %w", m.table, err)
		}
		// #nosec G201 -- m.table/m.newCols/m.copySelectCols are static literals in this file; no user input.
		copyStmt := fmt.Sprintf(`INSERT INTO %s(%s) SELECT %s FROM %s`, m.table, m.newCols, m.copySelectCols, legacyName)
		if _, err := tx.ExecContext(ctx, copyStmt); err != nil {
			return fmt.Errorf("copy %s rows: %w", m.table, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s`, legacyName)); err != nil {
			return fmt.Errorf("drop legacy %s: %w", m.table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
		return fmt.Errorf("bump user_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func txTableExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var found string
	err := tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found == name, nil
}

func txTableHasColumn(ctx context.Context, tx *sql.Tx, table string, column string) (bool, error) {
	exists, err := txTableExists(ctx, tx, table)
	if err != nil || !exists {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (store *sqliteBoardStore) LoadBoard(ctx context.Context, tenantID string, boardID string) (kanbanBoardState, bool, error) {
	tenantID = normalizeTenantID(tenantID)
	var raw string
	err := store.db.QueryRowContext(ctx, `SELECT state_json FROM board_snapshots WHERE tenant_id = ? AND board_id = ?`, tenantID, boardID).Scan(&raw)
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

func (store *sqliteBoardStore) SaveSnapshot(ctx context.Context, tenantID string, boardID string, state kanbanBoardState) error {
	tenantID = normalizeTenantID(tenantID)
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	updatedAt := state.UpdatedAt
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO board_snapshots(tenant_id, board_id, state_json, sequence_number, updated_at, saved_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, board_id) DO UPDATE SET
			state_json = excluded.state_json,
			sequence_number = excluded.sequence_number,
			updated_at = excluded.updated_at,
			saved_at = excluded.saved_at
	`, tenantID, boardID, string(stateJSON), state.SequenceNumber, updatedAt, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (store *sqliteBoardStore) AppendEvent(ctx context.Context, tenantID string, boardID string, event boardEventRecord, state kanbanBoardState) error {
	tenantID = normalizeTenantID(tenantID)
	if event.TenantID == "" {
		event.TenantID = tenantID
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO board_events(tenant_id, board_id, occurred_at, tool_name, event_json, state_json, sequence_number)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, tenantID, boardID, event.OccurredAt, event.ToolName, string(eventJSON), string(stateJSON), state.SequenceNumber)
	return err
}

func (store *sqliteBoardStore) SaveMeetingReport(ctx context.Context, report meetingIntelligenceReport) error {
	if report.BoardID == "" || report.MeetingID == "" {
		return fmt.Errorf("meeting report requires board_id and meeting_id")
	}
	tenantID := normalizeTenantID(report.TenantID)
	report.TenantID = tenantID
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	endedAt := report.EndedAt
	if endedAt == "" {
		endedAt = report.GeneratedAt
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO meeting_reports(tenant_id, board_id, meeting_id, ended_at, generated_at, report_json)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, board_id, meeting_id) DO UPDATE SET
			ended_at = excluded.ended_at,
			generated_at = excluded.generated_at,
			report_json = excluded.report_json
	`, tenantID, report.BoardID, report.MeetingID, endedAt, report.GeneratedAt, string(raw))
	return err
}

func (store *sqliteBoardStore) LoadMeetingReport(ctx context.Context, tenantID string, boardID string, meetingID string) (meetingIntelligenceReport, bool, error) {
	tenantID = normalizeTenantID(tenantID)
	var raw string
	err := store.db.QueryRowContext(ctx, `
		SELECT report_json
		FROM meeting_reports
		WHERE tenant_id = ? AND board_id = ? AND meeting_id = ?
	`, tenantID, boardID, meetingID).Scan(&raw)
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

func (store *sqliteBoardStore) ListMeetingReports(ctx context.Context, tenantID string, boardID string, limit int) (summaries []meetingReportSummary, err error) {
	tenantID = normalizeTenantID(tenantID)
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT report_json
		FROM meeting_reports
		WHERE tenant_id = ? AND board_id = ?
		ORDER BY ended_at DESC, generated_at DESC
		LIMIT ?
	`, tenantID, boardID, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close meeting report rows: %w", closeErr)
		}
	}()

	summaries = make([]meetingReportSummary, 0)
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

func (store *sqliteBoardStore) SaveAgentRun(ctx context.Context, tenantID string, boardID string, run agentRun) error {
	tenantID = normalizeTenantID(tenantID)
	if run.RunID == "" {
		return fmt.Errorf("agent run requires run_id")
	}
	if run.TenantID == "" {
		run.TenantID = tenantID
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
		INSERT INTO agent_runs(tenant_id, board_id, run_id, card_id, status, specialist, created_at, updated_at, run_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, board_id, run_id) DO UPDATE SET
			card_id = excluded.card_id,
			status = excluded.status,
			specialist = excluded.specialist,
			updated_at = excluded.updated_at,
			run_json = excluded.run_json
	`, tenantID, boardID, run.RunID, run.CardID, string(run.Status), run.Specialist, createdAt, updatedAt, string(raw))
	return err
}

func (store *sqliteBoardStore) LoadAgentRun(ctx context.Context, tenantID string, boardID string, runID string) (agentRun, bool, error) {
	tenantID = normalizeTenantID(tenantID)
	var raw string
	err := store.db.QueryRowContext(ctx, `
		SELECT run_json
		FROM agent_runs
		WHERE tenant_id = ? AND board_id = ? AND run_id = ?
	`, tenantID, boardID, runID).Scan(&raw)
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

func (store *sqliteBoardStore) ListAgentRuns(ctx context.Context, tenantID string, boardID string, limit int) (runs []agentRun, err error) {
	tenantID = normalizeTenantID(tenantID)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT run_json
		FROM agent_runs
		WHERE tenant_id = ? AND board_id = ?
		ORDER BY updated_at DESC
		LIMIT ?
	`, tenantID, boardID, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close agent run rows: %w", closeErr)
		}
	}()

	runs = make([]agentRun, 0)
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

func (store *sqliteBoardStore) SaveMutationRecord(ctx context.Context, tenantID string, boardID string, record boardMutationRecord, state kanbanBoardState) error {
	tenantID = normalizeTenantID(tenantID)
	if record.EventID == "" {
		return fmt.Errorf("mutation record requires event_id")
	}
	mutationJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO action_replay_events(tenant_id, board_id, event_id, occurred_at, tool_name, mutation_json, state_json, sequence_number)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, board_id, event_id) DO UPDATE SET
			occurred_at = excluded.occurred_at,
			tool_name = excluded.tool_name,
			mutation_json = excluded.mutation_json,
			state_json = excluded.state_json,
			sequence_number = excluded.sequence_number
	`, tenantID, boardID, record.EventID, record.OccurredAt, record.ToolName, string(mutationJSON), string(stateJSON), state.SequenceNumber)
	return err
}

func (store *sqliteBoardStore) UpdateMutationRecord(ctx context.Context, tenantID string, boardID string, record boardMutationRecord) error {
	tenantID = normalizeTenantID(tenantID)
	if record.EventID == "" {
		return fmt.Errorf("mutation record requires event_id")
	}
	mutationJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	result, err := store.db.ExecContext(ctx, `
		UPDATE action_replay_events
		SET mutation_json = ?
		WHERE tenant_id = ? AND board_id = ? AND event_id = ?
	`, string(mutationJSON), tenantID, boardID, record.EventID)
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows == 0 {
		return fmt.Errorf("mutation record %s was not found", record.EventID)
	}
	return nil
}

func (store *sqliteBoardStore) ListMutationRecords(ctx context.Context, tenantID string, boardID string, limit int) (records []boardMutationRecord, err error) {
	tenantID = normalizeTenantID(tenantID)
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT mutation_json
		FROM action_replay_events
		WHERE tenant_id = ? AND board_id = ?
		ORDER BY occurred_at DESC
		LIMIT ?
	`, tenantID, boardID, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close mutation record rows: %w", closeErr)
		}
	}()

	var newestFirst []boardMutationRecord
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record boardMutationRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, fmt.Errorf("decode mutation replay record: %w", err)
		}
		newestFirst = append(newestFirst, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	records = make([]boardMutationRecord, 0, len(newestFirst))
	for i := len(newestFirst) - 1; i >= 0; i-- {
		records = append(records, newestFirst[i])
	}
	return records, nil
}

func (store *sqliteBoardStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}
