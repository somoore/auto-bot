package meetings

import (
	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/core"
)

// RiskLevel is the policy classification carried on tool calls, pending
// confirmations, and recorded mutations. It re-exports core.RiskLevel so the
// meeting layer, governance, and connectors share one value space.
type RiskLevel = core.RiskLevel

// Risk level constants re-exported from internal/core so callers in
// cmd/server and downstream packages can refer to a single canonical set of
// "low" / "medium" / "high" values.
const (
	RiskLow    = core.RiskLow
	RiskMedium = core.RiskMedium
	RiskHigh   = core.RiskHigh
)

// Mode classifies the kind of meeting (standup, planning, retro, etc.) so
// the agent can tailor briefings, agendas, and follow-up generation.
type Mode string

// Canonical meeting modes. The string values are persisted in board snapshots
// and broadcast to browser clients, so any new mode must keep the same value
// shape ("snake_case lower").
const (
	ModeGeneral   Mode = "general"
	ModeStandup   Mode = "daily_standup"
	ModeOneOnOne  Mode = "one_on_one"
	ModePlanning  Mode = "sprint_planning"
	ModeGrooming  Mode = "backlog_grooming"
	ModeReview    Mode = "sprint_review"
	ModeRetro     Mode = "retrospective"
	ModeOpenEnded Mode = "open_ended"
)

// Participant is one person in an active scrum meeting. HasSpoken and
// LastUpdate are tracked so the agent can route the conversation and remind
// silent attendees.
type Participant struct {
	ParticipantID string `json:"participantId,omitempty"`
	Name          string `json:"name"`
	Role          string `json:"role,omitempty"`
	HasSpoken     bool   `json:"hasSpoken"`
	LastUpdate    string `json:"lastUpdate,omitempty"`
}

// ParticipantUpdate is a single status update a participant gave during a
// meeting (yesterday/today/blockers shape). It is the persisted record used
// for post-meeting reports and Jira write-back.
type ParticipantUpdate struct {
	ParticipantID string       `json:"participantId,omitempty"`
	Participant   string       `json:"participant"`
	CardID        string       `json:"cardId,omitempty"`
	Summary       string       `json:"summary"`
	Completed     []string     `json:"completed,omitempty"`
	Planned       []string     `json:"planned,omitempty"`
	Status        board.Status `json:"status,omitempty"`
	Blocker       string       `json:"blocker,omitempty"`
	Risks         []string     `json:"risks,omitempty"`
	ETA           string       `json:"eta,omitempty"`
	FollowUp      string       `json:"followUp,omitempty"`
	CreatedAt     string       `json:"createdAt"`
}

// FollowUp is an open action item generated during a meeting. Status values
// today are "open" / "closed"; new lifecycles should extend rather than
// reuse those tokens.
type FollowUp struct {
	ID        string `json:"id"`
	Owner     string `json:"owner,omitempty"`
	Text      string `json:"text"`
	CardID    string `json:"cardId,omitempty"`
	DueDate   string `json:"dueDate,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// Blocker is an unresolved impediment surfaced during a meeting. ResolvedAt
// is populated when the host marks it cleared.
type Blocker struct {
	ID         string `json:"id"`
	Owner      string `json:"owner,omitempty"`
	Text       string `json:"text"`
	CardID     string `json:"cardId,omitempty"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
}

// Ownership records who owns a responsibility or card and when the agent
// last confirmed that ownership during a meeting.
type Ownership struct {
	Owner          string `json:"owner"`
	CardID         string `json:"cardId,omitempty"`
	Responsibility string `json:"responsibility"`
	UpdatedAt      string `json:"updatedAt"`
}

