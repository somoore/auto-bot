package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/somoore/auto-bot/internal/standup"
)

// novaSonicAgendaTTL is how long we let the agenda build run before
// abandoning it. Beyond this the session start proceeds without the
// briefing rather than blocking the voice agent join.
const novaSonicAgendaTTL = 4 * time.Second

// sendAgendaUserContext computes the pre-meeting agenda for the bound
// board and emits it as a USER-role text content block before the audio
// stream opens. The content carries both a structured JSON payload (so
// the model can recall specific items by ID later in the conversation)
// and a natural-language briefing line that becomes the voice opening.
func (app *novaSonicApp) sendAgendaUserContext() error {
	builder := agendaBuilderFor(app.board)
	if builder == nil || app.board == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), novaSonicAgendaTTL)
	defer cancel()
	agenda, err := builder.BuildAgenda(ctx, app.board.tenantID, app.board.boardID, 0)
	if err != nil {
		return fmt.Errorf("build agenda: %w", err)
	}
	briefing := formatAgendaBriefing(agenda)
	if briefing == "" {
		// No items today; skip the userContext so the agent uses its
		// default greeting rather than reading an empty agenda.
		return nil
	}
	payload, err := json.Marshal(agenda)
	if err != nil {
		return fmt.Errorf("marshal agenda: %w", err)
	}
	content := "PRE_MEETING_AGENDA " + string(payload) + "\n\nUse this agenda as your opening. " + briefing

	userContentID := uuid.New().String()
	if err := app.sendEvent(novaSonicEvent("contentStart", map[string]any{
		"promptName":  app.promptID,
		"contentName": userContentID,
		"type":        "TEXT",
		"interactive": false,
		"role":        "USER",
		"textInputConfiguration": map[string]any{
			"mediaType": "text/plain",
		},
	})); err != nil {
		return fmt.Errorf("send agenda contentStart: %w", err)
	}
	if err := app.sendEvent(novaSonicEvent("textInput", map[string]any{
		"promptName":  app.promptID,
		"contentName": userContentID,
		"content":     content,
	})); err != nil {
		return fmt.Errorf("send agenda textInput: %w", err)
	}
	if err := app.sendEvent(novaSonicEvent("contentEnd", map[string]any{
		"promptName":  app.promptID,
		"contentName": userContentID,
	})); err != nil {
		return fmt.Errorf("send agenda contentEnd: %w", err)
	}
	return nil
}

// formatAgendaBriefing renders the agenda as a single sentence the voice
// agent reads aloud. The output is intentionally compact so the
// listener-side latency stays under the human attention window.
func formatAgendaBriefing(a standup.Agenda) string {
	if a.Summary == "" && len(a.Highlights)+len(a.Blockers)+len(a.RunsAwaitingReview)+len(a.OpenQuestions) == 0 {
		return ""
	}
	parts := []string{a.Summary}
	if len(a.Blockers) > 0 {
		parts = append(parts, "Blockers: "+joinFirstFew(blockerTitles(a.Blockers), 3))
	}
	if len(a.OpenQuestions) > 0 {
		parts = append(parts, "Open questions: "+joinFirstFew(questionPrompts(a.OpenQuestions), 2))
	}
	if len(a.ProposedSpeakerOrder) > 0 {
		parts = append(parts, "Proposed order: "+joinFirstFew(a.ProposedSpeakerOrder, 5))
	}
	return strings.Join(parts, " ")
}

func blockerTitles(blockers []standup.AgendaBlocker) []string {
	out := make([]string, 0, len(blockers))
	for _, b := range blockers {
		title := b.Title
		if b.Reason != "" {
			title = title + " (" + b.Reason + ")"
		}
		out = append(out, title)
	}
	return out
}

func questionPrompts(questions []standup.AgendaQuestion) []string {
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		out = append(out, q.Prompt)
	}
	return out
}

func joinFirstFew(items []string, n int) string {
	if n <= 0 || len(items) <= n {
		return strings.Join(items, "; ")
	}
	return strings.Join(items[:n], "; ") + " (+" + fmt.Sprintf("%d more", len(items)-n) + ")"
}
