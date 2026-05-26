// Board / agent wire types — mirror Go JSON shapes in
// internal/board/types.go and internal/agent/types.go literally. Field
// naming is intentionally mixed (camelCase for Card, snake_case for
// RunView, mixed on BoardState) because that is what the server emits.

export type CardStatus = "Backlog" | "In Progress" | "Blocked" | "Done"

export const CARD_STATUSES: readonly CardStatus[] = [
  "Backlog",
  "In Progress",
  "Blocked",
  "Done",
] as const

export type ActorKind = "human" | "agent"

export interface Actor {
  kind: ActorKind
  id: string
  displayName?: string
  avatarRef?: string
  agentProfile?: string
  ownerHumanId?: string
  email?: string
}

export interface User {
  accountId?: string
  displayName?: string
  emailAddress?: string
  active: boolean
}

export interface Estimate { original?: string; remaining?: string }
export interface Sprint {
  id?: number; name?: string; state?: string; goal?: string
  startDate?: string; endDate?: string
}
export interface Comment {
  id?: string; body: string; author?: string; createdAt?: string
}
export interface IssueLink {
  id?: string; type: string; direction?: string; sourceCardId?: string
  targetCardId: string; targetSummary?: string; targetStatus?: string
  relationship?: string; createdByVoice?: boolean
}
export interface Worklog {
  id?: string; author?: string; timeSpent: string
  timeSpentSeconds?: number; started?: string; comment?: string; createdAt?: string
}
export interface RemoteLink { id?: string; url: string; title: string; summary?: string }
export interface CustomField {
  name?: string
  // Custom field values are integration-defined; unknown is justified
  // because consumers branch on the concrete value at the read site.
  value?: unknown
}

export interface Card {
  id: string
  status: CardStatus
  title: string
  notes: string
  tags: string[]
  issueType?: string
  parentId?: string
  epicKey?: string
  assignee?: Actor
  reporter?: User
  watchers?: User[]
  dueDate?: string
  priority?: string
  storyPoints?: number
  estimate?: Estimate
  originalEstimate?: string
  remainingEstimate?: string
  sprint?: Sprint
  rank?: string
  rankHint?: string
  components?: string[]
  fixVersions?: string[]
  blockedReason?: string
  comments?: Comment[]
  issueLinks?: IssueLink[]
  worklogs?: Worklog[]
  remoteLinks?: RemoteLink[]
  customFields?: Record<string, CustomField>
}

export type RunQuestionStatus = "open" | "answered" | "expired" | "cancelled"

export interface RunQuestion {
  id: string
  tenant_id: string
  board_id: string
  run_id: string
  card_id: string
  step_index: number
  prompt: string
  reasoning?: string
  suggestions?: string[]
  asked_at: string
  ttl_seconds: number
  answered_at?: string
  answer?: string
  answered_by?: string
  answered_via?: "ui" | "voice" | "mcp"
  status: RunQuestionStatus
}

export interface RunQuestionRef {
  question_id: string
  prompt: string
  asked_at: string
}

export type AgentRunStatus =
  | "queued" | "classifying" | "fetching_context" | "reviewing"
  | "publishing" | "retrying" | "needs_input" | "waiting_on_human"
  | "completed" | "failed" | "unsupported" | "cancelled" | "taken_over"

export interface PlanStep {
  index: number
  title: string
  description?: string
  estimated_ms?: number
  started_at?: string
  completed_at?: string
  status: "pending" | "running" | "done" | "skipped" | "paused"
  outcome?: string
}

export interface CostBreakdown {
  cents: number
  by_model?: Record<string, number>
  audio_seconds?: number
  tokens_in?: number
  tokens_out?: number
  updated_at?: string
}

export interface Checkpoint {
  at: string
  status: AgentRunStatus
  step?: string
  message: string
}

export interface AgentRunView {
  run_id: string
  card_id: string
  jira_issue_key?: string
  card_title?: string
  objective?: string
  requested_by?: string
  retry_of?: string
  agent_profile?: string
  request_type?: string
  specialist?: string
  status: AgentRunStatus
  current_step?: string
  repo?: string
  branch?: string
  pull_request_number?: number
  pull_request_url?: string
  pm_model?: string
  review_model?: string
  review_lens?: string
  finding_count: number
  summary?: string
  publish_warnings?: string[]
  cost_budget_cents?: number
  estimated_cost_cents?: number
  model_calls?: number
  jira_comment_posted: boolean
  pr_review_posted: boolean
  error?: string
  checkpoints?: Checkpoint[]
  plan?: PlanStep[]
  cost?: CostBreakdown
  waiting_on?: RunQuestionRef
  sequence_number_start?: number
  sequence_number_end?: number
  created_at: string
  updated_at: string
  started_at?: string
  completed_at?: string
}

export interface BoardState {
  cards: Card[]
  agentRuns?: AgentRunView[]
  open_run_questions?: RunQuestion[]
  meeting?: unknown
  pendingConfirmations?: unknown[]
  recentMutations?: unknown[]
  conflicts?: unknown[]
  updatedAt?: string
  sequenceNumber: number
}

export type BoardEventName =
  | "board"
  | "run_question_asked"
  | "run_question_answered"
  | "run_question_expired"
  | "agent_run"
  | "action_result"
  | "status"
