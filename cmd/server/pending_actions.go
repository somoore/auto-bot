package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// pendingActionStatus is the lifecycle state of a staged tool call queued by
// dry-run mode. Pending actions are created when a tenant has dry-run enabled,
// and transition out via approve (status=applied), reject (status=rejected),
// or the TTL sweeper (status=expired).
type pendingActionStatus string

const (
	pendingActionStatusPending  pendingActionStatus = "pending"
	pendingActionStatusApplied  pendingActionStatus = "applied"
	pendingActionStatusRejected pendingActionStatus = "rejected"
	pendingActionStatusExpired  pendingActionStatus = "expired"
)

// defaultPendingActionTTL is the soft expiry window applied to every staged
// action so abandoned items do not accumulate forever. The sweeper marks
// expired actions on every BuildAgenda tick and on demand.
const defaultPendingActionTTL = 24 * time.Hour

// pendingAction is the canonical staged-tool-call record persisted in the
// pending_actions table. Args and Intent are arbitrary JSON; the store
// serializes them to TEXT and parses on read.
type pendingAction struct {
	TenantID  string                 `json:"tenant_id"`
	BoardID   string                 `json:"board_id"`
	ActionID  string                 `json:"action_id"`
	Tool      string                 `json:"tool"`
	Args      map[string]any         `json:"args,omitempty"`
	Intent    map[string]any         `json:"intent,omitempty"`
	CreatedAt string                 `json:"created_at"`
	ExpiresAt string                 `json:"expires_at,omitempty"`
	Status    pendingActionStatus    `json:"status"`
	Result    map[string]any         `json:"result,omitempty"`
	Decision  *pendingActionDecision `json:"decision,omitempty"`
}

// pendingActionDecision records the metadata of the human (or agent) decision
// that resolved a pending action. Persisted as JSON inside intent for now to
// avoid a schema migration; surfaced as a typed field in API responses.
type pendingActionDecision struct {
	DecidedAt   string `json:"decided_at"`
	DecidedBy   string `json:"decided_by,omitempty"`
	DecidedVia  string `json:"decided_via,omitempty"`
	Disposition string `json:"disposition"`
	Note        string `json:"note,omitempty"`
}

// ErrPendingActionNotFound is returned when no pending_actions row matches
// (tenantID, boardID, actionID). Callers can branch with errors.Is to map to
// HTTP 404 / JSON-RPC -32602.
var ErrPendingActionNotFound = errors.New("pending action not found")

// ErrPendingActionTerminal is returned when an approve/reject is attempted
// against an action that is already applied/rejected/expired.
var ErrPendingActionTerminal = errors.New("pending action is already terminal")

// pendingActionStore is the persistence surface for the dry-run staging queue.
// The sqliteBoardStore satisfies it; tests can substitute an in-memory mock.
type pendingActionStore interface {
	SavePendingAction(ctx context.Context, action pendingAction) error
	LoadPendingAction(ctx context.Context, tenantID, boardID, actionID string) (pendingAction, error)
	ListPendingActions(ctx context.Context, tenantID, boardID string, includeTerminal bool, limit int) ([]pendingAction, error)
	UpdatePendingActionStatus(ctx context.Context, tenantID, boardID, actionID string, status pendingActionStatus, decision *pendingActionDecision, result map[string]any) error
	ExpirePendingActions(ctx context.Context, tenantID, boardID string, now time.Time) (int, error)
}

// Compile-time check that *sqliteBoardStore implements pendingActionStore.
var _ pendingActionStore = (*sqliteBoardStore)(nil)

// memoryPendingActionStore is a thread-safe in-memory implementation used by
// tests and by deployments without a SQLite path. Production runs through the
// sqliteBoardStore variant.
type memoryPendingActionStore struct {
	mu      sync.Mutex
	actions map[string]pendingAction // key: tenant|board|action
}

func newMemoryPendingActionStore() *memoryPendingActionStore {
	return &memoryPendingActionStore{actions: map[string]pendingAction{}}
}

func memoryPendingActionKey(tenant, board, action string) string {
	return normalizeTenantID(tenant) + "|" + board + "|" + action
}

