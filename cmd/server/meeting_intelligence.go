package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type toolRiskLevel string

const (
	toolRiskLow    toolRiskLevel = "low"
	toolRiskMedium toolRiskLevel = "medium"
	toolRiskHigh   toolRiskLevel = "high"
)

type toolCallMeta struct {
	Source     string
	Actor      string
	CallID     string
	Transcript string
}

type pendingConfirmation struct {
	ConfirmationID string         `json:"confirmationId"`
	ToolName       string         `json:"toolName"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	RiskLevel      toolRiskLevel  `json:"riskLevel"`
	Prompt         string         `json:"prompt"`
	Source         string         `json:"source,omitempty"`
	Actor          string         `json:"actor,omitempty"`
	CallID         string         `json:"callId,omitempty"`
	CreatedAt      string         `json:"createdAt"`
	ExpiresAt      string         `json:"expiresAt"`
}

type pendingConfirmationView struct {
	ConfirmationID    string        `json:"confirmationId"`
	ToolName          string        `json:"toolName"`
	RiskLevel         toolRiskLevel `json:"riskLevel"`
	Prompt            string        `json:"prompt"`
	Source            string        `json:"source,omitempty"`
	Actor             string        `json:"actor,omitempty"`
	Confidence        float64       `json:"confidence,omitempty"`
	ConfidenceReasons []string      `json:"confidenceReasons,omitempty"`
	MatchedCardID     string        `json:"matchedCardId,omitempty"`
	GuardrailDecision string        `json:"guardrailDecision,omitempty"`
	CreatedAt         string        `json:"createdAt"`
	ExpiresAt         string        `json:"expiresAt"`
}

type transcriptEntry struct {
	Role      string `json:"role"`
	Speaker   string `json:"speaker,omitempty"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

type transcriptEvidence struct {
	Entries []transcriptEntry `json:"entries,omitempty"`
	Summary string            `json:"summary,omitempty"`
}

type boardMutationRecord struct {
	EventID       string             `json:"eventId"`
	OccurredAt    string             `json:"occurredAt"`
	Source        string             `json:"source"`
	Actor         string             `json:"actor,omitempty"`
	ToolName      string             `json:"toolName"`
	Arguments     map[string]any     `json:"arguments,omitempty"`
	Result        map[string]any     `json:"result,omitempty"`
	RiskLevel     toolRiskLevel      `json:"riskLevel"`
	Confirmation  string             `json:"confirmationId,omitempty"`
	CallID        string             `json:"callId,omitempty"`
	CardIDs       []string           `json:"cardIds,omitempty"`
	Summary       string             `json:"summary"`
	BeforeCards   []kanbanCard       `json:"beforeCards,omitempty"`
	AfterCards    []kanbanCard       `json:"afterCards,omitempty"`
	BeforeMeeting *scrumMeetingState `json:"beforeMeeting,omitempty"`
	AfterMeeting  *scrumMeetingState `json:"afterMeeting,omitempty"`
	Transcript    transcriptEvidence `json:"transcript,omitempty"`
	Sequence      int64              `json:"sequenceNumber"`
	Reverted      bool               `json:"reverted,omitempty"`
	UndoOf        string             `json:"undoOf,omitempty"`
}

type boardMutationView struct {
	EventID           string             `json:"eventId"`
	OccurredAt        string             `json:"occurredAt"`
	Source            string             `json:"source"`
	Actor             string             `json:"actor,omitempty"`
	ToolName          string             `json:"toolName"`
	RiskLevel         toolRiskLevel      `json:"riskLevel"`
	Confirmation      string             `json:"confirmationId,omitempty"`
	CardIDs           []string           `json:"cardIds,omitempty"`
	Summary           string             `json:"summary"`
	Confidence        float64            `json:"confidence,omitempty"`
	ConfidenceReasons []string           `json:"confidenceReasons,omitempty"`
	MatchedCardID     string             `json:"matchedCardId,omitempty"`
	GuardrailDecision string             `json:"guardrailDecision,omitempty"`
	Transcript        transcriptEvidence `json:"transcript,omitempty"`
	Sequence          int64              `json:"sequenceNumber"`
	Reverted          bool               `json:"reverted,omitempty"`
	UndoOf            string             `json:"undoOf,omitempty"`
}

type scrumFollowUp struct {
	ID        string `json:"id"`
	Owner     string `json:"owner,omitempty"`
	Text      string `json:"text"`
	CardID    string `json:"cardId,omitempty"`
	DueDate   string `json:"dueDate,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type scrumBlocker struct {
	ID         string `json:"id"`
	Owner      string `json:"owner,omitempty"`
	Text       string `json:"text"`
	CardID     string `json:"cardId,omitempty"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
}

type scrumOwnership struct {
	Owner          string `json:"owner"`
	CardID         string `json:"cardId,omitempty"`
	Responsibility string `json:"responsibility"`
	UpdatedAt      string `json:"updatedAt"`
}

type scrumBriefing struct {
	GeneratedAt          string   `json:"generatedAt"`
	Since                string   `json:"since"`
	Summary              string   `json:"summary"`
	TicketsMoved         int      `json:"ticketsMoved"`
	PRsReady             int      `json:"prsReady"`
	BlockedCount         int      `json:"blockedCount"`
	UnassignedCount      int      `json:"unassignedCount"`
	StaleCards           []string `json:"staleCards,omitempty"`
	UnresolvedBlockers   []string `json:"unresolvedBlockers,omitempty"`
	RecommendedQuestions []string `json:"recommendedQuestions,omitempty"`
}

type jiraConflict struct {
	ConflictID    string     `json:"conflictId"`
	CardID        string     `json:"cardId"`
	Source        string     `json:"source"`
	Summary       string     `json:"summary"`
	Fields        []string   `json:"fields,omitempty"`
	LocalCard     kanbanCard `json:"localCard"`
	JiraCard      kanbanCard `json:"jiraCard"`
	DetectedAt    string     `json:"detectedAt"`
	LocalSequence int64      `json:"localSequence"`
	ResolvedAt    string     `json:"resolvedAt,omitempty"`
	Resolution    string     `json:"resolution,omitempty"`
}

func normalizeToolCallMeta(meta toolCallMeta) toolCallMeta {
	if strings.TrimSpace(meta.Source) == "" {
		meta.Source = "tool"
	}
	meta.Source = truncateString(meta.Source, 80)
	meta.Actor = truncateString(meta.Actor, 120)
	meta.CallID = truncateString(meta.CallID, 160)
	meta.Transcript = truncateString(meta.Transcript, 2000)
	return meta
}

func riskForTool(toolName string) toolRiskLevel {
	switch toolName {
	case "assign_ticket", "unassign_ticket", "assign_ticket_to_agent", "set_eta", "set_priority", "set_reporter":
		return toolRiskMedium
	case "delete_ticket", "set_sprint", "rank_issue":
		return toolRiskHigh
	default:
		return toolRiskLow
	}
}

func requiresConfirmation(toolName string) bool {
	risk := riskForTool(toolName)
	return risk == toolRiskMedium || risk == toolRiskHigh
}

func (board *kanbanBoard) createPendingConfirmation(toolName string, args map[string]any, meta toolCallMeta) map[string]any {
	meta = normalizeToolCallMeta(meta)
	now := time.Now().UTC()
	confirmation := pendingConfirmation{
		ConfirmationID: board.nextOperationIDLocked("confirm"),
		ToolName:       toolName,
		Arguments:      cloneToolArgs(args),
		RiskLevel:      riskForTool(toolName),
		Prompt:         confirmationPrompt(toolName, args),
		Source:         meta.Source,
		Actor:          meta.Actor,
		CallID:         meta.CallID,
		CreatedAt:      now.Format(time.RFC3339Nano),
		ExpiresAt:      now.Add(2 * time.Minute).Format(time.RFC3339Nano),
	}
	if board.pendingConfirmations == nil {
		board.pendingConfirmations = map[string]pendingConfirmation{}
	}
	board.pendingConfirmations[confirmation.ConfirmationID] = confirmation
	broadcastKanbanEventForBoard(board.boardID, "confirmation", pendingConfirmationToView(confirmation))
	return map[string]any{
		"ok":                    false,
		"requires_confirmation": true,
		"confirmation_id":       confirmation.ConfirmationID,
		"risk_level":            confirmation.RiskLevel,
		"tool_name":             confirmation.ToolName,
		"prompt":                confirmation.Prompt,
	}
}

func confirmationPrompt(toolName string, args map[string]any) string {
	cardID := firstNonEmptyString(args, "card_id", "source_card_id", "parent_id")
	target := cardID
	if target == "" {
		target = "the selected issue"
	}
	switch toolName {
	case "assign_ticket":
		assignee := firstNonEmptyString(args, "display_name", "query", "account_id")
		if assignee == "" {
			assignee = "that Jira user"
		}
		return fmt.Sprintf("I heard you want to assign %s to %s. Confirm?", target, assignee)
	case "unassign_ticket":
		return fmt.Sprintf("I heard you want to unassign %s. Confirm?", target)
	case "assign_ticket_to_agent":
		objective := asString(args["objective"])
		if objective == "" {
			objective = "work this task"
		}
		return fmt.Sprintf("I heard you want to start autonomous agents on %s to %s. Confirm?", target, objective)
	case "set_eta":
		return fmt.Sprintf("I heard you want to set the ETA for %s to %s. Confirm?", target, asString(args["eta"]))
	case "set_priority":
		return fmt.Sprintf("I heard you want to set %s to %s priority. Confirm?", target, asString(args["priority"]))
	case "set_reporter":
		reporter := firstNonEmptyString(args, "display_name", "query", "account_id")
		return fmt.Sprintf("I heard you want to change the reporter on %s to %s. Confirm?", target, reporter)
	case "delete_ticket":
		return fmt.Sprintf("I heard you want to close or delete %s. Confirm?", target)
	case "set_sprint":
		return fmt.Sprintf("I heard you want to move %s into sprint %v. Confirm?", target, args["sprint_id"])
	case "rank_issue":
		return fmt.Sprintf("I heard you want to reorder %s in Jira. Confirm?", target)
	default:
		return fmt.Sprintf("I heard you want to run %s on %s. Confirm?", toolName, target)
	}
}

func (board *kanbanBoard) confirmPendingAction(args map[string]any, meta toolCallMeta) (map[string]any, bool, error) {
	confirmationID := asString(args["confirmation_id"])
	board.mu.Lock()
	if confirmationID == "" {
		confirmationID = board.latestPendingConfirmationIDLocked()
	}
	confirmation, ok := board.pendingConfirmations[confirmationID]
	if ok {
		delete(board.pendingConfirmations, confirmationID)
	}
	board.mu.Unlock()
	if !ok {
		return map[string]any{"ok": false, "error": "pending confirmation not found"}, false, nil
	}
	if expiresAt, err := time.Parse(time.RFC3339Nano, confirmation.ExpiresAt); err == nil && time.Now().UTC().After(expiresAt) {
		return map[string]any{"ok": false, "error": "pending confirmation expired", "confirmation_id": confirmationID}, false, nil
	}

	meta = normalizeToolCallMeta(meta)
	result, changed, err := board.applyToolCallWithConfirmationBypass(confirmation.ToolName, confirmation.Arguments, meta, confirmation.ConfirmationID)
	if result == nil {
		result = map[string]any{}
	}
	result["confirmed"] = true
	result["confirmation_id"] = confirmation.ConfirmationID
	result["original_tool_name"] = confirmation.ToolName
	result["original_arguments"] = cloneToolArgs(confirmation.Arguments)
	result["original_arguments_json"] = mustMarshalJSON(confirmation.Arguments)
	return result, changed, err
}

func (board *kanbanBoard) cancelPendingConfirmation(args map[string]any) (map[string]any, bool, error) {
	confirmationID := asString(args["confirmation_id"])
	board.mu.Lock()
	if confirmationID == "" {
		confirmationID = board.latestPendingConfirmationIDLocked()
	}
	confirmation, ok := board.pendingConfirmations[confirmationID]
	if ok {
		delete(board.pendingConfirmations, confirmationID)
	}
	board.mu.Unlock()
	if !ok {
		return map[string]any{"ok": false, "error": "pending confirmation not found"}, false, nil
	}
	broadcastKanbanEventForBoard(board.boardID, "confirmation_cancelled", pendingConfirmationToView(confirmation))
	return map[string]any{"ok": true, "cancelled": true, "confirmation_id": confirmationID}, false, nil
}

func (board *kanbanBoard) listPendingConfirmations() (map[string]any, bool, error) {
	board.mu.Lock()
	defer board.mu.Unlock()
	return map[string]any{
		"ok":                    true,
		"pending_confirmations": board.pendingConfirmationViewsLocked(),
	}, false, nil
}

func (board *kanbanBoard) latestPendingConfirmationIDLocked() string {
	var latest pendingConfirmation
	for _, confirmation := range board.pendingConfirmations {
		if latest.ConfirmationID == "" || confirmation.CreatedAt > latest.CreatedAt {
			latest = confirmation
		}
	}
	return latest.ConfirmationID
}

func (board *kanbanBoard) applyToolCallWithConfirmationBypass(toolName string, args map[string]any, meta toolCallMeta, confirmationID string) (map[string]any, bool, error) {
	before := board.SnapshotState()
	result, changed, err := board.applyToolCall(toolName, args)
	if err == nil && changed {
		after := board.SnapshotState()
		record := board.recordMutation(toolName, args, result, before, after, meta, confirmationID, "")
		board.persistMutationRecord(record, after)
	}
	return result, changed, err
}

func (board *kanbanBoard) recordMutation(toolName string, args map[string]any, result map[string]any, before kanbanBoardState, after kanbanBoardState, meta toolCallMeta, confirmationID string, undoOf string) boardMutationRecord {
	meta = normalizeToolCallMeta(meta)
	board.mu.Lock()
	defer board.mu.Unlock()

	record := boardMutationRecord{
		EventID:       board.nextOperationIDLocked("event"),
		OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:        meta.Source,
		Actor:         meta.Actor,
		ToolName:      toolName,
		Arguments:     cloneToolArgs(args),
		Result:        result,
		RiskLevel:     riskForTool(toolName),
		Confirmation:  confirmationID,
		CallID:        meta.CallID,
		CardIDs:       changedCardIDs(before.Cards, after.Cards, result),
		Summary:       mutationSummary(toolName, args, result),
		BeforeCards:   cloneKanbanCards(before.Cards),
		AfterCards:    cloneKanbanCards(after.Cards),
		BeforeMeeting: cloneScrumMeetingStatePointerValue(before.Meeting),
		AfterMeeting:  cloneScrumMeetingStatePointerValue(after.Meeting),
		Transcript:    board.transcriptEvidenceLocked(meta.Transcript),
		Sequence:      after.SequenceNumber,
		UndoOf:        undoOf,
	}
	board.mutationHistory = append(board.mutationHistory, record)
	if len(board.mutationHistory) > 200 {
		board.mutationHistory = append([]boardMutationRecord(nil), board.mutationHistory[len(board.mutationHistory)-200:]...)
	}
	return record
}

func mutationSummary(toolName string, args map[string]any, result map[string]any) string {
	cardID := firstNonEmptyString(args, "card_id", "source_card_id", "parent_id")
	if cardID == "" {
		cardID = asString(result["card_id"])
	}
	switch toolName {
	case "create_ticket", "create_subtask":
		if card, ok := result["card"].(kanbanCard); ok {
			return fmt.Sprintf("Created %s", card.ID)
		}
		return "Created a ticket"
	case "move_ticket":
		return fmt.Sprintf("Moved %s to %s", cardID, asString(args["status"]))
	case "set_blocked":
		return fmt.Sprintf("Marked %s blocked", cardID)
	case "delete_ticket":
		return fmt.Sprintf("Closed/deleted %s", cardID)
	case "assign_ticket":
		return fmt.Sprintf("Assigned %s", cardID)
	case "unassign_ticket":
		return fmt.Sprintf("Unassigned %s", cardID)
	case "set_eta":
		return fmt.Sprintf("Set ETA for %s", cardID)
	case "set_priority":
		return fmt.Sprintf("Set priority for %s", cardID)
	case "record_participant_update":
		return fmt.Sprintf("Recorded update from %s", firstNonEmptyString(args, "participant", "display_name", "participant_id"))
	default:
		if cardID != "" {
			return fmt.Sprintf("%s on %s", toolName, cardID)
		}
		return toolName
	}
}

func changedCardIDs(before []kanbanCard, after []kanbanCard, result map[string]any) []string {
	ids := map[string]struct{}{}
	if id := asString(result["card_id"]); id != "" {
		ids[id] = struct{}{}
	}
	if card, ok := result["card"].(kanbanCard); ok && card.ID != "" {
		ids[card.ID] = struct{}{}
	}
	beforeByID := map[string]kanbanCard{}
	for _, card := range before {
		beforeByID[card.ID] = card
	}
	afterByID := map[string]kanbanCard{}
	for _, card := range after {
		afterByID[card.ID] = card
	}
	for id, beforeCard := range beforeByID {
		if afterCard, ok := afterByID[id]; !ok || !cardsEquivalent(beforeCard, afterCard) {
			ids[id] = struct{}{}
		}
	}
	for id := range afterByID {
		if _, ok := beforeByID[id]; !ok {
			ids[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func cardsEquivalent(left, right kanbanCard) bool {
	return mustMarshalJSON(left) == mustMarshalJSON(right)
}

func (board *kanbanBoard) transcriptEvidenceLocked(extra string) transcriptEvidence {
	entries := append([]transcriptEntry(nil), board.lastTranscripts...)
	if len(entries) > 8 {
		entries = entries[len(entries)-8:]
	}
	if strings.TrimSpace(extra) != "" {
		entries = append(entries, transcriptEntry{
			Role:      "user",
			Text:      extra,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	var parts []string
	for _, entry := range entries {
		if strings.TrimSpace(entry.Text) != "" {
			parts = append(parts, strings.TrimSpace(entry.Text))
		}
	}
	return transcriptEvidence{
		Entries: entries,
		Summary: truncateString(strings.Join(parts, " "), 1000),
	}
}

func (board *kanbanBoard) RecordTranscript(role, speaker, text string) {
	text = truncateString(text, 2000)
	if text == "" {
		return
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	board.lastTranscripts = append(board.lastTranscripts, transcriptEntry{
		Role:      truncateString(strings.ToLower(strings.TrimSpace(role)), 40),
		Speaker:   truncateString(speaker, 120),
		Text:      text,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if len(board.lastTranscripts) > 50 {
		board.lastTranscripts = append([]transcriptEntry(nil), board.lastTranscripts[len(board.lastTranscripts)-50:]...)
	}
}

func (board *kanbanBoard) LastTranscriptAt() string {
	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lastTranscripts) == 0 {
		return ""
	}
	return board.lastTranscripts[len(board.lastTranscripts)-1].CreatedAt
}

func (board *kanbanBoard) undoLastMutation(args map[string]any, meta toolCallMeta) (map[string]any, bool, error) {
	eventID := asString(args["event_id"])
	board.mu.Lock()
	index := -1
	for i := len(board.mutationHistory) - 1; i >= 0; i-- {
		record := board.mutationHistory[i]
		if record.Reverted || record.ToolName == "undo_last_mutation" {
			continue
		}
		if eventID == "" || record.EventID == eventID {
			index = i
			break
		}
	}
	if index < 0 {
		board.mu.Unlock()
		return map[string]any{"ok": false, "error": "no undoable mutation found"}, false, nil
	}
	target := board.mutationHistory[index]
	before := board.snapshotStateLocked()
	board.cards = cloneKanbanCards(target.BeforeCards)
	if target.BeforeMeeting != nil {
		board.meeting = cloneScrumMeetingState(*target.BeforeMeeting)
	} else {
		board.meeting = scrumMeetingState{}
	}
	board.mutationHistory[index].Reverted = true
	board.touchLocked()
	after := board.snapshotStateLocked()
	board.mu.Unlock()

	result := map[string]any{
		"ok":           true,
		"undone":       true,
		"event_id":     target.EventID,
		"undo_summary": "Undid: " + target.Summary,
		"undo_record":  target,
	}
	record := board.recordMutation("undo_last_mutation", args, result, before, after, meta, "", target.EventID)
	board.persistMutationRecord(record, after)
	broadcastKanbanEventForBoard(board.boardID, "undo_result", boardMutationToView(record))
	return result, true, nil
}

func (board *kanbanBoard) getAuditEvents(args map[string]any) (map[string]any, bool, error) {
	limit, ok := asInt(args["limit"])
	if !ok || limit <= 0 || limit > 50 {
		limit = 20
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	views := board.mutationViewsLocked(limit)
	return map[string]any{"ok": true, "events": views}, false, nil
}

func (board *kanbanBoard) replayAuditEvent(args map[string]any) (map[string]any, bool, error) {
	eventID := asString(args["event_id"])
	if eventID == "" {
		return nil, false, fmt.Errorf("event_id is required")
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	for _, record := range board.mutationHistory {
		if record.EventID == eventID {
			return map[string]any{
				"ok":          true,
				"event":       boardMutationToView(record),
				"before":      kanbanBoardState{Cards: cloneKanbanCards(record.BeforeCards), Meeting: cloneScrumMeetingStatePointerValue(record.BeforeMeeting)},
				"after":       kanbanBoardState{Cards: cloneKanbanCards(record.AfterCards), Meeting: cloneScrumMeetingStatePointerValue(record.AfterMeeting)},
				"explanation": record.Summary,
				"transcript":  record.Transcript,
			}, false, nil
		}
	}
	return map[string]any{"ok": false, "error": "event not found"}, false, nil
}

func (board *kanbanBoard) recordMeetingMemory(args map[string]any) (map[string]any, bool, error) {
	board.mu.Lock()
	defer board.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	board.meeting.Agenda = uniqueStrings(append(board.meeting.Agenda, asStringSlice(args["agenda"])...))
	board.meeting.Decisions = uniqueStrings(append(board.meeting.Decisions, asStringSlice(args["decisions"])...))
	board.meeting.Risks = uniqueStrings(append(board.meeting.Risks, asStringSlice(args["risks"])...))
	board.meeting.ActionItems = uniqueStrings(append(board.meeting.ActionItems, asStringSlice(args["action_items"])...))
	board.meeting.ParkingLot = uniqueStrings(append(board.meeting.ParkingLot, asStringSlice(args["parking_lot"])...))
	for _, item := range asMemoryItems(args["follow_ups"]) {
		board.meeting.FollowUps = append(board.meeting.FollowUps, scrumFollowUp{
			ID:        board.nextOperationIDLocked("follow"),
			Owner:     truncateString(item.Owner, 120),
			Text:      truncateString(item.Text, 1000),
			CardID:    truncateString(item.CardID, 80),
			DueDate:   truncateString(item.DueDate, 40),
			Status:    "open",
			CreatedAt: now,
		})
	}
	for _, item := range asMemoryItems(args["blockers"]) {
		board.meeting.UnresolvedBlockers = append(board.meeting.UnresolvedBlockers, scrumBlocker{
			ID:        board.nextOperationIDLocked("blocker"),
			Owner:     truncateString(item.Owner, 120),
			Text:      truncateString(item.Text, 1000),
			CardID:    truncateString(item.CardID, 80),
			Status:    "open",
			CreatedAt: now,
		})
	}
	for _, item := range asMemoryItems(args["ownership"]) {
		owner := truncateString(item.Owner, 120)
		responsibility := truncateString(item.Text, 1000)
		if owner == "" || responsibility == "" {
			continue
		}
		board.upsertOwnershipLocked(scrumOwnership{
			Owner:          owner,
			CardID:         truncateString(item.CardID, 80),
			Responsibility: responsibility,
			UpdatedAt:      now,
		})
	}
	board.touchLocked()
	return map[string]any{
		"ok":      true,
		"meeting": cloneScrumMeetingState(board.meeting),
	}, true, nil
}

type memoryItem struct {
	Owner   string
	Text    string
	CardID  string
	DueDate string
}

func asMemoryItems(value any) []memoryItem {
	rawValues, ok := value.([]any)
	if !ok {
		stringsValue := asStringSlice(value)
		items := make([]memoryItem, 0, len(stringsValue))
		for _, text := range stringsValue {
			items = append(items, memoryItem{Text: text})
		}
		return items
	}
	items := make([]memoryItem, 0, len(rawValues))
	for _, raw := range rawValues {
		switch typed := raw.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				items = append(items, memoryItem{Text: typed})
			}
		case map[string]any:
			items = append(items, memoryItem{
				Owner:   firstNonEmptyString(typed, "owner", "assignee", "participant"),
				Text:    firstNonEmptyString(typed, "text", "summary", "item", "responsibility"),
				CardID:  firstNonEmptyString(typed, "card_id", "issue_key"),
				DueDate: firstNonEmptyString(typed, "due_date", "eta"),
			})
		}
	}
	return items
}

func (board *kanbanBoard) syncMeetingMemoryFromUpdateLocked(update scrumParticipantUpdate) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if update.Blocker != "" {
		board.meeting.UnresolvedBlockers = append(board.meeting.UnresolvedBlockers, scrumBlocker{
			ID:        board.nextOperationIDLocked("blocker"),
			Owner:     update.Participant,
			Text:      update.Blocker,
			CardID:    update.CardID,
			Status:    "open",
			CreatedAt: now,
		})
	}
	if update.FollowUp != "" {
		board.meeting.FollowUps = append(board.meeting.FollowUps, scrumFollowUp{
			ID:        board.nextOperationIDLocked("follow"),
			Owner:     update.Participant,
			Text:      update.FollowUp,
			CardID:    update.CardID,
			DueDate:   update.ETA,
			Status:    "open",
			CreatedAt: now,
		})
	}
	if update.CardID != "" {
		board.upsertOwnershipLocked(scrumOwnership{
			Owner:          update.Participant,
			CardID:         update.CardID,
			Responsibility: update.Summary,
			UpdatedAt:      now,
		})
	}
}

func (board *kanbanBoard) upsertOwnershipLocked(ownership scrumOwnership) {
	for index := range board.meeting.Ownership {
		existing := &board.meeting.Ownership[index]
		if strings.EqualFold(existing.Owner, ownership.Owner) && strings.EqualFold(existing.CardID, ownership.CardID) {
			existing.Responsibility = ownership.Responsibility
			existing.UpdatedAt = ownership.UpdatedAt
			return
		}
	}
	board.meeting.Ownership = append(board.meeting.Ownership, ownership)
}

func (board *kanbanBoard) generateScrumBriefing(args map[string]any) (map[string]any, bool, error) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	if rawSince := asString(args["since"]); rawSince != "" {
		if parsed, err := time.Parse(time.RFC3339, rawSince); err == nil {
			since = parsed.UTC()
		}
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	briefing := board.scrumBriefingLocked(since)
	board.meeting.LastBriefing = &briefing
	return map[string]any{
		"ok":            true,
		"briefing":      briefing,
		"briefing_text": briefing.Summary,
	}, false, nil
}

func (board *kanbanBoard) scrumBriefingLocked(since time.Time) scrumBriefing {
	moved := 0
	for _, record := range board.mutationHistory {
		occurred, err := time.Parse(time.RFC3339Nano, record.OccurredAt)
		if err != nil || occurred.Before(since) {
			continue
		}
		switch record.ToolName {
		case "move_ticket", "set_blocked", "record_participant_update", "delete_ticket", "undo_last_mutation":
			moved++
		}
	}

	blocked := 0
	unassigned := 0
	prReady := 0
	stale := make([]string, 0)
	for _, card := range board.cards {
		if card.Status == kanbanStatusBlocked {
			blocked++
		}
		if card.Assignee == nil && card.Status != kanbanStatusDone {
			unassigned++
		}
		if cardLooksPRReady(card) {
			prReady++
		}
		if cardLooksStale(card, since) {
			stale = append(stale, card.ID)
		}
	}
	if len(stale) > 5 {
		stale = stale[:5]
	}

	unresolved := make([]string, 0, len(board.meeting.UnresolvedBlockers))
	for _, blocker := range board.meeting.UnresolvedBlockers {
		if blocker.Status == "" || blocker.Status == "open" {
			unresolved = append(unresolved, blocker.Text)
		}
	}
	if len(unresolved) > 5 {
		unresolved = unresolved[:5]
	}

	questions := make([]string, 0)
	if blocked > 0 {
		questions = append(questions, "Who owns unblocking the blocked work?")
	}
	if unassigned > 0 {
		questions = append(questions, "Which unassigned issues need owners today?")
	}
	if len(stale) > 0 {
		questions = append(questions, "Should stale work stay in scope?")
	}

	summary := fmt.Sprintf("Since yesterday, %d ticket%s moved, %d PR%s look ready, %d item%s are blocked, %d open item%s have no owner",
		moved, plural(moved), prReady, plural(prReady), blocked, plural(blocked), unassigned, plural(unassigned))
	if len(stale) > 0 {
		summary += ", and " + strings.Join(stale, ", ") + " look stale"
	}
	summary += "."

	return scrumBriefing{
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		Since:                since.Format(time.RFC3339Nano),
		Summary:              summary,
		TicketsMoved:         moved,
		PRsReady:             prReady,
		BlockedCount:         blocked,
		UnassignedCount:      unassigned,
		StaleCards:           stale,
		UnresolvedBlockers:   unresolved,
		RecommendedQuestions: questions,
	}
}

func cardLooksPRReady(card kanbanCard) bool {
	for _, tag := range card.Tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "pr-ready" || normalized == "ready-for-review" || normalized == "review" {
			return true
		}
	}
	for _, link := range card.RemoteLinks {
		normalized := strings.ToLower(link.URL + " " + link.Title + " " + link.Summary)
		if strings.Contains(normalized, "/pull/") || strings.Contains(normalized, "pull request") || strings.Contains(normalized, "pr ready") {
			return true
		}
	}
	for _, comment := range card.Comments {
		normalized := strings.ToLower(comment.Body)
		if strings.Contains(normalized, "pr ready") || strings.Contains(normalized, "pull request ready") {
			return true
		}
	}
	return false
}

func cardLooksStale(card kanbanCard, since time.Time) bool {
	if card.Status == kanbanStatusDone {
		return false
	}
	if card.DueDate != "" {
		due, err := time.Parse("2006-01-02", card.DueDate)
		if err == nil && due.Before(time.Now().UTC().Truncate(24*time.Hour)) {
			return true
		}
	}
	_ = since
	return false
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func (board *kanbanBoard) nextOperationIDLocked(prefix string) string {
	board.operationCounter++
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UTC().UnixNano(), board.operationCounter)
}

func pendingConfirmationToView(confirmation pendingConfirmation) pendingConfirmationView {
	confidence, reasons := confidenceForToolAction(confirmation.ToolName, confirmation.Arguments, confirmation.RiskLevel, true)
	return pendingConfirmationView{
		ConfirmationID:    confirmation.ConfirmationID,
		ToolName:          confirmation.ToolName,
		RiskLevel:         confirmation.RiskLevel,
		Prompt:            confirmation.Prompt,
		Source:            confirmation.Source,
		Actor:             confirmation.Actor,
		Confidence:        confidence,
		ConfidenceReasons: reasons,
		MatchedCardID:     firstNonEmptyString(confirmation.Arguments, "card_id", "source_card_id", "parent_id"),
		GuardrailDecision: "awaiting human confirmation",
		CreatedAt:         confirmation.CreatedAt,
		ExpiresAt:         confirmation.ExpiresAt,
	}
}

func boardMutationToView(record boardMutationRecord) boardMutationView {
	confidence, reasons := confidenceForToolAction(record.ToolName, record.Arguments, record.RiskLevel, record.Confirmation != "")
	return boardMutationView{
		EventID:           record.EventID,
		OccurredAt:        record.OccurredAt,
		Source:            record.Source,
		Actor:             record.Actor,
		ToolName:          record.ToolName,
		RiskLevel:         record.RiskLevel,
		Confirmation:      record.Confirmation,
		CardIDs:           append([]string(nil), record.CardIDs...),
		Summary:           record.Summary,
		Confidence:        confidence,
		ConfidenceReasons: reasons,
		MatchedCardID:     firstNonEmpty(record.CardIDs...),
		GuardrailDecision: guardrailDecisionForMutation(record),
		Transcript:        record.Transcript,
		Sequence:          record.Sequence,
		Reverted:          record.Reverted,
		UndoOf:            record.UndoOf,
	}
}

func confidenceForToolAction(toolName string, args map[string]any, risk toolRiskLevel, confirmed bool) (float64, []string) {
	reasons := []string{fmt.Sprintf("Server selected %s with %s risk.", toolName, risk)}
	score := 0.9
	if cardID := firstNonEmptyString(args, "card_id", "source_card_id", "parent_id"); cardID != "" {
		reasons = append(reasons, "Matched explicit card id "+cardID+".")
	} else {
		score -= 0.08
		reasons = append(reasons, "No explicit card id was present.")
	}
	switch risk {
	case toolRiskHigh:
		score -= 0.18
		reasons = append(reasons, "High-risk Jira action needs explicit confirmation.")
	case toolRiskMedium:
		score -= 0.1
		reasons = append(reasons, "Medium-risk Jira action needs explicit confirmation.")
	default:
		reasons = append(reasons, "Low-risk action can proceed without confirmation.")
	}
	if confirmed {
		score += 0.05
		reasons = append(reasons, "A human confirmation gate was satisfied.")
	}
	if score < 0.35 {
		score = 0.35
	}
	if score > 0.98 {
		score = 0.98
	}
	return score, reasons
}

func guardrailDecisionForMutation(record boardMutationRecord) string {
	if record.Confirmation != "" {
		return "confirmed before Jira mutation"
	}
	if record.RiskLevel == toolRiskLow {
		return "allowed as low-risk meeting action"
	}
	return "allowed by server policy"
}

func (board *kanbanBoard) mutationViewsLocked(limit int) []boardMutationView {
	if limit <= 0 {
		limit = 20
	}
	start := len(board.mutationHistory) - limit
	if start < 0 {
		start = 0
	}
	views := make([]boardMutationView, 0, len(board.mutationHistory)-start)
	for index := len(board.mutationHistory) - 1; index >= start; index-- {
		views = append(views, boardMutationToView(board.mutationHistory[index]))
	}
	return views
}

func (board *kanbanBoard) pendingConfirmationViewsLocked() []pendingConfirmationView {
	views := make([]pendingConfirmationView, 0, len(board.pendingConfirmations))
	now := time.Now().UTC()
	for id, confirmation := range board.pendingConfirmations {
		if expiresAt, err := time.Parse(time.RFC3339Nano, confirmation.ExpiresAt); err == nil && now.After(expiresAt) {
			delete(board.pendingConfirmations, id)
			continue
		}
		views = append(views, pendingConfirmationToView(confirmation))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt > views[j].CreatedAt })
	return views
}

func cloneScrumMeetingStatePointerValue(meeting *scrumMeetingState) *scrumMeetingState {
	if meeting == nil {
		return nil
	}
	cloned := cloneScrumMeetingState(*meeting)
	return &cloned
}

func (board *kanbanBoard) persistMutationRecord(record boardMutationRecord, state kanbanBoardState) {
	if board.store == nil {
		return
	}
	if err := board.store.SaveSnapshot(context.Background(), board.boardID, state); err != nil {
		log.Errorf("Failed to persist board snapshot: %v", err)
	}
	event := boardEventRecord{
		BoardID:        board.boardID,
		OccurredAt:     record.OccurredAt,
		ToolName:       record.ToolName,
		Arguments:      record.Arguments,
		Result:         record.Result,
		SequenceNumber: state.SequenceNumber,
		EventID:        record.EventID,
		Source:         record.Source,
		Actor:          record.Actor,
		RiskLevel:      string(record.RiskLevel),
		ConfirmationID: record.Confirmation,
		UndoOf:         record.UndoOf,
		Summary:        record.Summary,
	}
	if err := board.store.AppendEvent(context.Background(), board.boardID, event, state); err != nil {
		log.Errorf("Failed to persist board event: %v", err)
	}
}
