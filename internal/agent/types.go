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
const (
	StatusQueued          RunStatus = "queued"
	StatusClassifying     RunStatus = "classifying"
	StatusFetchingContext RunStatus = "fetching_context"
	StatusReviewing       RunStatus = "reviewing"
	StatusPublishing      RunStatus = "publishing"
	StatusRetrying        RunStatus = "retrying"
	StatusNeedsInput      RunStatus = "needs_input"
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
	CreatedAt          string              `json:"created_at"`
	UpdatedAt          string              `json:"updated_at"`
	StartedAt          string              `json:"started_at,omitempty"`
	CompletedAt        string              `json:"completed_at,omitempty"`
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
	RunID              string              `json:"run_id"`
	CardID             string              `json:"card_id"`
	JiraIssueKey       string              `json:"jira_issue_key,omitempty"`
	CardTitle          string              `json:"card_title,omitempty"`
	Objective          string              `json:"objective,omitempty"`
	RequestedBy        string              `json:"requested_by,omitempty"`
	RetryOf            string              `json:"retry_of,omitempty"`
	AgentProfile       string              `json:"agent_profile,omitempty"`
	RequestType        string              `json:"request_type,omitempty"`
	Specialist         string              `json:"specialist,omitempty"`
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
	FindingCount       int                 `json:"finding_count"`
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
	CreatedAt          string              `json:"created_at"`
	UpdatedAt          string              `json:"updated_at"`
	StartedAt          string              `json:"started_at,omitempty"`
	CompletedAt        string              `json:"completed_at,omitempty"`
}
