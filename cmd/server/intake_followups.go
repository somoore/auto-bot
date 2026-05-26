package main

import "github.com/somoore/auto-bot/internal/intake"

// runIntakeFollowups fans out the agent-driven side effects of a stored
// intake: creating Blocked-column cards for any unanchored blockers and
// posting thread comments against MentionedCards.
//
// This file is the seam for commit 4 in the async-intake landing
// sequence. Today (commit 2) it is a no-op so the HTTP endpoint can be
// exercised end-to-end without depending on ApplyToolCall behavior
// that commit 4 introduces. Commit 4 replaces the body with the actual
// fan-out via sharedBoard.ApplyToolCallWithMeta.
func runIntakeFollowups(in intake.Intake) intakeFollowupResult {
	_ = in
	return intakeFollowupResult{}
}
