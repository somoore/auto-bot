package standup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/meetings"
)

// Closer wires the post-meeting actions together. Close() iterates the
// FollowUps + UnresolvedBlockers on a freshly-ended meeting report, creates
// matching cards for any entry that does not already point at one, and kicks
// a RunCoordinator.Start for the subset whose assignees are agents.
type Closer struct {
	Cards CardCreator
	Runs  agent.RunCoordinator
	Sink  ArtifactSink
}

// CardCreator is the narrow write surface the closer depends on for
// materializing follow-up cards. cmd/server adapts its kanbanBoard to this
// interface; tests substitute an in-memory stub.
type CardCreator interface {
	CreateCard(ctx context.Context, req CardRequest) (board.Card, error)
}

// CardRequest is the shape passed to CardCreator.CreateCard. The closer
// fills in title, notes, tags, status, and the optional assignee Actor;
// implementations decide how to map those onto their native create
// pipeline. Title is required.
type CardRequest struct {
	TenantID string
	BoardID  string
	Title    string
	Notes    string
	Tags     []string
	Status   board.Status
	Assignee *board.Actor
	// Source labels the closer-driven origin so the receiving system can
	// distinguish auto-generated cards from human ones.
	Source string
}

// ArtifactSink persists the meeting report as a typed artifact. The
// production adapter wraps meetingReportStore; tests provide a recorder.
type ArtifactSink interface {
	PersistMeetingReport(ctx context.Context, report MeetingArtifact) error
}

// MeetingArtifact is the lossy projection of the cmd/server
// meetingIntelligenceReport that crosses the import boundary. Only the
// fields the closer reads / persists live here; the closer does not need
// the full report shape to do its job.
type MeetingArtifact struct {
	TenantID           string              `json:"tenant_id"`
	BoardID            string              `json:"board_id"`
	MeetingID          string              `json:"meeting_id"`
	EndedAt            string              `json:"ended_at,omitempty"`
	Summary            string              `json:"summary,omitempty"`
	FollowUps          []meetings.FollowUp `json:"follow_ups,omitempty"`
	UnresolvedBlockers []meetings.Blocker  `json:"unresolved_blockers,omitempty"`
	HostIdentity       string              `json:"host_identity,omitempty"`
}

// CloseResult captures what the closer did on a single Close invocation so
// callers (HTTP handlers, MCP tools, voice agent) can echo the outcome.
type CloseResult struct {
	CardsCreated []string `json:"cards_created"`
	RunsStarted  []string `json:"runs_started"`
	Errors       []string `json:"errors,omitempty"`
}

// Close walks the supplied artifact and:
//  1. Creates a Backlog card for every FollowUp + UnresolvedBlocker that
//     does not already point at a card.
//  2. For follow-ups whose assignee is an agent Actor, calls Runs.Start so
//     the agent picks up the work immediately.
//  3. Persists the report via Sink.PersistMeetingReport.
//
// Errors during one entry are recorded but do not abort the loop; the
// CloseResult.Errors slice captures every failure so the caller can decide
// how loudly to surface them.
func (c *Closer) Close(ctx context.Context, artifact MeetingArtifact) (CloseResult, error) {
	if c == nil {
		return CloseResult{}, errors.New("standup: Closer is nil")
	}
	if c.Cards == nil {
		return CloseResult{}, errors.New("standup: Closer.Cards is nil")
	}
	result := CloseResult{}
	for _, followUp := range artifact.FollowUps {
		if followUp.CardID != "" {
			continue
		}
		title := strings.TrimSpace(followUp.Text)
		if title == "" {
			continue
		}
		assignee := actorForOwner(followUp.Owner)
		req := CardRequest{
			TenantID: artifact.TenantID,
			BoardID:  artifact.BoardID,
			Title:    truncate(title, 120),
			Notes:    "Auto-created from meeting " + artifact.MeetingID + ":\n\n" + title,
			Tags:     []string{"standup-follow-up"},
			Status:   board.StatusBacklog,
			Assignee: assignee,
			Source:   "standup-closer",
		}
		card, err := c.Cards.CreateCard(ctx, req)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("create follow-up card: %v", err))
			continue
		}
		result.CardsCreated = append(result.CardsCreated, card.ID)
		if assignee != nil && assignee.Kind == board.ActorKindAgent && c.Runs != nil {
			runReq := agent.RunRequest{
				TenantID:     artifact.TenantID,
				BoardID:      artifact.BoardID,
				CardID:       card.ID,
				Objective:    "Resolve standup follow-up: " + req.Title,
				RequestedBy:  artifact.HostIdentity,
				AgentProfile: assignee.AgentProfile,
				RequestType:  "auto",
			}
			run, err := c.Runs.Start(ctx, runReq)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("start run on %s: %v", card.ID, err))
				continue
			}
			result.RunsStarted = append(result.RunsStarted, run.RunID)
		}
	}
	for _, blocker := range artifact.UnresolvedBlockers {
		if blocker.CardID != "" {
			continue
		}
		title := strings.TrimSpace(blocker.Text)
		if title == "" {
			continue
		}
		req := CardRequest{
			TenantID: artifact.TenantID,
			BoardID:  artifact.BoardID,
			Title:    "Blocker: " + truncate(title, 110),
			Notes:    "Auto-created from meeting " + artifact.MeetingID + ":\n\n" + title,
			Tags:     []string{"standup-blocker"},
			Status:   board.StatusBlocked,
			Assignee: actorForOwner(blocker.Owner),
			Source:   "standup-closer",
		}
		card, err := c.Cards.CreateCard(ctx, req)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("create blocker card: %v", err))
			continue
		}
		result.CardsCreated = append(result.CardsCreated, card.ID)
	}
	if c.Sink != nil {
		if err := c.Sink.PersistMeetingReport(ctx, artifact); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("persist meeting report: %v", err))
		}
	}
	return result, nil
}

func actorForOwner(owner string) *board.Actor {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil
	}
	if strings.HasPrefix(owner, "agent:") {
		profile := strings.TrimPrefix(owner, "agent:")
		return &board.Actor{
			Kind:         board.ActorKindAgent,
			ID:           "agent:" + profile,
			DisplayName:  profile,
			AgentProfile: profile,
		}
	}
	return &board.Actor{
		Kind:        board.ActorKindHuman,
		ID:          owner,
		DisplayName: owner,
	}
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
