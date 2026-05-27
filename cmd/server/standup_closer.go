package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/meetings"
	"github.com/somoore/auto-bot/internal/standup"
)

// closerCardCreator adapts the in-process kanbanBoard to the
// standup.CardCreator interface. It funnels create requests through
// ApplyToolCallWithMeta so guardrails, audit logging, and broadcast fan-out
// fire for every closer-driven card just like a normal create.
type closerCardCreator struct {
	board *kanbanBoard
}

func newCloserCardCreator(b *kanbanBoard) *closerCardCreator {
	return &closerCardCreator{board: b}
}

func (c *closerCardCreator) CreateCard(_ context.Context, req standup.CardRequest) (board.Card, error) {
	if c == nil || c.board == nil {
		return board.Card{}, fmt.Errorf("closerCardCreator: board is nil")
	}
	args := map[string]any{
		"title":  req.Title,
		"notes":  req.Notes,
		"tags":   req.Tags,
		"status": string(req.Status),
	}
	if req.Assignee != nil {
		if req.Assignee.ID != "" || req.Assignee.Email != "" || req.Assignee.DisplayName != "" {
			args["assignee_display_name"] = req.Assignee.DisplayName
		}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return board.Card{}, fmt.Errorf("encode create args: %w", err)
	}
	// Trusted in-process create — SkipConfirmation is set because the
	// closer is itself the human-authorized confirmation of the follow-up
	// (the meeting report's existence is the gating decision).
	result, _, err := c.board.ApplyToolCallWithMeta("create_ticket", string(raw), toolCallMeta{
		Dispatcher:       "standup-closer",
		Actor:            req.Source,
		SkipConfirmation: true,
	})
	if err != nil {
		return board.Card{}, err
	}
	cardID, _ := result["card_id"].(string)
	if cardID == "" {
		return board.Card{}, fmt.Errorf("create_ticket did not return card_id")
	}
	for _, snap := range c.board.SnapshotState().Cards {
		if snap.ID == cardID {
			return snap, nil
		}
	}
	return board.Card{ID: cardID, Title: req.Title, Status: req.Status}, nil
}

// closerArtifactSink writes the artifact into the meeting_reports table via
// the existing sqliteBoardStore implementation. When persistence is in-memory
// only the sink becomes a no-op.
type closerArtifactSink struct {
	board *kanbanBoard
}

func newCloserArtifactSink(b *kanbanBoard) *closerArtifactSink {
	return &closerArtifactSink{board: b}
}

func (s *closerArtifactSink) PersistMeetingReport(_ context.Context, _ standup.MeetingArtifact) error {
	// The canonical meetingIntelligenceReport is already persisted by
	// archiveMeetingReport before the closer runs. The closer sink no
	// longer writes a separate shadow row — the standup_closer event
	// broadcast (in runClosersOnReport) is the closer's audit trail, and
	// the persisted report has every FollowUp / Blocker the closer acted
	// on.
	return nil
}

// closerForBoard returns the production-wired standup.Closer for the
// supplied board. Returns nil when the board has no persistent backing —
// the in-memory test runs leave the closer disabled.
func closerForBoard(b *kanbanBoard) *standup.Closer {
	if b == nil {
		return nil
	}
	var coord agent.RunCoordinator
	if agentOrchestrator != nil {
		coord = agentOrchestrator
	}
	return &standup.Closer{
		Cards: newCloserCardCreator(b),
		Runs:  coord,
		Sink:  newCloserArtifactSink(b),
	}
}

// runClosersOnReport is invoked from archiveMeetingReport after the canonical
// report has been persisted. It is non-fatal: failures inside the closer log
// but do not affect the report archive.
func runClosersOnReport(report meetingIntelligenceReport) {
	board := sharedBoard
	if board == nil {
		return
	}
	c := closerForBoard(board)
	if c == nil {
		return
	}
	artifact := standup.MeetingArtifact{
		TenantID:           report.TenantID,
		BoardID:            report.BoardID,
		MeetingID:          report.MeetingID,
		EndedAt:            report.EndedAt,
		Summary:            report.Summary,
		HostIdentity:       report.HostIdentity,
		FollowUps:          append([]meetings.FollowUp(nil), report.FollowUps...),
		UnresolvedBlockers: append([]meetings.Blocker(nil), report.UnresolvedBlockers...),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := c.Close(ctx, artifact)
	if err != nil {
		log.Errorf("standup closer failed: %v", err)
		return
	}
	log.Infof("standup closer: meeting=%s cards_created=%d runs_started=%d errors=%d",
		report.MeetingID, len(result.CardsCreated), len(result.RunsStarted), len(result.Errors))
	for _, e := range result.Errors {
		log.Warnf("standup closer error: %s", e)
	}
	broadcastKanbanEventForBoard(board.tenantID, board.boardID, "standup_closer", result)
}
