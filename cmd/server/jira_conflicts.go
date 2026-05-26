package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (board *kanbanBoard) ApplyJiraCards(cards []kanbanCard, source string) []jiraConflict {
	board.mu.Lock()
	defer board.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	incomingByID := make(map[string]kanbanCard, len(cards))
	for _, card := range cards {
		incomingByID[card.ID] = cloneKanbanCard(card)
	}
	localByID := make(map[string]int, len(board.cards))
	for index, card := range board.cards {
		localByID[card.ID] = index
	}

	var conflicts []jiraConflict
	nextCards := make([]kanbanCard, 0, len(cards))
	for _, incoming := range cards {
		if index, ok := localByID[incoming.ID]; ok {
			local := board.cards[index]
			if !cardsEquivalent(local, incoming) && board.hasLocalMutationSinceJiraRefreshLocked(incoming.ID) {
				conflict := jiraConflict{
					ConflictID:    board.nextOperationIDLocked("conflict"),
					CardID:        incoming.ID,
					Source:        source,
					Summary:       fmt.Sprintf("Jira changed %s while local meeting changes are pending.", incoming.ID),
					Fields:        changedCardFields(local, incoming),
					LocalCard:     cloneKanbanCard(local),
					JiraCard:      cloneKanbanCard(incoming),
					DetectedAt:    now,
					LocalSequence: board.sequenceNumber,
				}
				board.conflicts = append(board.conflicts, conflict)
				conflicts = append(conflicts, conflict)
				nextCards = append(nextCards, cloneKanbanCard(local))
				continue
			}
		}
		nextCards = append(nextCards, cloneKanbanCard(incoming))
	}

	board.cards = nextCards
	board.nextCreatedIndex = nextCreatedIndexForCards(board.cards)
	board.touchLocked()
	board.lastJiraRefreshSeq = board.sequenceNumber
	if len(board.conflicts) > 50 {
		board.conflicts = append([]jiraConflict(nil), board.conflicts[len(board.conflicts)-50:]...)
	}
	return conflicts
}

func (board *kanbanBoard) hasLocalMutationSinceJiraRefreshLocked(cardID string) bool {
	for index := len(board.mutationHistory) - 1; index >= 0; index-- {
		record := board.mutationHistory[index]
		if record.Sequence <= board.lastJiraRefreshSeq {
			return false
		}
		for _, changedID := range record.CardIDs {
			if strings.EqualFold(changedID, cardID) {
				return true
			}
		}
	}
	return false
}

func changedCardFields(local, incoming kanbanCard) []string {
	fields := make([]string, 0)
	if local.Status != incoming.Status {
		fields = append(fields, "status")
	}
	if local.Title != incoming.Title {
		fields = append(fields, "title")
	}
	if local.Notes != incoming.Notes {
		fields = append(fields, "notes")
	}
	if mustMarshalJSON(local.Tags) != mustMarshalJSON(incoming.Tags) {
		fields = append(fields, "tags")
	}
	if mustMarshalJSON(local.Assignee) != mustMarshalJSON(incoming.Assignee) {
		fields = append(fields, "assignee")
	}
	if local.DueDate != incoming.DueDate {
		fields = append(fields, "eta")
	}
	if local.Priority != incoming.Priority {
		fields = append(fields, "priority")
	}
	if local.BlockedReason != incoming.BlockedReason {
		fields = append(fields, "blocked_reason")
	}
	return fields
}

