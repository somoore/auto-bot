package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// pendingActionDiff is the before/after summary returned for a staged action.
// Before/After carry full card snapshots so the UI can render a side-by-side
// view; ChangedCards lists card IDs that differ; CreatedCardIDs and
// RemovedCardIDs surface adds/removes for the diff legend.
type pendingActionDiff struct {
	ActionID       string         `json:"action_id"`
	Tool           string         `json:"tool"`
	Args           map[string]any `json:"args,omitempty"`
	Before         []kanbanCard   `json:"before"`
	After          []kanbanCard   `json:"after"`
	ChangedCardIDs []string       `json:"changed_card_ids,omitempty"`
	CreatedCardIDs []string       `json:"created_card_ids,omitempty"`
	RemovedCardIDs []string       `json:"removed_card_ids,omitempty"`
	Error          string         `json:"error,omitempty"`
	SequenceBefore int64          `json:"sequence_before"`
	SequenceAfter  int64          `json:"sequence_after"`
	MeetingChanged bool           `json:"meeting_changed,omitempty"`
}

// PreviewPendingAction loads the staged action and applies it to a copy of
// the board state, returning the before/after diff without mutating the real
// board. Errors during the simulated apply (validation failures, missing
// cards, etc.) are surfaced in pendingActionDiff.Error so the UI can warn
// the user before they approve.
func (board *kanbanBoard) PreviewPendingAction(ctx context.Context, actionID string) (pendingActionDiff, error) {
	store := globalPendingActionStore()
	if store == nil {
		return pendingActionDiff{}, fmt.Errorf("pending action store is not configured")
	}
	action, err := store.LoadPendingAction(ctx, board.tenantID, board.boardID, actionID)
	if err != nil {
		return pendingActionDiff{}, err
	}

	before := board.SnapshotState()
	// Build a virtual board seeded from the real board's current state.
	virtual := newKanbanBoard()
	virtual.tenantID = board.tenantID
	virtual.boardID = board.boardID
	virtual.cards = cloneKanbanCards(before.Cards)
	virtual.nextCreatedIndex = nextCreatedIndexForCards(virtual.cards)
	virtual.sequenceNumber = before.SequenceNumber
	if before.Meeting != nil {
		virtual.meeting = *before.Meeting
	}

	rawArgs, err := json.Marshal(action.Args)
	if err != nil {
		return pendingActionDiff{}, fmt.Errorf("encode args for preview: %w", err)
	}
	diff := pendingActionDiff{
		ActionID:       action.ActionID,
		Tool:           action.Tool,
		Args:           action.Args,
		Before:         before.Cards,
		SequenceBefore: before.SequenceNumber,
	}
	_, _, applyErr := virtual.ApplyToolCallWithMeta(action.Tool, string(rawArgs), toolCallMeta{
		Dispatcher:       "dry-run-preview",
		SkipConfirmation: true,
	})
	after := virtual.SnapshotState()
	diff.After = after.Cards
	diff.SequenceAfter = after.SequenceNumber
	if applyErr != nil {
		diff.Error = applyErr.Error()
	}

	diff.ChangedCardIDs, diff.CreatedCardIDs, diff.RemovedCardIDs = computeCardDiff(before.Cards, after.Cards)
	diff.MeetingChanged = meetingStatesDiffer(before.Meeting, after.Meeting)

	return diff, nil
}

// computeCardDiff compares before/after card slices by ID and returns the
// sets of changed, newly-created, and removed cards. Card equality is
// determined by deep JSON equality so any field-level change shows up.
func computeCardDiff(before, after []kanbanCard) (changed, created, removed []string) {
	beforeMap := indexCardsByID(before)
	afterMap := indexCardsByID(after)
	for id, bc := range beforeMap {
		ac, ok := afterMap[id]
		if !ok {
			removed = append(removed, id)
			continue
		}
		if !cardsEqual(bc, ac) {
			changed = append(changed, id)
		}
	}
	for id := range afterMap {
		if _, ok := beforeMap[id]; !ok {
			created = append(created, id)
		}
	}
	sort.Strings(changed)
	sort.Strings(created)
	sort.Strings(removed)
	return changed, created, removed
}

func indexCardsByID(cards []kanbanCard) map[string]kanbanCard {
	out := make(map[string]kanbanCard, len(cards))
	for _, c := range cards {
		out[c.ID] = c
	}
	return out
}

func cardsEqual(a, b kanbanCard) bool {
	aRaw, errA := json.Marshal(a)
	bRaw, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aRaw) == string(bRaw)
}

func meetingStatesDiffer(a, b *scrumMeetingState) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	aRaw, _ := json.Marshal(a)
	bRaw, _ := json.Marshal(b)
	return string(aRaw) != string(bRaw)
}
