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
// using a dedicated dispatcher ("intake") so the audit log
// (action_replay_events), risk-classification, and confirmation gates
// fire exactly the same way they do for voice-driven and MCP-driven
// calls.
//
// SecArch-002 alignment: assign_ticket is risk-medium and normally
// queues a confirmation. We skip that queue ONLY when the caller is
// self-assigning (the authenticated identity matches the intake's
// submitter). EM-files-on-behalf intakes go through the normal
// confirmation queue so the assignee gets the standard 6-second window
// to reject. callerIdentity is the request-auth identity; an empty
// callerIdentity (test paths) is treated as untrusted and queues the
// confirmation.
//
// Returns a summary the HTTP layer echoes back to the React form so the
// user sees what changed without a board refresh.
func runIntakeFollowups(in intake.Intake, callerIdentity string) intakeFollowupResult {
	result := intakeFollowupResult{}
	if sharedBoard == nil {
		return result
	}

	meta := toolCallMeta{
		Dispatcher: "intake",
		Actor:      in.Submitter,
	}
	selfAssigning := callerIdentity != "" &&
		strings.EqualFold(strings.TrimSpace(callerIdentity), strings.TrimSpace(in.Submitter))

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

		// Assign the submitter so the new card shows up on their queue
		// immediately. Skip-confirmation only when the authenticated
		// caller IS the submitter; cross-user EM-files-on-behalf goes
		// through the normal confirmation queue (SecArch-002). The
		// ApplyToolCall path records the assignment in the audit log
		// either way.
		assignMeta := meta
		assignMeta.SkipConfirmation = selfAssigning
		if assignErr := assignCardToSubmitter(cardID, in.Submitter, assignMeta); assignErr != nil {
			log.Errorf("intake/followups: assign blocker card to submitter failed: %v", assignErr)
		}

		// Re-snapshot the card so the response reflects the post-
		// assignment state — the React confirmation list reads
		// CreatedCards.Assignee to render attribution.
		if refreshed, ok := lookupCardFromBoard(cardID); ok {
			card = refreshed
		}
		result.CreatedCards = append(result.CreatedCards, card)
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
// submitter as both account_id and display_name. The caller decides
// whether to set meta.SkipConfirmation: self-assignment skips the
// queue, cross-user assignment goes through the standard medium-risk
// confirmation gate (SecArch-002).
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
	_, _, err = sharedBoard.ApplyToolCallWithMeta("assign_ticket", string(raw), meta)
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
