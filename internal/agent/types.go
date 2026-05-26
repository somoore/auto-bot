package agent

// DefaultPMModel and DefaultReviewModel are the Bedrock model IDs used by the
// orchestrator for classification (PM) and review passes when a run does not
// explicitly override them.
const (
	DefaultPMModel     = "us.anthropic.claude-haiku-4-5-20251001-v1:0"
	DefaultReviewModel = "us.anthropic.claude-sonnet-4-6"
)

// RunStatus is the lifecycle state of an agent Run.
type RunStatus string

// Canonical Run lifecycle states. External progress events map onto these so
// browser clients, Jira comments, and meeting reports share a vocabulary.
//
// StatusNeedsInput is the legacy "the agent could not proceed because some
// upstream context (typically a PR diff or repository handle) was missing"
// signal. The run is effectively terminated until a human retries it with the
// missing context.
//
// StatusWaitingOnHuman is the newer ask-the-human pause: the agent has
// recorded a RunQuestion on the card thread and is sleeping until either an
// answer arrives or the TTL expires. The run is not terminated; resumption is
// expected on answer.
const (
	StatusQueued          RunStatus = "queued"
	StatusClassifying     RunStatus = "classifying"
	StatusFetchingContext RunStatus = "fetching_context"
	StatusReviewing       RunStatus = "reviewing"
	StatusPublishing      RunStatus = "publishing"
	StatusRetrying        RunStatus = "retrying"
	StatusNeedsInput      RunStatus = "needs_input"
	StatusWaitingOnHuman  RunStatus = "waiting_on_human"
	StatusCompleted       RunStatus = "completed"
	StatusFailed          RunStatus = "failed"
	StatusUnsupported     RunStatus = "unsupported"
	StatusCancelled       RunStatus = "cancelled"
	StatusTakenOver       RunStatus = "taken_over"
)

