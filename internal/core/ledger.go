package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ActionIntent records the normalized intent inferred from live user evidence
// before a tool or connector action executes.
type ActionIntent struct {
	// ID is stable across tool calls and external confirmations for this
	// intent.
	ID         string         `json:"id"`
	MeetingID  string         `json:"meeting_id,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	Connector  string         `json:"connector,omitempty"`
	Action     string         `json:"action"`
	Target     string         `json:"target,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Risk       RiskLevel      `json:"risk"`
	Evidence   []Evidence     `json:"evidence,omitempty"`
	Confidence Confidence     `json:"confidence,omitempty"`
	// CreatedAt is stored in UTC when the ledger fills it.
	CreatedAt time.Time `json:"created_at"`
}

// ToolCallRecord records the selected tool, arguments, timing, and local error
// state associated with an intent.
type ToolCallRecord struct {
	ID string `json:"id"`
	// IntentID links this tool call to a previously recorded ActionIntent.
	IntentID  string         `json:"intent_id"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments,omitempty"`
	// StartedAt and CompletedAt are UTC timestamps when provided by the caller
	// or filled by the ledger.
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// ExternalConfirmation records the external API result that proves what a
// connector did or did not change outside the local board.
type ExternalConfirmation struct {
	ID string `json:"id"`
	// IntentID links this external result to a previously recorded ActionIntent.
	IntentID   string        `json:"intent_id"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Connector  string        `json:"connector"`
	Status     string        `json:"status"`
	ExternalID string        `json:"external_id,omitempty"`
	Message    string        `json:"message,omitempty"`
	Receipt    ActionReceipt `json:"receipt,omitempty"`
	Evidence   []Evidence    `json:"evidence,omitempty"`
	// CreatedAt is stored in UTC when the ledger fills it.
	CreatedAt time.Time      `json:"created_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ReplayStep is a human-oriented audit step derived from ledger records.
type ReplayStep struct {
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Detail   string         `json:"detail,omitempty"`
	At       time.Time      `json:"at,omitempty"`
	Evidence []Evidence     `json:"evidence,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// ActionReplay is the complete replayable history for one action intent.
type ActionReplay struct {
	Intent        ActionIntent           `json:"intent"`
	ToolCalls     []ToolCallRecord       `json:"tool_calls,omitempty"`
	Confirmations []ExternalConfirmation `json:"confirmations,omitempty"`
	// Steps are ordered for human replay: intent, tool calls by start time, then
	// external confirmations by creation time.
	Steps []ReplayStep `json:"steps"`
}

// ActionLedger stores intent, tool, and external-confirmation evidence for
// later audit replay.
type ActionLedger interface {
	// RecordIntent stores a normalized action intent.
	RecordIntent(context.Context, ActionIntent) (ActionIntent, error)
	// RecordToolCall stores the tool selected for an existing intent.
	RecordToolCall(context.Context, ToolCallRecord) (ToolCallRecord, error)
	// RecordExternalConfirmation stores the external API outcome for an intent.
	RecordExternalConfirmation(context.Context, ExternalConfirmation) (ExternalConfirmation, error)
	// Replay reconstructs a human-readable audit trail for an intent ID.
	Replay(context.Context, string) (ActionReplay, error)
}

// InMemoryActionLedger is a deterministic in-process ledger implementation for
// tests, demos, and adapters that have not yet been backed by durable storage.
type InMemoryActionLedger struct {
	mu            sync.RWMutex
	intents       map[string]ActionIntent
	toolCalls     map[string][]ToolCallRecord
	confirmations map[string][]ExternalConfirmation
	nextID        int64
	now           func() time.Time
}

// NewInMemoryActionLedger returns an empty in-memory action ledger.
func NewInMemoryActionLedger() *InMemoryActionLedger {
	return &InMemoryActionLedger{
		intents:       map[string]ActionIntent{},
		toolCalls:     map[string][]ToolCallRecord{},
		confirmations: map[string][]ExternalConfirmation{},
		now:           time.Now,
	}
}

// RecordIntent stores an intent and fills missing ID and timestamp values.
func (ledger *InMemoryActionLedger) RecordIntent(_ context.Context, intent ActionIntent) (ActionIntent, error) {
	if intent.Action == "" {
		return ActionIntent{}, errors.New("intent action is required")
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if intent.ID == "" {
		intent.ID = ledger.nextLocked("intent")
	}
	if intent.CreatedAt.IsZero() {
		intent.CreatedAt = ledger.now().UTC()
	}
	intent.Evidence = cloneEvidence(intent.Evidence)
	intent.Confidence.Reasons = append([]string{}, intent.Confidence.Reasons...)
	ledger.intents[intent.ID] = intent
	return intent, nil
}

// RecordToolCall stores a tool call for an existing intent.
func (ledger *InMemoryActionLedger) RecordToolCall(_ context.Context, call ToolCallRecord) (ToolCallRecord, error) {
	if call.IntentID == "" {
		return ToolCallRecord{}, errors.New("tool call intent_id is required")
	}
	if call.Tool == "" {
		return ToolCallRecord{}, errors.New("tool call tool is required")
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if _, ok := ledger.intents[call.IntentID]; !ok {
		return ToolCallRecord{}, fmt.Errorf("unknown intent %q", call.IntentID)
	}
	if call.ID == "" {
		call.ID = ledger.nextLocked("tool")
	}
	if call.StartedAt.IsZero() {
		call.StartedAt = ledger.now().UTC()
	}
	call.Arguments = cloneMap(call.Arguments)
	ledger.toolCalls[call.IntentID] = append(ledger.toolCalls[call.IntentID], call)
	return call, nil
}

// RecordExternalConfirmation stores an external API result for an existing
// intent.
func (ledger *InMemoryActionLedger) RecordExternalConfirmation(_ context.Context, confirmation ExternalConfirmation) (ExternalConfirmation, error) {
	if confirmation.IntentID == "" {
		return ExternalConfirmation{}, errors.New("external confirmation intent_id is required")
	}
	if confirmation.Connector == "" {
		return ExternalConfirmation{}, errors.New("external confirmation connector is required")
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if _, ok := ledger.intents[confirmation.IntentID]; !ok {
		return ExternalConfirmation{}, fmt.Errorf("unknown intent %q", confirmation.IntentID)
	}
	if confirmation.ID == "" {
		confirmation.ID = ledger.nextLocked("external")
	}
	if confirmation.CreatedAt.IsZero() {
		confirmation.CreatedAt = ledger.now().UTC()
	}
	confirmation.Evidence = cloneEvidence(confirmation.Evidence)
	confirmation.Metadata = cloneMap(confirmation.Metadata)
	ledger.confirmations[confirmation.IntentID] = append(ledger.confirmations[confirmation.IntentID], confirmation)
	return confirmation, nil
}

// Replay returns the ordered intent, tool-call, and external-confirmation steps
// for an intent ID.
func (ledger *InMemoryActionLedger) Replay(_ context.Context, intentID string) (ActionReplay, error) {
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
	intent, ok := ledger.intents[intentID]
	if !ok {
		return ActionReplay{}, fmt.Errorf("unknown intent %q", intentID)
	}
	calls := append([]ToolCallRecord{}, ledger.toolCalls[intentID]...)
	confirmations := append([]ExternalConfirmation{}, ledger.confirmations[intentID]...)
	sort.Slice(calls, func(i, j int) bool { return calls[i].StartedAt.Before(calls[j].StartedAt) })
	sort.Slice(confirmations, func(i, j int) bool { return confirmations[i].CreatedAt.Before(confirmations[j].CreatedAt) })

	steps := []ReplayStep{
		{
			Kind:     "intent",
			Title:    "Intent captured",
			Detail:   intent.Action,
			At:       intent.CreatedAt,
			Evidence: cloneEvidence(intent.Evidence),
			Data: map[string]any{
				"actor":      intent.Actor,
				"target":     intent.Target,
				"risk":       intent.Risk,
				"confidence": intent.Confidence,
			},
		},
	}
	for _, call := range calls {
		step := ReplayStep{
			Kind:   "tool_call",
			Title:  "Tool selected",
			Detail: call.Tool,
			At:     call.StartedAt,
			Data: map[string]any{
				"arguments": cloneMap(call.Arguments),
			},
		}
		if call.Error != "" {
			step.Data["error"] = call.Error
		}
		steps = append(steps, step)
	}
	for _, confirmation := range confirmations {
		steps = append(steps, ReplayStep{
			Kind:     "external_confirmation",
			Title:    "External API result",
			Detail:   confirmation.Status,
			At:       confirmation.CreatedAt,
			Evidence: cloneEvidence(confirmation.Evidence),
			Data: map[string]any{
				"connector":   confirmation.Connector,
				"external_id": confirmation.ExternalID,
				"message":     confirmation.Message,
				"receipt":     confirmation.Receipt,
			},
		})
	}
	return ActionReplay{
		Intent:        intent,
		ToolCalls:     calls,
		Confirmations: confirmations,
		Steps:         steps,
	}, nil
}

func (ledger *InMemoryActionLedger) nextLocked(prefix string) string {
	ledger.nextID++
	return fmt.Sprintf("%s-%06d", prefix, ledger.nextID)
}

func cloneEvidence(evidence []Evidence) []Evidence {
	if len(evidence) == 0 {
		return nil
	}
	return append([]Evidence{}, evidence...)
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
