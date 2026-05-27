package standup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
)

// Agenda is the pre-meeting briefing assembled by BuildAgenda. The voice
// agent reads each section aloud at meeting start; the React drawer renders
// them as collapsible groups so silent meeting hosts have parity.
//
// The struct is intentionally JSON-tagged so it round-trips through the
// nova_sonic userContext event without an extra translation layer.
type Agenda struct {
	TenantID             string            `json:"tenant_id"`
	BoardID              string            `json:"board_id"`
	GeneratedAt          string            `json:"generated_at"`
	Window               string            `json:"window"`
	Highlights           []AgendaHighlight `json:"highlights,omitempty"`
	Blockers             []AgendaBlocker   `json:"blockers,omitempty"`
	RunsAwaitingReview   []AgendaRun       `json:"runs_awaiting_review,omitempty"`
	OpenQuestions        []AgendaQuestion  `json:"open_questions,omitempty"`
	ProposedSpeakerOrder []string          `json:"proposed_speaker_order,omitempty"`
	Summary              string            `json:"summary,omitempty"`
}

// AgendaHighlight is a card-level callout. The agenda promotes recently-
// completed work and cards moved into In Progress within the lookback window.
type AgendaHighlight struct {
	CardID   string `json:"card_id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee,omitempty"`
	Note     string `json:"note,omitempty"`
}