func (board *kanbanBoard) RecordJiraSyncFailure(toolName string, rawArgs string, result map[string]any, syncErr error) jiraConflict {
	args := map[string]any{}
	if trimmed := strings.TrimSpace(rawArgs); trimmed != "" {
		_ = json.Unmarshal([]byte(trimmed), &args)
	}
	cardID := firstNonEmptyString(args, "card_id", "source_card_id", "parent_id")
	if cardID == "" {
		cardID = asString(result["card_id"])
	}
	if cardID == "" {
		return jiraConflict{}
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	local, ok := board.findCardLocked(cardID)
	if !ok {
		return jiraConflict{}
	}
	conflict := jiraConflict{
		ConflictID:    board.nextOperationIDLocked("conflict"),
		CardID:        cardID,
		Source:        "jira-write-through",
		Summary:       fmt.Sprintf("Jira write-through failed for %s: %s", toolName, syncErr.Error()),
		Fields:        []string{toolName},
		LocalCard:     cloneKanbanCard(*local),
		JiraCard:      cloneKanbanCard(*local),
		DetectedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		LocalSequence: board.sequenceNumber,
	}
	board.conflicts = append(board.conflicts, conflict)
	return conflict
}

func (board *kanbanBoard) resolveJiraConflict(args map[string]any) (map[string]any, bool, error) {
	conflictID := asString(args["conflict_id"])
	resolution := strings.ToLower(strings.TrimSpace(asString(args["resolution"])))
	if conflictID == "" {
		return nil, false, fmt.Errorf("conflict_id is required")
	}
	if resolution != "keep_local" && resolution != "use_jira" {
		return nil, false, fmt.Errorf("resolution must be keep_local or use_jira")
	}

	board.mu.Lock()
	defer board.mu.Unlock()
	for index := range board.conflicts {
		conflict := &board.conflicts[index]
		if conflict.ConflictID != conflictID {
			continue
		}
		if conflict.ResolvedAt != "" {
			return map[string]any{"ok": true, "already_resolved": true, "conflict": *conflict}, false, nil
		}
		if resolution == "use_jira" {
			if card, ok := board.findCardLocked(conflict.CardID); ok {
				*card = cloneKanbanCard(conflict.JiraCard)
			} else {
				board.cards = append(board.cards, cloneKanbanCard(conflict.JiraCard))
			}
			board.touchLocked()
		}
		conflict.Resolution = resolution
		conflict.ResolvedAt = time.Now().UTC().Format(time.RFC3339Nano)
		broadcastKanbanEventForBoard(board.tenantID, board.boardID, "conflict_resolved", *conflict)
		return map[string]any{"ok": true, "conflict": *conflict, "resolution": resolution}, resolution == "use_jira", nil
	}
	return map[string]any{"ok": false, "error": "conflict not found"}, false, nil
}

func (syncer *jiraSyncer) ApplyUndo(ctx context.Context, record boardMutationRecord) error {
	beforeByID := map[string]kanbanCard{}
	for _, card := range record.BeforeCards {
		beforeByID[card.ID] = card
	}
	afterByID := map[string]kanbanCard{}
	for _, card := range record.AfterCards {
		afterByID[card.ID] = card
	}

	var errors []string
	for id, afterCard := range afterByID {
		beforeCard, existedBefore := beforeByID[id]
		if !existedBefore {
			if err := syncer.client.CloseIssue(ctx, afterCard.ID); err != nil {
				errors = append(errors, err.Error())
			}
			continue
		}
		if err := syncer.restoreJiraCard(ctx, beforeCard); err != nil {
			errors = append(errors, err.Error())
		}
	}
	for id, beforeCard := range beforeByID {
		if _, existedAfter := afterByID[id]; !existedAfter {
			if _, err := syncer.client.CreateIssue(ctx, beforeCard); err != nil {
				errors = append(errors, err.Error())
			}
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}

func (syncer *jiraSyncer) restoreJiraCard(ctx context.Context, card kanbanCard) error {
	if err := syncer.client.UpdateIssue(ctx, card.ID, card.Title, card.Notes); err != nil {
		return err
	}
	if err := syncer.moveIssue(ctx, card.ID, card.Status, card.BlockedReason); err != nil {
		return err
	}
	switch {
	case card.Assignee == nil:
		if err := syncer.client.AssignIssue(ctx, card.ID, ""); err != nil {
			return err
		}
	case card.Assignee.Kind == kanbanActorKindHuman:
		if err := syncer.client.AssignIssue(ctx, card.ID, card.Assignee.ID); err != nil {
			return err
		}
	default:
		// Agent (or other non-human) assignee has no Jira identity.
		// Clear the Jira assignee so we do not leak a synthetic id into
		// Jira's user search.
		if err := syncer.client.AssignIssue(ctx, card.ID, ""); err != nil {
			return err
		}
	}
	if err := syncer.client.SetDueDate(ctx, card.ID, card.DueDate); err != nil {
		return err
	}
	if strings.TrimSpace(card.Priority) != "" {
		if err := syncer.client.SetPriority(ctx, card.ID, card.Priority); err != nil {
			return err
		}
	}
	return nil
}
