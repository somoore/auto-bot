package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/somoore/auto-bot/internal/intake"
)

// runIntakeFollowups fans out the agent-driven side effects of a stored
// intake:
//
//   - Every blocker that does NOT already reference an existing card
//     creates a new Blocked-column card with the intake's submitter as
//     the assignee. This is the "blocker becomes a card" promise from
//     Daria's persona walkthrough.
//   - Every MentionedCards reference (whether the card was just created
//     or already existed) gets a thread comment carrying the intake
//     snippet, attributed to the submitter.
//
// All board mutations route through sharedBoard.ApplyToolCallWithMeta
// using a dedicated dispatcher ("intake") so ActionLedger,
// risk-classification, and confirmation gates fire exactly the same
// way they do for voice-driven and MCP-driven calls. The intake
// handler authenticates the caller before this fires, so the SkipConfir-
// mation gate is taken for assign_ticket only — keeping the human in
// the loop for create_ticket/add_comment would create UI friction that
// the persona doc explicitly calls out as the wrong default for the
// async flow.
//
// Returns a summary the HTTP layer echoes back to the React form so the
// user sees what changed without a board refresh.
func runIntakeFollowups(in intake.Intake) intakeFollowupResult {
	result := intakeFollowupResult{}
	if sharedBoard == nil {
		return result
	}

	meta := toolCallMeta{
		Dispatcher: "intake",
		Actor:      in.Submitter,
	}

	// Track every card we touch so MentionedCards comments fire against
	// just-created cards too. Use the canonical case so the membership
	// test is stable.
	touched := map[string]struct{}{}
	for _, ref := range in.MentionedCards {
		touched[ref] = struct{}{}
	}
	// Blockers without a CardID become new Blocked cards. Blockers WITH
	// a CardID get a comment on the referenced card instead (handled in
	// the MentionedCards loop below since CardID is auto-promoted into
	// MentionedCards by intake.Normalize when the parser detected one).
	for _, blocker := range in.Blockers {
		if strings.TrimSpace(blocker.CardID) != "" {
			continue
		}
		cardID, card, err := createBlockerCard(in, blocker, meta)
		if err != nil {
			log.Errorf("intake/followups: create blocker card failed: %v", err)
			continue
		}
		result.CreatedCards = append(result.CreatedCards, card)

		// Self-assign the submitter so the new card shows up on the
		// submitter's queue immediately. Skip-confirmation is justified
		// because the request is authenticated AS the submitter and the
		// blocker text came from them; queueing a confirmation back to
		// the same user would be ceremony with no consent value. The
		// ApplyToolCall path still records the assignment in
		// ActionLedger.
		if assignErr := assignCardToSubmitter(cardID, in.Submitter, meta); assignErr != nil {
			log.Errorf("intake/followups: assign blocker card to submitter failed: %v", assignErr)
		}
	}

	// Post a thread comment for every MentionedCards entry that exists
	// on the current board. Unknown card IDs are skipped — the parser
	// is permissive (Jira-style PROJ-42 references can appear in text
	// even when the project isn't synced to this board) and we don't
	// want a typo to fail the whole intake.
	for ref := range touched {
		card, ok := lookupCardFromBoard(ref)
		if !ok {
			continue
		}
		comment, err := postIntakeComment(card.ID, in, meta)
		if err != nil {
			log.Errorf("intake/followups: comment on %s failed: %v", card.ID, err)
			continue
		}
		result.PostedComments = append(result.PostedComments, comment)
	}

	return result
}

// createBlockerCard dispatches a create_ticket call that lands a new
// Blocked-column card carrying the blocker text and a back-pointer to
// the intake submitter.
func createBlockerCard(in intake.Intake, blocker intake.BlockerItem, meta toolCallMeta) (string, kanbanCard, error) {
	notes := fmt.Sprintf("Async intake from %s — %s", in.Submitter, blocker.Text)
	if strings.TrimSpace(blocker.Due) != "" {
		notes += " (due " + blocker.Due + ")"
	}
	args := map[string]any{
		"title":  truncateString(blocker.Text, 120),
		"notes":  notes,
		"status": string(kanbanStatusBlocked),
		"tags":   []string{"intake", "blocker"},
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", kanbanCard{}, fmt.Errorf("marshal create_ticket args: %w", err)
	}
	result, _, err := sharedBoard.ApplyToolCallWithMeta("create_ticket", string(raw), meta)
	if err != nil {
		return "", kanbanCard{}, err
	}
	card, _ := result["card"].(kanbanCard)
	cardID, _ := result["card_id"].(string)
	if cardID == "" {
		cardID = card.ID
	}
	return cardID, card, nil
}

// assignCardToSubmitter posts an assign_ticket call carrying the
// submitter as both account_id and display_name. SkipConfirmation is
// set so the confirmation queue does not stall the followup — the
// intake handler already authenticated as the submitter and is
// self-assigning a card the same caller just created.
func assignCardToSubmitter(cardID string, submitter string, meta toolCallMeta) error {
	args := map[string]any{
		"card_id":      cardID,
		"account_id":   submitter,
		"display_name": submitter,
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal assign_ticket args: %w", err)
	}
	skipMeta := meta
	skipMeta.SkipConfirmation = true
	_, _, err = sharedBoard.ApplyToolCallWithMeta("assign_ticket", string(raw), skipMeta)
	return err
}

// postIntakeComment dispatches an add_comment with a snippet that
// reflects what the submitter said about the referenced card.
func postIntakeComment(cardID string, in intake.Intake, meta toolCallMeta) (postedIntakeComment, error) {
	body := buildIntakeCommentBody(in)
	args := map[string]any{
		"card_id": cardID,
		"comment": body,
		"author":  in.Submitter,
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return postedIntakeComment{}, fmt.Errorf("marshal add_comment args: %w", err)
	}
	if _, _, err := sharedBoard.ApplyToolCallWithMeta("add_comment", string(raw), meta); err != nil {
		return postedIntakeComment{}, err
	}
	return postedIntakeComment{
		CardID: cardID,
		Body:   body,
		Author: in.Submitter,
	}, nil
}

// buildIntakeCommentBody turns the intake into a short snippet suitable
// for posting on a referenced card. Includes only the parts that
// actually changed; an empty Yesterday/Today/Blockers is omitted.
func buildIntakeCommentBody(in intake.Intake) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Async intake from %s:", in.Submitter))
	if strings.TrimSpace(in.Yesterday) != "" {
		parts = append(parts, "Yesterday: "+in.Yesterday)
	}
	if strings.TrimSpace(in.Today) != "" {
		parts = append(parts, "Today: "+in.Today)
	}
	if len(in.Blockers) > 0 {
		blockerLines := make([]string, 0, len(in.Blockers))
		for _, b := range in.Blockers {
			blockerLines = append(blockerLines, "- "+b.Text)
		}
		parts = append(parts, "Blockers:\n"+strings.Join(blockerLines, "\n"))
	}
	return strings.Join(parts, "\n")
}
