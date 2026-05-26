import type {
  AgentRunView,
  Card,
  RunQuestion,
} from "../types/board"

export function makeCard(overrides: Partial<Card> = {}): Card {
  return {
    id: "ABV2-088",
    status: "In Progress",
    title: "Wire drawer to backend",
    notes: "",
    tags: [],
    issueType: "Story",
    ...overrides,
  }
}

export function makeQuestion(overrides: Partial<RunQuestion> = {}): RunQuestion {
  return {
    id: "q-1",
    tenant_id: "default",
    board_id: "default",
    run_id: "run-1",
    card_id: "ABV2-088",
    step_index: 1,
    prompt: "Should I retry the failed lint step?",
    reasoning: "ESLint flagged 3 unused imports; safe to autofix.",
    suggestions: ["Yes, autofix", "No, leave it", "Show the diff first"],
    asked_at: new Date().toISOString(),
    ttl_seconds: 600,
    status: "open",
    ...overrides,
  }
}

export function makeRun(overrides: Partial<AgentRunView> = {}): AgentRunView {
  return {
    run_id: "run-1",
    card_id: "ABV2-088",
    agent_profile: "nova",
    status: "needs_input",
    current_step: "Running tests",
    finding_count: 0,
    jira_comment_posted: false,
    pr_review_posted: false,
    plan: [
      { index: 0, title: "Clone repo", status: "done" },
      { index: 1, title: "Run tests", status: "running" },
      { index: 2, title: "Open PR", status: "pending" },
    ],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  }
}