// Briefing is the standup briefing summary cached on a meeting state. It
// surfaces the headline metrics, blockers, and recommended questions used to
// drive the next conversation.
type Briefing struct {
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

// State is the in-memory record of an active or recently-finished scrum
// meeting. It is broadcast to clients as part of the board snapshot and
// persisted alongside the board so a server restart keeps the agenda,
// decisions, action items, and follow-ups intact.
type State struct {
	MeetingID          string              `json:"meetingId,omitempty"`
	Active             bool                `json:"active"`
	Mode               Mode                `json:"mode,omitempty"`
	Goal               string              `json:"goal,omitempty"`
	SprintID           string              `json:"sprintId,omitempty"`
	SprintName         string              `json:"sprintName,omitempty"`
	Agenda             []string            `json:"agenda,omitempty"`
	StartedAt          string              `json:"startedAt,omitempty"`
	EndedAt            string              `json:"endedAt,omitempty"`
	CurrentSpeaker     string              `json:"currentSpeaker,omitempty"`
	Participants       []Participant       `json:"participants,omitempty"`
	Updates            []ParticipantUpdate `json:"updates,omitempty"`
	Decisions          []string            `json:"decisions,omitempty"`
	Risks              []string            `json:"risks,omitempty"`
	ActionItems        []string            `json:"actionItems,omitempty"`
	ParkingLot         []string            `json:"parkingLot,omitempty"`
	FollowUps          []FollowUp          `json:"followUps,omitempty"`
	UnresolvedBlockers []Blocker           `json:"unresolvedBlockers,omitempty"`
	Ownership          []Ownership         `json:"ownership,omitempty"`
	LastBriefing       *Briefing           `json:"lastBriefing,omitempty"`
}

// TranscriptEntry is one sanitized line of meeting transcript retained for
// audit and intelligence reports. CreatedAt is RFC3339Nano UTC.
type TranscriptEntry struct {
	Role           string `json:"role"`
	Speaker        string `json:"speaker,omitempty"`
	Text           string `json:"text"`
	OriginalText   string `json:"original_text,omitempty"`
	TranslatedText string `json:"translated_text,omitempty"`
	Language       string `json:"language,omitempty"`
	InputMode      string `json:"input_mode,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// TranscriptEvidence is the recent-transcript snapshot attached to a board
// mutation as audit evidence for that action.
type TranscriptEvidence struct {
	Entries []TranscriptEntry `json:"entries,omitempty"`
	Summary string            `json:"summary,omitempty"`
}

// ExternalConfirmation is the receipt from an external system (Jira, GitHub,
// Slack) that an action was applied (or explicitly was not applied). It is
// recorded alongside the in-memory mutation so the host can replay why local
// state may diverge from the upstream tool.
type ExternalConfirmation struct {
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

// PendingConfirmation is a medium/high-risk tool call that is waiting for an
// explicit host "yes" before mutating board or external state. The arguments
// are cloned so later edits in cmd/server cannot mutate the stored copy.
type PendingConfirmation struct {
	ConfirmationID string         `json:"confirmationId"`
	ToolName       string         `json:"toolName"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	RiskLevel      RiskLevel      `json:"riskLevel"`
	Prompt         string         `json:"prompt"`
	Source         string         `json:"source,omitempty"`
	Actor          string         `json:"actor,omitempty"`
	CallID         string         `json:"callId,omitempty"`
	CreatedAt      string         `json:"createdAt"`
	ExpiresAt      string         `json:"expiresAt"`
}

// PendingConfirmationView is the client-safe projection of a pending
// confirmation. It omits the raw tool arguments and adds the confidence and
// guardrail explanations rendered in the meeting drawer.
type PendingConfirmationView struct {
	ConfirmationID    string    `json:"confirmationId"`
	ToolName          string    `json:"toolName"`
	RiskLevel         RiskLevel `json:"riskLevel"`
	Prompt            string    `json:"prompt"`
	Source            string    `json:"source,omitempty"`
	Actor             string    `json:"actor,omitempty"`
	Confidence        float64   `json:"confidence,omitempty"`
	ConfidenceReasons []string  `json:"confidenceReasons,omitempty"`
	MatchedCardID     string    `json:"matchedCardId,omitempty"`
	GuardrailDecision string    `json:"guardrailDecision,omitempty"`
	CreatedAt         string    `json:"createdAt"`
	ExpiresAt         string    `json:"expiresAt"`
}

// BoardMutationView is the client-safe audit summary for one board or Jira
// mutation. The full mutation record (with before/after card snapshots) lives
// in cmd/server; this view is what the meeting drawer and audit log render.
type BoardMutationView struct {
	EventID               string                 `json:"eventId"`
	OccurredAt            string                 `json:"occurredAt"`
	Source                string                 `json:"source"`
	Actor                 string                 `json:"actor,omitempty"`
	ToolName              string                 `json:"toolName"`
	RiskLevel             RiskLevel              `json:"riskLevel"`
	Confirmation          string                 `json:"confirmationId,omitempty"`
	CardIDs               []string               `json:"cardIds,omitempty"`
	Summary               string                 `json:"summary"`
	Confidence            float64                `json:"confidence,omitempty"`
	ConfidenceReasons     []string               `json:"confidenceReasons,omitempty"`
	MatchedCardID         string                 `json:"matchedCardId,omitempty"`
	GuardrailDecision     string                 `json:"guardrailDecision,omitempty"`
	ExternalConfirmations []ExternalConfirmation `json:"externalConfirmations,omitempty"`
	APIStatus             string                 `json:"apiStatus,omitempty"`
	Transcript            TranscriptEvidence     `json:"transcript,omitempty"`
	Sequence              int64                  `json:"sequenceNumber"`
	Reverted              bool                   `json:"reverted,omitempty"`
	UndoOf                string                 `json:"undoOf,omitempty"`
}