// Run is the persisted internal state for an autonomous Bedrock-backed
// Jira/GitHub run.
type Run struct {
	RunID              string              `json:"run_id"`
	TenantID           string              `json:"tenant_id,omitempty"`
	BoardID            string              `json:"board_id"`
	CardID             string              `json:"card_id"`
	JiraIssueKey       string              `json:"jira_issue_key,omitempty"`
	CardTitle          string              `json:"card_title,omitempty"`
	Objective          string              `json:"objective"`
	RequestedBy        string              `json:"requested_by,omitempty"`
	RetryOf            string              `json:"retry_of,omitempty"`
	AgentProfile       string              `json:"agent_profile"`
	RequestType        string              `json:"request_type"`
	Specialist         string              `json:"specialist"`
	Status             RunStatus           `json:"status"`
	CurrentStep        string              `json:"current_step,omitempty"`
	Repo               string              `json:"repo,omitempty"`
	Branch             string              `json:"branch,omitempty"`
	PullRequestNumber  int                 `json:"pull_request_number,omitempty"`
	PullRequestURL     string              `json:"pull_request_url,omitempty"`
	PMModel            string              `json:"pm_model,omitempty"`
	ReviewModel        string              `json:"review_model,omitempty"`
	Classification     Classification      `json:"classification,omitempty"`
	ReviewLens         string              `json:"review_lens,omitempty"`
	Findings           []CodeReviewFinding `json:"findings,omitempty"`
	Summary            string              `json:"summary,omitempty"`
	PublishWarnings    []string            `json:"publish_warnings,omitempty"`
	CostBudgetCents    int                 `json:"cost_budget_cents,omitempty"`
	EstimatedCostCents int                 `json:"estimated_cost_cents,omitempty"`
	ModelCalls         int                 `json:"model_calls,omitempty"`
	JiraCommentPosted  bool                `json:"jira_comment_posted"`
	PRReviewPosted     bool                `json:"pr_review_posted"`
	Error              string              `json:"error,omitempty"`
	Checkpoints        []Checkpoint        `json:"checkpoints,omitempty"`
	// Plan is the ordered list of planning steps the agent intends to execute.
	// Empty for legacy runs that pre-date the planning surface.
	Plan []PlanStep `json:"plan,omitempty"`
	// Cost is the rolling cost accounting for this Run, aggregated by model.
	Cost CostBreakdown `json:"cost,omitempty"`
	// WaitingOn is non-nil when the Run is paused on a RunQuestion. The full
	// question record lives in the run_questions table; this is the lightweight
	// pointer surfaced inside Run JSON so the drawer can render "waiting on..."
	// without a second lookup.
	WaitingOn *RunQuestionRef `json:"waiting_on,omitempty"`
	// SequenceNumberStart / SequenceNumberEnd record the board sequence numbers
	// observed at the beginning and end of the Run. They let post-run replay
	// reconstruct the exact board state the agent reasoned over.
	SequenceNumberStart int64  `json:"sequence_number_start,omitempty"`
	SequenceNumberEnd   int64  `json:"sequence_number_end,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	StartedAt           string `json:"started_at,omitempty"`
	CompletedAt         string `json:"completed_at,omitempty"`
}

// PlanStep is one entry in a Run's plan. Index is 1-based and stable across
// pauses; StartedAt / CompletedAt are RFC3339Nano timestamps recorded as the
// orchestrator transitions through the plan.
type PlanStep struct {
	Index       int    `json:"index"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	EstimatedMs int64  `json:"estimated_ms,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	// Status values: pending | running | done | skipped | paused.
	Status  string `json:"status"`
	Outcome string `json:"outcome,omitempty"`
}

// CostBreakdown is the rolling cost accounting for a Run. Cents is the total;
// ByModel breaks it down per Bedrock/OpenAI model ID for product-proof metrics
// and budget enforcement.
type CostBreakdown struct {
	Cents        int            `json:"cents"`
	ByModel      map[string]int `json:"by_model,omitempty"`
	AudioSeconds int            `json:"audio_seconds,omitempty"`
	TokensIn     int            `json:"tokens_in,omitempty"`
	TokensOut    int            `json:"tokens_out,omitempty"`
	UpdatedAt    string         `json:"updated_at,omitempty"`
}

// RunQuestion is a single ask-the-human pause emitted by an agent Run. It is
// persisted in the run_questions SQLite table so that operators can answer it
// via the UI, voice ("hey, that question on EMAL-12 - the answer is ..."), or
// MCP. The Run itself stores only a RunQuestionRef pointer.
//
// TODO(sprint-1.3): swap ID for a ULID once the helper exists. For now callers
// supply an opaque string ID; the orchestrator wires up generation.
type RunQuestion struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	BoardID     string   `json:"board_id"`
	RunID       string   `json:"run_id"`
	CardID      string   `json:"card_id"`
	StepIndex   int      `json:"step_index"`
	Prompt      string   `json:"prompt"`
	Reasoning   string   `json:"reasoning,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
	AskedAt     string   `json:"asked_at"`
	// TTLSeconds defaults to 14400 (4h) when unset. Past this window the
	// question is auto-expired by ExpireRunQuestions.
	TTLSeconds int    `json:"ttl_seconds"`
	AnsweredAt string `json:"answered_at,omitempty"`
	Answer     string `json:"answer,omitempty"`
	AnsweredBy string `json:"answered_by,omitempty"`
	// AnsweredVia values: ui | voice | mcp.
	AnsweredVia string `json:"answered_via,omitempty"`
	// Status values: open | answered | expired | cancelled.
	Status string `json:"status"`
}

// RunQuestionRef is the lightweight pointer embedded in Run.WaitingOn. It
// carries just enough information for the drawer/Jira comment to show "this
// run is waiting on question Q about X" without loading the full question.
type RunQuestionRef struct {
	QuestionID string `json:"question_id"`
	Prompt     string `json:"prompt"`
	AskedAt    string `json:"asked_at"`
}