func (s *memoryPendingActionStore) SavePendingAction(_ context.Context, action pendingAction) error {
	if action.ActionID == "" {
		return fmt.Errorf("pending action requires action_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	action.TenantID = normalizeTenantID(action.TenantID)
	s.actions[memoryPendingActionKey(action.TenantID, action.BoardID, action.ActionID)] = action
	return nil
}

func (s *memoryPendingActionStore) LoadPendingAction(_ context.Context, tenantID, boardID, actionID string) (pendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, ok := s.actions[memoryPendingActionKey(tenantID, boardID, actionID)]
	if !ok {
		return pendingAction{}, ErrPendingActionNotFound
	}
	return action, nil
}

func (s *memoryPendingActionStore) ListPendingActions(_ context.Context, tenantID, boardID string, includeTerminal bool, limit int) ([]pendingAction, error) {
	tenantID = normalizeTenantID(tenantID)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]pendingAction, 0, len(s.actions))
	for _, a := range s.actions {
		if a.TenantID != tenantID || a.BoardID != boardID {
			continue
		}
		if !includeTerminal && a.Status != pendingActionStatusPending {
			continue
		}
		out = append(out, a)
	}
	// sort newest-first
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt > out[i].CreatedAt {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memoryPendingActionStore) UpdatePendingActionStatus(_ context.Context, tenantID, boardID, actionID string, status pendingActionStatus, decision *pendingActionDecision, result map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memoryPendingActionKey(tenantID, boardID, actionID)
	action, ok := s.actions[key]
	if !ok {
		return ErrPendingActionNotFound
	}
	if action.Status != pendingActionStatusPending {
		return ErrPendingActionTerminal
	}
	action.Status = status
	if decision != nil {
		action.Decision = decision
	}
	if result != nil {
		action.Result = result
	}
	s.actions[key] = action
	return nil
}

func (s *memoryPendingActionStore) ExpirePendingActions(_ context.Context, tenantID, boardID string, now time.Time) (int, error) {
	tenantID = normalizeTenantID(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	expired := 0
	for key, a := range s.actions {
		if a.TenantID != tenantID || a.BoardID != boardID {
			continue
		}
		if a.Status != pendingActionStatusPending {
			continue
		}
		if a.ExpiresAt == "" {
			continue
		}
		deadline, err := time.Parse(time.RFC3339Nano, a.ExpiresAt)
		if err != nil {
			deadline, err = time.Parse(time.RFC3339, a.ExpiresAt)
			if err != nil {
				continue
			}
		}
		if !now.Before(deadline) {
			a.Status = pendingActionStatusExpired
			a.Decision = &pendingActionDecision{
				DecidedAt:   now.UTC().Format(time.RFC3339Nano),
				DecidedVia:  "sweeper",
				Disposition: string(pendingActionStatusExpired),
				Note:        "ttl elapsed",
			}
			s.actions[key] = a
			expired++
		}
	}
	return expired, nil
}

// SQLite implementation

func (store *sqliteBoardStore) SavePendingAction(ctx context.Context, action pendingAction) error {
	if action.ActionID == "" {
		return fmt.Errorf("pending action requires action_id")
	}
	action.TenantID = normalizeTenantID(action.TenantID)
	argsJSON, err := json.Marshal(action.Args)
	if err != nil {
		return fmt.Errorf("encode pending action args: %w", err)
	}
	intentJSON, err := json.Marshal(action.Intent)
	if err != nil {
		return fmt.Errorf("encode pending action intent: %w", err)
	}
	var resultJSON []byte
	if action.Result != nil {
		resultJSON, err = json.Marshal(action.Result)
		if err != nil {
			return fmt.Errorf("encode pending action result: %w", err)
		}
	}
	var decisionJSON []byte
	if action.Decision != nil {
		decisionJSON, err = json.Marshal(action.Decision)
		if err != nil {
			return fmt.Errorf("encode pending action decision: %w", err)
		}
	}
	if action.Status == "" {
		action.Status = pendingActionStatusPending
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO pending_actions(
			tenant_id, board_id, action_id, tool, args_json, intent_json,
			created_at, expires_at, status, result_json, decision_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, board_id, action_id) DO UPDATE SET
			tool = excluded.tool,
			args_json = excluded.args_json,
			intent_json = excluded.intent_json,
			expires_at = excluded.expires_at,
			status = excluded.status,
			result_json = excluded.result_json,
			decision_json = excluded.decision_json
	`,
		action.TenantID, action.BoardID, action.ActionID, action.Tool,
		string(argsJSON), string(intentJSON),
		action.CreatedAt, action.ExpiresAt, string(action.Status),
		nullableString(resultJSON), nullableString(decisionJSON),
	)
	return err
}

func nullableString(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func (store *sqliteBoardStore) LoadPendingAction(ctx context.Context, tenantID, boardID, actionID string) (pendingAction, error) {
	tenantID = normalizeTenantID(tenantID)
	row := store.db.QueryRowContext(ctx, `
		SELECT tool, args_json, intent_json, created_at, expires_at, status, result_json, decision_json
		FROM pending_actions
		WHERE tenant_id = ? AND board_id = ? AND action_id = ?
	`, tenantID, boardID, actionID)
	return scanPendingAction(row, tenantID, boardID, actionID)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPendingAction(row rowScanner, tenantID, boardID, actionID string) (pendingAction, error) {
	var (
		tool, argsRaw, intentRaw, createdAt, expiresAt, status string
		resultRaw, decisionRaw                                 sql.NullString
	)
	err := row.Scan(&tool, &argsRaw, &intentRaw, &createdAt, &expiresAt, &status, &resultRaw, &decisionRaw)
	if err == sql.ErrNoRows {
		return pendingAction{}, ErrPendingActionNotFound
	}
	if err != nil {
		return pendingAction{}, err
	}
	action := pendingAction{
		TenantID:  tenantID,
		BoardID:   boardID,
		ActionID:  actionID,
		Tool:      tool,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Status:    pendingActionStatus(status),
	}
	if strings.TrimSpace(argsRaw) != "" && argsRaw != "null" {
		if err := json.Unmarshal([]byte(argsRaw), &action.Args); err != nil {
			return pendingAction{}, fmt.Errorf("decode pending action args: %w", err)
		}
	}
	if strings.TrimSpace(intentRaw) != "" && intentRaw != "null" {
		if err := json.Unmarshal([]byte(intentRaw), &action.Intent); err != nil {
			return pendingAction{}, fmt.Errorf("decode pending action intent: %w", err)
		}
	}
	if resultRaw.Valid && strings.TrimSpace(resultRaw.String) != "" && resultRaw.String != "null" {
		if err := json.Unmarshal([]byte(resultRaw.String), &action.Result); err != nil {
			return pendingAction{}, fmt.Errorf("decode pending action result: %w", err)
		}
	}
	if decisionRaw.Valid && strings.TrimSpace(decisionRaw.String) != "" && decisionRaw.String != "null" {
		var decision pendingActionDecision
		if err := json.Unmarshal([]byte(decisionRaw.String), &decision); err != nil {
			return pendingAction{}, fmt.Errorf("decode pending action decision: %w", err)
		}
		action.Decision = &decision
	}
	return action, nil
}

func (store *sqliteBoardStore) ListPendingActions(ctx context.Context, tenantID, boardID string, includeTerminal bool, limit int) ([]pendingAction, error) {
	tenantID = normalizeTenantID(tenantID)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `
		SELECT action_id, tool, args_json, intent_json, created_at, expires_at, status, result_json, decision_json
		FROM pending_actions
		WHERE tenant_id = ? AND board_id = ?`
	args := []any{tenantID, boardID}
	if !includeTerminal {
		query += ` AND status = ?`
		args = append(args, string(pendingActionStatusPending))
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]pendingAction, 0, limit)
	for rows.Next() {
		var (
			actionID, tool, argsRaw, intentRaw, createdAt, expiresAt, status string
			resultRaw, decisionRaw                                           sql.NullString
		)
		if err := rows.Scan(&actionID, &tool, &argsRaw, &intentRaw, &createdAt, &expiresAt, &status, &resultRaw, &decisionRaw); err != nil {
			return nil, err
		}
		action := pendingAction{
			TenantID:  tenantID,
			BoardID:   boardID,
			ActionID:  actionID,
			Tool:      tool,
			CreatedAt: createdAt,
			ExpiresAt: expiresAt,
			Status:    pendingActionStatus(status),
		}
		if strings.TrimSpace(argsRaw) != "" && argsRaw != "null" {
			_ = json.Unmarshal([]byte(argsRaw), &action.Args)
		}
		if strings.TrimSpace(intentRaw) != "" && intentRaw != "null" {
			_ = json.Unmarshal([]byte(intentRaw), &action.Intent)
		}
		if resultRaw.Valid && strings.TrimSpace(resultRaw.String) != "" && resultRaw.String != "null" {
			_ = json.Unmarshal([]byte(resultRaw.String), &action.Result)
		}
		if decisionRaw.Valid && strings.TrimSpace(decisionRaw.String) != "" && decisionRaw.String != "null" {
			var decision pendingActionDecision
			if err := json.Unmarshal([]byte(decisionRaw.String), &decision); err == nil {
				action.Decision = &decision
			}
		}
		out = append(out, action)
	}
	return out, rows.Err()
}

func (store *sqliteBoardStore) UpdatePendingActionStatus(ctx context.Context, tenantID, boardID, actionID string, status pendingActionStatus, decision *pendingActionDecision, result map[string]any) error {
	tenantID = normalizeTenantID(tenantID)
	var current string
	err := store.db.QueryRowContext(ctx, `SELECT status FROM pending_actions WHERE tenant_id = ? AND board_id = ? AND action_id = ?`, tenantID, boardID, actionID).Scan(&current)
	if err == sql.ErrNoRows {
		return ErrPendingActionNotFound
	}
	if err != nil {
		return err
	}
	if pendingActionStatus(current) != pendingActionStatusPending {
		return ErrPendingActionTerminal
	}
	var decisionJSON, resultJSON sql.NullString
	if decision != nil {
		raw, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		decisionJSON = sql.NullString{String: string(raw), Valid: true}
	}
	if result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resultJSON = sql.NullString{String: string(raw), Valid: true}
	}
	_, err = store.db.ExecContext(ctx, `
		UPDATE pending_actions
		SET status = ?, decision_json = COALESCE(?, decision_json), result_json = COALESCE(?, result_json)
		WHERE tenant_id = ? AND board_id = ? AND action_id = ?
	`, string(status), decisionJSON, resultJSON, tenantID, boardID, actionID)
	return err
}

func (store *sqliteBoardStore) ExpirePendingActions(ctx context.Context, tenantID, boardID string, now time.Time) (int, error) {
	tenantID = normalizeTenantID(tenantID)
	rows, err := store.db.QueryContext(ctx, `
		SELECT action_id, expires_at FROM pending_actions
		WHERE tenant_id = ? AND board_id = ? AND status = ?
	`, tenantID, boardID, string(pendingActionStatusPending))
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	expiredIDs := make([]string, 0)
	for rows.Next() {
		var id, expiresAt string
		if err := rows.Scan(&id, &expiresAt); err != nil {
			return 0, err
		}
		if expiresAt == "" {
			continue
		}
		deadline, err := time.Parse(time.RFC3339Nano, expiresAt)
		if err != nil {
			deadline, err = time.Parse(time.RFC3339, expiresAt)
			if err != nil {
				continue
			}
		}
		if !now.Before(deadline) {
			expiredIDs = append(expiredIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(expiredIDs) == 0 {
		return 0, nil
	}
	expiredAt := now.UTC().Format(time.RFC3339Nano)
	decisionRaw, _ := json.Marshal(pendingActionDecision{
		DecidedAt:   expiredAt,
		DecidedVia:  "sweeper",
		Disposition: string(pendingActionStatusExpired),
		Note:        "ttl elapsed",
	})
	for _, id := range expiredIDs {
		_, err := store.db.ExecContext(ctx, `
			UPDATE pending_actions
			SET status = ?, decision_json = ?
			WHERE tenant_id = ? AND board_id = ? AND action_id = ?
		`, string(pendingActionStatusExpired), string(decisionRaw), tenantID, boardID, id)
		if err != nil {
			return 0, err
		}
	}
	return len(expiredIDs), nil
}

// generatePendingActionID returns a 24-hex-char random identifier suitable
// for log scoping and URLs. crypto/rand keeps it unguessable so external
// observers cannot enumerate the queue.
func generatePendingActionID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to time-based — collisions extremely unlikely in this
		// path; rand.Read failure is itself catastrophic.
		return fmt.Sprintf("pa_%d", time.Now().UnixNano())
	}
	return "pa_" + hex.EncodeToString(buf)
}
