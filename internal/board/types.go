package board

// Status is the kanban column / workflow state of a Card.
type Status string

// Canonical kanban statuses. External systems (Jira, Linear, GitHub) map their
// workflow states onto these via the projection layer.
const (
	StatusBacklog    Status = "Backlog"
	StatusInProgress Status = "In Progress"
	StatusBlocked    Status = "Blocked"
	StatusDone       Status = "Done"
)

// Card is the JSON card shape shared by browser clients, Jira sync,
// meeting reports, and model-safe board snapshots.
type Card struct {
	ID                string           `json:"id"`
	Status            Status           `json:"status"`
	Title             string           `json:"title"`
	Notes             string           `json:"notes"`
	Tags              []string         `json:"tags"`
	IssueType         string           `json:"issueType,omitempty"`
	ParentID          string           `json:"parentId,omitempty"`
	EpicKey           string           `json:"epicKey,omitempty"`
	Assignee          *User            `json:"assignee,omitempty"`
	Reporter          *User            `json:"reporter,omitempty"`
	Watchers          []User           `json:"watchers,omitempty"`
	DueDate           string           `json:"dueDate,omitempty"`
	Priority          string           `json:"priority,omitempty"`
	StoryPoints       *float64         `json:"storyPoints,omitempty"`
	Estimate          *Estimate        `json:"estimate,omitempty"`
	OriginalEstimate  string           `json:"originalEstimate,omitempty"`
	RemainingEstimate string           `json:"remainingEstimate,omitempty"`
	Sprint            *Sprint          `json:"sprint,omitempty"`
	Rank              string           `json:"rank,omitempty"`
	RankHint          string           `json:"rankHint,omitempty"`
	Components        []string         `json:"components,omitempty"`
	FixVersions       []string         `json:"fixVersions,omitempty"`
	BlockedReason     string           `json:"blockedReason,omitempty"`
	Comments          []Comment        `json:"comments,omitempty"`
	IssueLinks        []IssueLink      `json:"issueLinks,omitempty"`
	Worklogs          []Worklog        `json:"worklogs,omitempty"`
	RemoteLinks       []RemoteLink     `json:"remoteLinks,omitempty"`
	CustomFields      map[string]Field `json:"customFields,omitempty"`
}

// User is the normalized user identity shape used for assignees,
// reporters, and watchers.
type User struct {
	AccountID    string `json:"accountId,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active"`
}

// Comment is a single comment attached to a Card.
type Comment struct {
	ID        string `json:"id,omitempty"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// Estimate captures original and remaining time estimates for a Card.
type Estimate struct {
	Original  string `json:"original,omitempty"`
	Remaining string `json:"remaining,omitempty"`
}

// Sprint is the sprint membership shape attached to a Card.
type Sprint struct {
	ID        int    `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	State     string `json:"state,omitempty"`
	Goal      string `json:"goal,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

// IssueLink is a typed relationship between two Cards (blocks, relates, etc.).
type IssueLink struct {
	ID             string `json:"id,omitempty"`
	Type           string `json:"type"`
	Direction      string `json:"direction,omitempty"`
	SourceCardID   string `json:"sourceCardId,omitempty"`
	TargetCardID   string `json:"targetCardId"`
	TargetSummary  string `json:"targetSummary,omitempty"`
	TargetStatus   string `json:"targetStatus,omitempty"`
	Relationship   string `json:"relationship,omitempty"`
	CreatedByVoice bool   `json:"createdByVoice,omitempty"`
}

// Worklog is a time-tracking entry recorded against a Card.
type Worklog struct {
	ID               string `json:"id,omitempty"`
	Author           string `json:"author,omitempty"`
	TimeSpent        string `json:"timeSpent"`
	TimeSpentSeconds int64  `json:"timeSpentSeconds,omitempty"`
	Started          string `json:"started,omitempty"`
	Comment          string `json:"comment,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
}

// RemoteLink attaches an external URL (design doc, PR, dashboard) to a Card.
type RemoteLink struct {
	ID      string `json:"id,omitempty"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// Field is a custom Jira/Linear field value carried on a Card.
type Field struct {
	Name  string `json:"name,omitempty"`
	Value any    `json:"value,omitempty"`
}
