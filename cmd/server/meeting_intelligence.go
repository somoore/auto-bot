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

// pendingConfirmationView is the client-safe representation of a medium/high
// risk action waiting for host confirmation.
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

// transcriptEntry is the retained meeting transcript shape used for audit and
// intelligence reports. CreatedAt is RFC3339Nano UTC.
type transcriptEntry struct {
	Role           string `json:"role"`
	Speaker        string `json:"speaker,omitempty"`
	Text           string `json:"text"`
	OriginalText   string `json:"original_text,omitempty"`
	TranslatedText string `json:"translated_text,omitempty"`
	Language       string `json:"language,omitempty"`
	InputMode      string `json:"input_mode,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

type transcriptEvidence struct {
	Entries []transcriptEntry `json:"entries,omitempty"`
	Summary string            `json:"summary,omitempty"`
}

type externalActionConfirmation struct {
	System      string `json:"system"`
	Operation   string `json:"operation,omitempty"`
	Required    bool   `json:"required"`
	Configured  bool   `json:"configured"`
	OK          bool   `json:"ok"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
	ConfirmedAt string `json:"confirmedAt,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

type boardMutationRecord struct {
	EventID               string                       `json:"eventId"`
	OccurredAt            string                       `json:"occurredAt"`
	Source                string                       `json:"source"`
	Actor                 string                       `json:"actor,omitempty"`
	ToolName              string                       `json:"toolName"`
	Arguments             map[string]any               `json:"arguments,omitempty"`
	Result                map[string]any               `json:"result,omitempty"`
	RiskLevel             toolRiskLevel                `json:"riskLevel"`
	Confirmation          string                       `json:"confirmationId,omitempty"`
	CallID                string                       `json:"callId,omitempty"`
	CardIDs               []string                     `json:"cardIds,omitempty"`
	Summary               string                       `json:"summary"`
	ExternalConfirmations []externalActionConfirmation `json:"externalConfirmations,omitempty"`
	BeforeCards           []kanbanCard                 `json:"beforeCards,omitempty"`
	AfterCards            []kanbanCard                 `json:"afterCards,omitempty"`
	BeforeMeeting         *scrumMeetingState           `json:"beforeMeeting,omitempty"`
	AfterMeeting          *scrumMeetingState           `json:"afterMeeting,omitempty"`
	Transcript            transcriptEvidence           `json:"transcript,omitempty"`
	Sequence              int64                        `json:"sequenceNumber"`
	Reverted              bool                         `json:"reverted,omitempty"`
	UndoOf                string                       `json:"undoOf,omitempty"`
}

// boardMutationView is the client-safe audit summary for one board or Jira
// mutation.
type boardMutationView struct {
	EventID               string                       `json:"eventId"`
	OccurredAt            string                       `json:"occurredAt"`
	Source                string                       `json:"source"`
	Actor                 string                       `json:"actor,omitempty"`
	ToolName              string                       `json:"toolName"`
	RiskLevel             toolRiskLevel                `json:"riskLevel"`
	Confirmation          string                       `json:"confirmationId,omitempty"`
	CardIDs               []string                     `json:"cardIds,omitempty"`
	Summary               string                       `json:"summary"`
	Confidence            float64                      `json:"confidence,omitempty"`
	ConfidenceReasons     []string                     `json:"confidenceReasons,omitempty"`
	MatchedCardID         string                       `json:"matchedCardId,omitempty"`
	GuardrailDecision     string                       `json:"guardrailDecision,omitempty"`
	ExternalConfirmations []externalActionConfirmation `json:"externalConfirmations,omitempty"`
	APIStatus             string                       `json:"apiStatus,omitempty"`
	Transcript            transcriptEvidence           `json:"transcript,omitempty"`
	Sequence              int64                        `json:"sequenceNumber"`
	Reverted              bool                         `json:"reverted,omitempty"`
	UndoOf                string                       `json:"undoOf,omitempty"`
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

// jiraConflict is the client-visible local-vs-Jira divergence record used to
// ask the meeting host which version should win.
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
	case "delete_ticket", "set_sprint", "rank_issue", "prioritize_ticket":
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
	case "rank_issue", "prioritize_ticket":
		if targetCardID := firstNonEmptyString(args, "above_card_id", "before_card_id", "below_card_id", "after_card_id"); targetCardID != "" {
			return fmt.Sprintf("I heard you want to reorder %s relative to %s. Confirm?", target, targetCardID)
		}
		if position := asString(args["position"]); position != "" {
			column := firstNonEmptyString(args, "target_status", "status", "column")
			if column != "" {
				return fmt.Sprintf("I heard you want to move %s to the %s of %s. Confirm?", target, position, column)
			}
			return fmt.Sprintf("I heard you want to move %s to the %s of its column. Confirm?", target, position)
		}
		return fmt.Sprintf("I heard you want to reorder %s in Jira. Confirm?", target)
	default:
		return fmt.Sprintf("I heard you want to run %s on %s. Confirm?", toolName, target)
	}
}

func (board *kanbanBoard) confirmPendingAction(args map[string]any, meta toolCallMeta) (map[string]any, bool, error) {
	confirmationID := asString(args["confirmation_id"])
	board.mu.Lock()
	// Guard mixed-risk sweeps: a bare/generic affirmation ("yes", "ok") must not
	// confirm every pending action when those actions span more than one risk
	// level, because the host may have only intended to approve one of them. An
	// explicit aggregate phrase ("yes to all", "both") still sweeps, and a same-
	// risk pending set still sweeps. When blocked, leave the confirmations
	// pending and ask the host to name which one.
	if disambiguation := board.mixedRiskSweepDisambiguationLocked(confirmationID); disambiguation != nil {
		board.mu.Unlock()
		return disambiguation, false, nil
	}
	confirmations := board.takePendingConfirmationsLocked(confirmationID)
	board.mu.Unlock()
	if len(confirmations) == 0 {
		return map[string]any{"ok": false, "error": "pending confirmation not found"}, false, nil
	}

	meta = normalizeToolCallMeta(meta)
	if len(confirmations) == 1 {
		return board.executePendingConfirmation(confirmations[0], meta)
	}

	actions := make([]any, 0, len(confirmations))
	confirmedIDs := make([]string, 0, len(confirmations))
	expiredIDs := make([]string, 0)
	changedAny := false
	var firstErr error
	for _, confirmation := range confirmations {
		result, changed, err := board.executePendingConfirmation(confirmation, meta)
		if result == nil {
			result = map[string]any{}
		}
		result["changed"] = changed
		actions = append(actions, result)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if asBool(result["confirmed"]) {
			confirmedIDs = append(confirmedIDs, confirmation.ConfirmationID)
		}
		if asString(result["error"]) == "pending confirmation expired" {
			expiredIDs = append(expiredIDs, confirmation.ConfirmationID)
		}
		changedAny = changedAny || changed
	}

	result := map[string]any{
		"ok":                firstErr == nil && len(confirmedIDs) > 0,
		"confirmed":         len(confirmedIDs) > 0,
		"confirmed_count":   len(confirmedIDs),
		"confirmation_ids":  confirmedIDs,
		"confirmed_actions": actions,
		"summary":           fmt.Sprintf("Confirmed %d pending actions.", len(confirmedIDs)),
	}
	if len(expiredIDs) > 0 {
		result["expired_confirmation_ids"] = expiredIDs
	}
	if firstErr != nil {
		result["ok"] = false
		result["error"] = "one or more pending confirmations failed: " + firstErr.Error()
	}
	return result, changedAny, nil
}

func (board *kanbanBoard) executePendingConfirmation(confirmation pendingConfirmation, meta toolCallMeta) (map[string]any, bool, error) {
	if expiresAt, err := time.Parse(time.RFC3339Nano, confirmation.ExpiresAt); err == nil && time.Now().UTC().After(expiresAt) {
		return map[string]any{"ok": false, "error": "pending confirmation expired", "confirmation_id": confirmation.ConfirmationID}, false, nil
	}

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
	confirmations := board.takePendingConfirmationsLocked(confirmationID)
	board.mu.Unlock()
	if len(confirmations) == 0 {
		return map[string]any{"ok": false, "error": "pending confirmation not found"}, false, nil
	}
	cancelledIDs := make([]string, 0, len(confirmations))
	cancelled := make([]any, 0, len(confirmations))
	for _, confirmation := range confirmations {
		broadcastKanbanEventForBoard(board.boardID, "confirmation_cancelled", pendingConfirmationToView(confirmation))
		cancelledIDs = append(cancelledIDs, confirmation.ConfirmationID)
		cancelled = append(cancelled, map[string]any{
			"confirmation_id": confirmation.ConfirmationID,
			"tool_name":       confirmation.ToolName,
			"risk_level":      confirmation.RiskLevel,
		})
	}
	if len(confirmations) == 1 {
		return map[string]any{"ok": true, "cancelled": true, "confirmation_id": confirmations[0].ConfirmationID}, false, nil
	}
	return map[string]any{
		"ok":                      true,
		"cancelled":               true,
		"cancelled_count":         len(cancelledIDs),
		"confirmation_ids":        cancelledIDs,
		"cancelled_confirmations": cancelled,
	}, false, nil
}

func (board *kanbanBoard) listPendingConfirmations() (map[string]any, bool, error) {
	board.mu.Lock()
	defer board.mu.Unlock()
	return map[string]any{
		"ok":                    true,
		"pending_confirmations": board.pendingConfirmationViewsLocked(),
	}, false, nil
}

func (board *kanbanBoard) takePendingConfirmationsLocked(confirmationID string) []pendingConfirmation {
	confirmationID = strings.TrimSpace(confirmationID)
	if confirmationID != "" {
		confirmation, ok := board.pendingConfirmations[confirmationID]
		if ok {
			delete(board.pendingConfirmations, confirmationID)
			return []pendingConfirmation{confirmation}
		}
		if confirmations := board.takePendingConfirmationsByReferenceLocked(confirmationID); len(confirmations) > 0 {
			return confirmations
		}
		if confirmationReferenceMeansAll(confirmationID) {
			return board.takeAllPendingConfirmationsLocked()
		}
		return nil
	}

	return board.takeAllPendingConfirmationsLocked()
}

// pendingConfirmationToolsForReference reports the distinct tool names that a
// confirm_action / cancel_confirmation with the given confirmation_id reference
// would resolve, WITHOUT mutating the pending set. It mirrors the selection
// logic of takePendingConfirmationsLocked so the authorization gate inspects
// exactly the same set the resolution will act on. An empty reference means
// "all pending". Returns nil when the reference matches nothing.
func (board *kanbanBoard) pendingConfirmationToolsForReference(confirmationID string) []string {
	board.mu.Lock()
	defer board.mu.Unlock()

	confirmationID = strings.TrimSpace(confirmationID)
	var selected []pendingConfirmation
	switch {
	case confirmationID == "":
		selected = board.pendingConfirmationsSliceLocked()
	default:
		if confirmation, ok := board.pendingConfirmations[confirmationID]; ok {
			selected = []pendingConfirmation{confirmation}
		} else if byRef := board.pendingConfirmationsMatchingReferenceLocked(confirmationID); len(byRef) > 0 {
			selected = byRef
		} else if confirmationReferenceMeansAll(confirmationID) {
			selected = board.pendingConfirmationsSliceLocked()
		}
	}
	if len(selected) == 0 {
		return nil
	}
	tools := make([]string, 0, len(selected))
	for _, confirmation := range selected {
		tools = append(tools, confirmation.ToolName)
	}
	return tools
}

// pendingConfirmationsSliceLocked returns a copy of all pending confirmations
// without removing them. Callers must hold board.mu.
func (board *kanbanBoard) pendingConfirmationsSliceLocked() []pendingConfirmation {
	out := make([]pendingConfirmation, 0, len(board.pendingConfirmations))
	for _, confirmation := range board.pendingConfirmations {
		out = append(out, confirmation)
	}
	return out
}

// pendingConfirmationsMatchingReferenceLocked is the read-only counterpart of
// takePendingConfirmationsByReferenceLocked: it returns matches without deleting
// them. Callers must hold board.mu.
func (board *kanbanBoard) pendingConfirmationsMatchingReferenceLocked(reference string) []pendingConfirmation {
	normalizedReference := strings.ToLower(strings.TrimSpace(reference))
	confirmations := make([]pendingConfirmation, 0, len(board.pendingConfirmations))
	for id, confirmation := range board.pendingConfirmations {
		if strings.Contains(normalizedReference, strings.ToLower(id)) {
			confirmations = append(confirmations, confirmation)
		}
	}
	return confirmations
}

func (board *kanbanBoard) takePendingConfirmationsByReferenceLocked(reference string) []pendingConfirmation {
	normalizedReference := strings.ToLower(strings.TrimSpace(reference))
	confirmations := make([]pendingConfirmation, 0, len(board.pendingConfirmations))
	for id, confirmation := range board.pendingConfirmations {
		if strings.Contains(normalizedReference, strings.ToLower(id)) {
			confirmations = append(confirmations, confirmation)
		}
	}
	if len(confirmations) == 0 {
		return nil
	}
	for _, confirmation := range confirmations {
		delete(board.pendingConfirmations, confirmation.ConfirmationID)
	}
	sort.Slice(confirmations, func(i, j int) bool {
		return confirmations[i].CreatedAt < confirmations[j].CreatedAt
	})
	return confirmations
}

// mixedRiskSweepDisambiguationLocked returns a non-nil disambiguation response
// when a generic affirmation would otherwise sweep a mixed-risk pending set.
// It returns nil (allowing the normal take to proceed) for explicit-id
// confirmations, explicit aggregate phrases ("all"/"both"), single pending
// actions, and same-risk pending sets. Callers must hold board.mu.
func (board *kanbanBoard) mixedRiskSweepDisambiguationLocked(confirmationID string) map[string]any {
	// Only generic affirmations that resolve to "confirm all" are gated. An
	// empty reference, or a reference that means-all but is not an explicit
	// aggregate phrase, counts as generic.
	trimmed := strings.TrimSpace(confirmationID)
	if trimmed != "" {
		// A specific id or a reference matching specific ids is not a generic
		// sweep, so let the normal path handle it.
		if _, ok := board.pendingConfirmations[trimmed]; ok {
			return nil
		}
		if board.referenceMatchesSpecificConfirmationLocked(trimmed) {
			return nil
		}
		if !confirmationReferenceMeansAll(trimmed) {
			return nil
		}
		if confirmationReferenceIsExplicitAll(trimmed) {
			return nil
		}
	}

	if len(board.pendingConfirmations) <= 1 || !board.pendingConfirmationsAreMixedRiskLocked() {
		return nil
	}

	return map[string]any{
		"ok":                      false,
		"requires_disambiguation": true,
		"reason":                  "mixed_risk_pending_set",
		// prompt is read aloud/relayed to the human. instruction is for the
		// model: it must ask the human and must NOT self-confirm, because only
		// live user speech may authorize these actions.
		"prompt":                     "These pending actions are at different risk levels. Which one should I confirm? Name it, or say \"yes to all\" to confirm every pending action.",
		"instruction":                "Ask the live user the prompt above and wait for their answer. Do NOT call confirm_action with an aggregate phrase yourself; the bare confirmation was ambiguous and only the live user may resolve a mixed-risk set.",
		"pending_confirmations":      board.pendingConfirmationViewsLocked(),
		"pending_confirmation_count": len(board.pendingConfirmations),
	}
}

// referenceMatchesSpecificConfirmationLocked reports whether the reference
// targets specific pending confirmation ids (rather than meaning "all").
// Callers must hold board.mu.
func (board *kanbanBoard) referenceMatchesSpecificConfirmationLocked(reference string) bool {
	normalizedReference := strings.ToLower(strings.TrimSpace(reference))
	for id := range board.pendingConfirmations {
		if strings.Contains(normalizedReference, strings.ToLower(id)) {
			return true
		}
	}
	return false
}

// pendingConfirmationsAreMixedRiskLocked reports whether the pending set spans
// more than one distinct risk level. Callers must hold board.mu.
func (board *kanbanBoard) pendingConfirmationsAreMixedRiskLocked() bool {
	var firstRisk toolRiskLevel
	seenFirst := false
	for _, confirmation := range board.pendingConfirmations {
		if !seenFirst {
			firstRisk = confirmation.RiskLevel
			seenFirst = true
			continue
		}
		if confirmation.RiskLevel != firstRisk {
			return true
		}
	}
	return false
}

func (board *kanbanBoard) takeAllPendingConfirmationsLocked() []pendingConfirmation {
	confirmations := make([]pendingConfirmation, 0, len(board.pendingConfirmations))
	for id, confirmation := range board.pendingConfirmations {
		delete(board.pendingConfirmations, id)
		confirmations = append(confirmations, confirmation)
	}
	sort.Slice(confirmations, func(i, j int) bool {
		return confirmations[i].CreatedAt < confirmations[j].CreatedAt
	})
	return confirmations
}

func confirmationReferenceMeansAll(reference string) bool {
	raw := strings.ToLower(strings.TrimSpace(reference))
	normalized := normalizeConfirmationReference(reference)
	if normalized == "" {
		return false
	}
	for _, blocker := range []string{"no", "not", "dont", "do not", "cancel", "except", "only"} {
		if normalized == blocker || strings.Contains(normalized, " "+blocker+" ") || strings.HasPrefix(normalized, blocker+" ") || strings.HasSuffix(normalized, " "+blocker) {
			return false
		}
	}
	switch normalized {
	case "all", "all actions", "all pending", "all pending actions",
		"both", "both actions", "both of them", "the two", "two", "2",
		"yes", "yes both", "yes to both", "yeah", "yep", "y", "ok", "okay",
		"confirm", "confirmed", "confirm both", "i confirm", "i confirm both",
		"them", "these", "those", "everything":
		return true
	default:
		words := confirmationReferenceWords(normalized)
		hasAggregateWord := words["both"] || words["all"] || words["everything"]
		hasConfirmationWord := words["yes"] || words["yeah"] || words["yep"] || words["ok"] || words["okay"] || words["confirm"] || words["confirmed"]
		if strings.Contains(raw, "confirm-") {
			return hasAggregateWord
		}
		return hasAggregateWord || hasConfirmationWord
	}
}

// confirmationReferenceIsExplicitAll reports whether the reference explicitly
// asks to confirm the entire pending set ("all", "both", "yes to all",
// "everything", "the two"), as opposed to a bare affirmation ("yes", "ok").
// Only an explicit aggregate phrase is allowed to sweep a mixed-risk set.
func confirmationReferenceIsExplicitAll(reference string) bool {
	normalized := normalizeConfirmationReference(reference)
	if normalized == "" {
		return false
	}
	switch normalized {
	case "all", "all actions", "all pending", "all pending actions",
		"both", "both actions", "both of them", "the two", "two", "2",
		"yes both", "yes to both", "yes all", "yes to all", "confirm both",
		"i confirm both", "everything", "them", "these", "those":
		return true
	}
	words := confirmationReferenceWords(normalized)
	return words["all"] || words["both"] || words["everything"]
}

func normalizeConfirmationReference(reference string) string {
	replacer := strings.NewReplacer(
		"_", " ",
		"-", " ",
		",", " ",
		".", " ",
		"!", " ",
		"?", " ",
		":", " ",
		";", " ",
		"/", " ",
		"\\", " ",
		"\"", "",
		"'", "",
	)
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(strings.TrimSpace(reference)))), " ")
}

func confirmationReferenceWords(normalized string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.Fields(normalized) {
		words[word] = true
	}
	return words
}

func confirmedActionResults(result map[string]any) []map[string]any {
	if result == nil {
		return nil
	}
	raw, ok := result["confirmed_actions"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		actions := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if action, ok := item.(map[string]any); ok {
				actions = append(actions, action)
			}
		}
		return actions
	default:
		return nil
	}
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

	eventID := board.nextOperationIDLocked("event")
	if result != nil {
		result["audit_event_id"] = eventID
	}
	record := boardMutationRecord{
		EventID:       eventID,
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

func (board *kanbanBoard) attachExternalConfirmationsToMutation(result map[string]any) {
	if board == nil || result == nil {
		return
	}
	for _, action := range confirmedActionResults(result) {
		board.attachExternalConfirmationsToMutation(action)
	}
	confirmations := externalConfirmationsFromResult(result)
	if len(confirmations) == 0 {
		return
	}
	eventID := asString(result["audit_event_id"])
	if eventID == "" {
		return
	}

	board.mu.Lock()
	index := -1
	for i := len(board.mutationHistory) - 1; i >= 0; i-- {
		if board.mutationHistory[i].EventID == eventID {
			index = i
			break
		}
	}
	if index < 0 {
		board.mu.Unlock()
		return
	}
	board.mutationHistory[index].ExternalConfirmations = confirmations
	board.mutationHistory[index].Result = cloneToolArgs(result)
	record := board.mutationHistory[index]
	state := board.snapshotStateLocked()
	board.mu.Unlock()

	board.persistMutationRecord(record, state)
}

func externalConfirmationsFromResult(result map[string]any) []externalActionConfirmation {
	raw, ok := result["external_confirmations"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []externalActionConfirmation:
		return cloneExternalActionConfirmations(typed)
	case []any:
		confirmations := make([]externalActionConfirmation, 0, len(typed))
		for _, item := range typed {
			if confirmation := externalConfirmationFromAny(item); confirmation.System != "" {
				confirmations = append(confirmations, confirmation)
			}
		}
		return confirmations
	default:
		if confirmation := externalConfirmationFromAny(typed); confirmation.System != "" {
			return []externalActionConfirmation{confirmation}
		}
		return nil
	}
}

func externalConfirmationFromAny(value any) externalActionConfirmation {
	switch typed := value.(type) {
	case externalActionConfirmation:
		return typed
	case map[string]any:
		return externalActionConfirmation{
			System:      truncateString(asString(typed["system"]), 80),
			Operation:   truncateString(asString(typed["operation"]), 120),
			Required:    asBool(typed["required"]),
			Configured:  asBool(typed["configured"]),
			OK:          asBool(typed["ok"]),
			Message:     truncateString(asString(typed["message"]), 500),
			Error:       truncateString(asString(typed["error"]), 500),
			ConfirmedAt: truncateString(firstNonEmpty(asString(typed["confirmedAt"]), asString(typed["confirmed_at"])), 80),
			Evidence:    truncateString(asString(typed["evidence"]), 500),
		}
	default:
		return externalActionConfirmation{}
	}
}

func cloneExternalActionConfirmations(confirmations []externalActionConfirmation) []externalActionConfirmation {
	if len(confirmations) == 0 {
		return nil
	}
	out := make([]externalActionConfirmation, len(confirmations))
	copy(out, confirmations)
	return out
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
	case "rank_issue", "prioritize_ticket":
		if targetCardID := firstNonEmptyString(args, "above_card_id", "before_card_id", "below_card_id", "after_card_id"); targetCardID != "" {
			return fmt.Sprintf("Prioritized %s relative to %s", cardID, targetCardID)
		}
		if position := asString(result["position"]); position != "" {
			return fmt.Sprintf("Prioritized %s to %s of %s", cardID, position, asString(result["status"]))
		}
		return fmt.Sprintf("Prioritized %s", cardID)
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

// RecordTranscript records a simple transcript entry using the same truncation,
// timestamp, and retention rules as RecordTranscriptEntry.
func (board *kanbanBoard) RecordTranscript(role, speaker, text string) {
	board.RecordTranscriptEntry(transcriptEntry{
		Role:    role,
		Speaker: speaker,
		Text:    text,
	})
}

// RecordTranscriptEntry stores a sanitized transcript entry for meeting
// intelligence and audit evidence. Text fields are truncated, CreatedAt is
// filled with an RFC3339Nano UTC timestamp when omitted, and only the latest 50
// entries are retained in memory.
func (board *kanbanBoard) RecordTranscriptEntry(entry transcriptEntry) {
	entry.Text = truncateString(entry.Text, 2000)
	if entry.Text == "" {
		return
	}
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	entry.Role = truncateString(strings.ToLower(strings.TrimSpace(entry.Role)), 40)
	entry.Speaker = truncateString(entry.Speaker, 120)
	entry.OriginalText = truncateString(entry.OriginalText, 2000)
	entry.TranslatedText = truncateString(entry.TranslatedText, 2000)
	entry.Language = truncateString(strings.ToLower(strings.TrimSpace(entry.Language)), 40)
	entry.InputMode = truncateString(strings.ToLower(strings.TrimSpace(entry.InputMode)), 40)
	board.mu.Lock()
	defer board.mu.Unlock()
	board.lastTranscripts = append(board.lastTranscripts, entry)
	if len(board.lastTranscripts) > 50 {
		board.lastTranscripts = append([]transcriptEntry(nil), board.lastTranscripts[len(board.lastTranscripts)-50:]...)
	}
}

// LastTranscriptAt returns the RFC3339Nano timestamp for the latest retained
// transcript entry, or an empty string when no transcript has been recorded.
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
				"tool_call": map[string]any{
					"source":       record.Source,
					"actor":        record.Actor,
					"tool_name":    record.ToolName,
					"arguments":    cloneToolArgs(record.Arguments),
					"call_id":      record.CallID,
					"risk_level":   record.RiskLevel,
					"confirmation": record.Confirmation,
				},
				"api_confirmations": cloneExternalActionConfirmations(record.ExternalConfirmations),
				"api_status":        apiStatusForMutation(record),
				"replay_steps":      replayStepsForMutation(record),
			}, false, nil
		}
	}
	return map[string]any{"ok": false, "error": "event not found"}, false, nil
}

func replayStepsForMutation(record boardMutationRecord) []map[string]any {
	steps := make([]map[string]any, 0, 5)
	transcript := strings.TrimSpace(record.Transcript.Summary)
	if transcript == "" {
		transcript = "No transcript evidence was captured for this action."
	}
	steps = append(steps, map[string]any{
		"label":  "Live speech evidence",
		"status": "captured",
		"detail": transcript,
	})

	toolDetail := record.ToolName
	if len(record.CardIDs) > 0 {
		toolDetail += " on " + strings.Join(record.CardIDs, ", ")
	}
	steps = append(steps, map[string]any{
		"label":  "Tool selected",
		"status": "selected",
		"detail": toolDetail,
	})

	steps = append(steps, map[string]any{
		"label":  "Guardrail and confidence",
		"status": guardrailDecisionForMutation(record),
		"detail": strings.Join(confidenceReasonsForReplay(record), " "),
	})

	if len(record.ExternalConfirmations) == 0 {
		steps = append(steps, map[string]any{
			"label":  "External API",
			"status": "not required",
			"detail": "This action did not require a Jira or GitHub write.",
		})
	} else {
		for _, confirmation := range record.ExternalConfirmations {
			status := externalActionStatus(confirmation)
			detail := confirmation.Message
			if confirmation.Error != "" {
				detail += " Error: " + confirmation.Error
			}
			if confirmation.Evidence != "" {
				detail += " Evidence: " + confirmation.Evidence
			}
			steps = append(steps, map[string]any{
				"label":  titleWord(firstNonEmpty(confirmation.System, "external")) + " API",
				"status": status,
				"detail": strings.TrimSpace(detail),
			})
		}
	}

	steps = append(steps, map[string]any{
		"label":  "Board state",
		"status": "recorded",
		"detail": record.Summary,
	})
	return steps
}

func titleWord(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func confidenceReasonsForReplay(record boardMutationRecord) []string {
	_, reasons := confidenceForMutation(record)
	return reasons
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
	confidence, reasons := confidenceForMutation(record)
	return boardMutationView{
		EventID:               record.EventID,
		OccurredAt:            record.OccurredAt,
		Source:                record.Source,
		Actor:                 record.Actor,
		ToolName:              record.ToolName,
		RiskLevel:             record.RiskLevel,
		Confirmation:          record.Confirmation,
		CardIDs:               append([]string(nil), record.CardIDs...),
		Summary:               record.Summary,
		Confidence:            confidence,
		ConfidenceReasons:     reasons,
		MatchedCardID:         firstNonEmpty(record.CardIDs...),
		GuardrailDecision:     guardrailDecisionForMutation(record),
		ExternalConfirmations: cloneExternalActionConfirmations(record.ExternalConfirmations),
		APIStatus:             apiStatusForMutation(record),
		Transcript:            record.Transcript,
		Sequence:              record.Sequence,
		Reverted:              record.Reverted,
		UndoOf:                record.UndoOf,
	}
}

func confidenceForMutation(record boardMutationRecord) (float64, []string) {
	score, reasons := confidenceForToolAction(record.ToolName, record.Arguments, record.RiskLevel, record.Confirmation != "")
	for _, confirmation := range record.ExternalConfirmations {
		if !confirmation.Required {
			continue
		}
		switch {
		case confirmation.OK:
			score += 0.04
			reasons = append(reasons, fmt.Sprintf("%s API confirmed the write.", confirmation.System))
		case !confirmation.Configured:
			score -= 0.2
			reasons = append(reasons, fmt.Sprintf("%s API was not configured; only local state changed.", confirmation.System))
		default:
			score -= 0.25
			reasons = append(reasons, fmt.Sprintf("%s API did not confirm the write.", confirmation.System))
		}
	}
	if score < 0.1 {
		score = 0.1
	}
	if score > 0.98 {
		score = 0.98
	}
	return score, reasons
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
	if status := apiStatusForMutation(record); status == "api_failed" {
		return "local mutation kept, external API write failed"
	} else if status == "api_not_configured" {
		return "local mutation only; external API not configured"
	} else if status == "api_confirmed" {
		return "external API confirmed"
	}
	if record.Confirmation != "" {
		return "confirmed before Jira mutation"
	}
	if record.RiskLevel == toolRiskLow {
		return "allowed as low-risk meeting action"
	}
	return "allowed by server policy"
}

func apiStatusForMutation(record boardMutationRecord) string {
	if len(record.ExternalConfirmations) == 0 {
		return ""
	}
	anyRequired := false
	allOK := true
	anyUnconfigured := false
	for _, confirmation := range record.ExternalConfirmations {
		if !confirmation.Required {
			continue
		}
		anyRequired = true
		if !confirmation.Configured {
			anyUnconfigured = true
		}
		if !confirmation.OK {
			allOK = false
		}
	}
	if !anyRequired {
		return "local_only"
	}
	if allOK {
		return "api_confirmed"
	}
	if anyUnconfigured {
		return "api_not_configured"
	}
	return "api_failed"
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
	if ledgerStore, ok := board.store.(mutationLedgerStore); ok {
		if err := ledgerStore.SaveMutationRecord(context.Background(), board.boardID, record, state); err != nil {
			log.Errorf("Failed to persist action replay event: %v", err)
		}
	}
}

func cloneBoardMutationRecords(records []boardMutationRecord) []boardMutationRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]boardMutationRecord, len(records))
	for i, record := range records {
		out[i] = cloneBoardMutationRecord(record)
	}
	return out
}

func cloneBoardMutationRecord(record boardMutationRecord) boardMutationRecord {
	record.Arguments = cloneToolArgs(record.Arguments)
	record.Result = cloneToolArgs(record.Result)
	record.CardIDs = append([]string(nil), record.CardIDs...)
	record.ExternalConfirmations = cloneExternalActionConfirmations(record.ExternalConfirmations)
	record.BeforeCards = cloneKanbanCards(record.BeforeCards)
	record.AfterCards = cloneKanbanCards(record.AfterCards)
	record.BeforeMeeting = cloneScrumMeetingStatePointerValue(record.BeforeMeeting)
	record.AfterMeeting = cloneScrumMeetingStatePointerValue(record.AfterMeeting)
	record.Transcript.Entries = append([]transcriptEntry(nil), record.Transcript.Entries...)
	return record
}