// AgendaBlocker mirrors AgendaHighlight for blocked cards.
type AgendaBlocker struct {
	CardID    string `json:"card_id"`
	Title     string `json:"title"`
	Reason    string `json:"reason,omitempty"`
	Assignee  string `json:"assignee,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// AgendaRun is a non-terminal Run that completed its plan but has not yet
// been reviewed (publish step pending or PR/Jira comment unposted). The
// standup host walks through each item so the team can ack or kick a
// follow-up Run.
type AgendaRun struct {
	RunID        string `json:"run_id"`
	CardID       string `json:"card_id"`
	JiraIssueKey string `json:"jira_issue_key,omitempty"`
	Status       string `json:"status"`
	AgentProfile string `json:"agent_profile,omitempty"`
	Summary      string `json:"summary,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// AgendaQuestion is an open RunQuestion (ask-the-human pause) the team needs
// to resolve before the underlying Run can proceed.
type AgendaQuestion struct {
	QuestionID string `json:"question_id"`
	RunID      string `json:"run_id"`
	CardID     string `json:"card_id"`
	Prompt     string `json:"prompt"`
	AskedAt    string `json:"asked_at"`
}

// BoardReader is the narrow read surface the agenda builder depends on. It
// exists so internal/standup stays free of cmd/server imports; the production
// adapter wraps the kanbanBoard.
type BoardReader interface {
	Cards(ctx context.Context, tenantID, boardID string) ([]board.Card, error)
	RecentMutationCardIDs(ctx context.Context, tenantID, boardID string, since time.Time) ([]string, error)
}

// RunReader is the narrow read surface for Run + RunQuestion lookups. The
// production adapter wraps the sqliteBoardStore.
type RunReader interface {
	ListAgentRuns(ctx context.Context, tenantID, boardID string, limit int) ([]agent.Run, error)
	ListOpenRunQuestions(ctx context.Context, tenantID, boardID string) ([]agent.RunQuestion, error)
}

// AgendaBuilder bundles the reader dependencies BuildAgenda needs. Callers
// instantiate one per process and share it across meetings.
type AgendaBuilder struct {
	Board BoardReader
	Runs  RunReader
}

// DefaultWindow is the lookback applied when callers do not pass an explicit
// `since` argument. 24h covers the daily-standup cadence.
const DefaultWindow = 24 * time.Hour

// BuildAgenda assembles an Agenda from the board state, agent_runs, and
// run_questions tables. The since argument bounds the highlight + recent-
// mutation scan; pass zero to use DefaultWindow.
func (b *AgendaBuilder) BuildAgenda(ctx context.Context, tenantID, boardID string, since time.Duration) (Agenda, error) {
	if b == nil {
		return Agenda{}, errors.New("standup: AgendaBuilder is nil")
	}
	if b.Board == nil {
		return Agenda{}, errors.New("standup: BoardReader is nil")
	}
	if since <= 0 {
		since = DefaultWindow
	}
	now := time.Now().UTC()
	cutoff := now.Add(-since)

	agenda := Agenda{
		TenantID:    tenantID,
		BoardID:     boardID,
		GeneratedAt: now.Format(time.RFC3339Nano),
		Window:      since.String(),
	}

	cards, err := b.Board.Cards(ctx, tenantID, boardID)
	if err != nil {
		return Agenda{}, fmt.Errorf("read board cards: %w", err)
	}

	recentMoved := map[string]struct{}{}
	if ids, err := b.Board.RecentMutationCardIDs(ctx, tenantID, boardID, cutoff); err == nil {
		for _, id := range ids {
			recentMoved[id] = struct{}{}
		}
	}

	speakerSet := map[string]struct{}{}
	for _, c := range cards {
		switch c.Status {
		case board.StatusBlocked:
			agenda.Blockers = append(agenda.Blockers, AgendaBlocker{
				CardID:   c.ID,
				Title:    c.Title,
				Reason:   c.BlockedReason,
				Assignee: assigneeName(c),
			})
			if name := assigneeName(c); name != "" {
				speakerSet[name] = struct{}{}
			}
		case board.StatusInProgress, board.StatusDone:
			if _, ok := recentMoved[c.ID]; !ok {
				// Only highlight cards that moved in the window so the
				// agenda stays focused on the day's news, not the whole
				// backlog.
				continue
			}
			note := ""
			if c.Status == board.StatusDone {
				note = "completed since last standup"
			} else {
				note = "moved into in-progress"
			}
			agenda.Highlights = append(agenda.Highlights, AgendaHighlight{
				CardID:   c.ID,
				Title:    c.Title,
				Status:   string(c.Status),
				Assignee: assigneeName(c),
				Note:     note,
			})
			if name := assigneeName(c); name != "" {
				speakerSet[name] = struct{}{}
			}
		}
	}

	if b.Runs != nil {
		runs, err := b.Runs.ListAgentRuns(ctx, tenantID, boardID, 50)
		if err == nil {
			for _, run := range runs {
				if runNeedsReview(run) {
					agenda.RunsAwaitingReview = append(agenda.RunsAwaitingReview, AgendaRun{
						RunID:        run.RunID,
						CardID:       run.CardID,
						JiraIssueKey: run.JiraIssueKey,
						Status:       string(run.Status),
						AgentProfile: run.AgentProfile,
						Summary:      run.Summary,
						UpdatedAt:    run.UpdatedAt,
					})
				}
			}
		}
		questions, err := b.Runs.ListOpenRunQuestions(ctx, tenantID, boardID)
		if err == nil {
			for _, q := range questions {
				agenda.OpenQuestions = append(agenda.OpenQuestions, AgendaQuestion{
					QuestionID: q.ID,
					RunID:      q.RunID,
					CardID:     q.CardID,
					Prompt:     q.Prompt,
					AskedAt:    q.AskedAt,
				})
			}
		}
	}

	for name := range speakerSet {
		agenda.ProposedSpeakerOrder = append(agenda.ProposedSpeakerOrder, name)
	}
	sort.Strings(agenda.ProposedSpeakerOrder)
	agenda.Summary = summarize(agenda)
	return agenda, nil
}

func assigneeName(c board.Card) string {
	if c.Assignee == nil {
		return ""
	}
	if c.Assignee.DisplayName != "" {
		return c.Assignee.DisplayName
	}
	return c.Assignee.ID
}

func runNeedsReview(run agent.Run) bool {
	switch run.Status {
	case agent.StatusCompleted:
		// Completed runs that have not posted their PR review or have
		// publish warnings still need a human glance.
		if !run.PRReviewPosted && run.PullRequestNumber > 0 {
			return true
		}
		if len(run.PublishWarnings) > 0 {
			return true
		}
		return false
	case agent.StatusWaitingOnHuman, agent.StatusNeedsInput, agent.StatusPaused:
		return true
	}
	return false
}

func summarize(a Agenda) string {
	parts := []string{}
	if n := len(a.Highlights); n > 0 {
		parts = append(parts, fmt.Sprintf("%d highlight%s", n, pluralS(n)))
	}
	if n := len(a.Blockers); n > 0 {
		parts = append(parts, fmt.Sprintf("%d blocker%s", n, pluralS(n)))
	}
	if n := len(a.RunsAwaitingReview); n > 0 {
		parts = append(parts, fmt.Sprintf("%d run%s awaiting review", n, pluralS(n)))
	}
	if n := len(a.OpenQuestions); n > 0 {
		parts = append(parts, fmt.Sprintf("%d open question%s", n, pluralS(n)))
	}
	if len(parts) == 0 {
		return "No items today; quick check-in."
	}
	return "We have " + strings.Join(parts, ", ") + " today."
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