// Classification is the PM-model verdict on what kind of work a Run is doing
// and which specialist lens should review it.
type Classification struct {
	RequestType string   `json:"request_type,omitempty"`
	Specialist  string   `json:"specialist,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
	Needs       []string `json:"needs,omitempty"`
}

// CodeReviewFinding is the normalized finding shape used for Jira comments,
// optional GitHub PR reviews, and meeting intelligence.
type CodeReviewFinding struct {
	Severity        string   `json:"severity"`
	Category        string   `json:"category,omitempty"`
	CWE             string   `json:"cwe,omitempty"`
	Title           string   `json:"title"`
	File            string   `json:"file,omitempty"`
	Line            int      `json:"line,omitempty"`
	Body            string   `json:"body"`
	Evidence        string   `json:"evidence,omitempty"`
	Impact          string   `json:"impact,omitempty"`
	ExploitScenario string   `json:"exploit_scenario,omitempty"`
	SuggestedFix    string   `json:"suggested_fix,omitempty"`
	Tests           []string `json:"tests,omitempty"`
	Confidence      float64  `json:"confidence,omitempty"`
}

// Checkpoint is a single timeline entry on a Run, recorded whenever the
// orchestrator transitions status or surfaces a milestone message.
type Checkpoint struct {
	At      string    `json:"at"`
	Status  RunStatus `json:"status"`
	Step    string    `json:"step,omitempty"`
	Message string    `json:"message"`
}

// RunView is the client-safe run timeline shape shown in the live drawer and
// post-meeting intelligence report.
type RunView struct {
	RunID               string              `json:"run_id"`
	CardID              string              `json:"card_id"`
	JiraIssueKey        string              `json:"jira_issue_key,omitempty"`
	CardTitle           string              `json:"card_title,omitempty"`
	Objective           string              `json:"objective,omitempty"`
	RequestedBy         string              `json:"requested_by,omitempty"`
	RetryOf             string              `json:"retry_of,omitempty"`
	AgentProfile        string              `json:"agent_profile,omitempty"`
	RequestType         string              `json:"request_type,omitempty"`
	Specialist          string              `json:"specialist,omitempty"`
	Status              RunStatus           `json:"status"`
	CurrentStep         string              `json:"current_step,omitempty"`
	Repo                string              `json:"repo,omitempty"`
	Branch              string              `json:"branch,omitempty"`
	PullRequestNumber   int                 `json:"pull_request_number,omitempty"`
	PullRequestURL      string              `json:"pull_request_url,omitempty"`
	PMModel             string              `json:"pm_model,omitempty"`
	ReviewModel         string              `json:"review_model,omitempty"`
	Classification      Classification      `json:"classification,omitempty"`
	ReviewLens          string              `json:"review_lens,omitempty"`
	FindingCount        int                 `json:"finding_count"`
	Findings            []CodeReviewFinding `json:"findings,omitempty"`
	Summary             string              `json:"summary,omitempty"`
	PublishWarnings     []string            `json:"publish_warnings,omitempty"`
	CostBudgetCents     int                 `json:"cost_budget_cents,omitempty"`
	EstimatedCostCents  int                 `json:"estimated_cost_cents,omitempty"`
	ModelCalls          int                 `json:"model_calls,omitempty"`
	JiraCommentPosted   bool                `json:"jira_comment_posted"`
	PRReviewPosted      bool                `json:"pr_review_posted"`
	Error               string              `json:"error,omitempty"`
	Checkpoints         []Checkpoint        `json:"checkpoints,omitempty"`
	Plan                []PlanStep          `json:"plan,omitempty"`
	Cost                CostBreakdown       `json:"cost,omitempty"`
	WaitingOn           *RunQuestionRef     `json:"waiting_on,omitempty"`
	SequenceNumberStart int64               `json:"sequence_number_start,omitempty"`
	SequenceNumberEnd   int64               `json:"sequence_number_end,omitempty"`
	CreatedAt           string              `json:"created_at"`
	UpdatedAt           string              `json:"updated_at"`
	StartedAt           string              `json:"started_at,omitempty"`
	CompletedAt         string              `json:"completed_at,omitempty"`
}
