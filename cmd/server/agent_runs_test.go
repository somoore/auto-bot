package main

import (
	"context"
	"strings"
	"testing"
)

func TestAgentModelDefaultsUseAWSBedrockClaudeSonnetAndHaiku(t *testing.T) {
	t.Setenv("AGENT_PM_MODEL", "")
	t.Setenv("AGENT_REVIEW_MODEL", "")

	if got := agentPMModel(); !strings.Contains(got, "anthropic.claude-haiku") {
		t.Fatalf("agent PM model = %q, want Bedrock Claude Haiku model id", got)
	}
	if got := agentReviewModel(); !strings.Contains(got, "anthropic.claude-sonnet") {
		t.Fatalf("agent review model = %q, want Bedrock Claude Sonnet model id", got)
	}
}

func TestAgentClassificationRequiresBedrockClient(t *testing.T) {
	orchestrator := &agentRunOrchestrator{}
	_, err := orchestrator.classifyRun(context.Background(), agentRun{
		CardID:       "EMAL-99",
		CardTitle:    "Review auth PR",
		Objective:    "run a code review",
		AgentProfile: "project_manager",
		RequestType:  "auto",
		Repo:         "scottmoore/auto-bot",
		PMModel:      agentPMModel(),
	})
	if err == nil {
		t.Fatal("classifyRun should fail when the AWS Bedrock model client is not configured")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "bedrock") {
		t.Fatalf("classifyRun error = %q, want Bedrock setup error", err.Error())
	}
}

func TestSecurityReviewRoutesThroughPullRequestReviewer(t *testing.T) {
	run := agentRun{
		Objective:   "conduct a security review for auth bypasses and injection exploit paths",
		RequestType: "security_scan",
		Specialist:  "security_scanner",
		CardTitle:   "Review auth PR",
	}
	classification := deterministicAgentClassification(run)
	if classification.RequestType != "security_scan" || classification.Specialist != "security_scanner" {
		t.Fatalf("classification = %#v, want security scanner", classification)
	}
	if !agentRunCanUsePullRequestReviewer(run) {
		t.Fatal("security scanner runs should use the PR reviewer with security lens")
	}
	if got := reviewLensForRun(run); got != "security" {
		t.Fatalf("review lens = %q, want security", got)
	}
}

func TestAgentRunCommentIncludesSecurityReviewFields(t *testing.T) {
	comment := formatAgentRunComment(agentRun{
		RunID:             "agent-run-1",
		Status:            agentRunCompleted,
		Specialist:        "security_scanner",
		RequestType:       "security_scan",
		ReviewLens:        "security",
		Repo:              "somoore/auto-bot",
		PullRequestNumber: 7,
		Summary:           "Found one exploitable issue.",
		Findings: []codeReviewFinding{{
			Severity:        "high",
			Category:        "security",
			CWE:             "CWE-89",
			Title:           "SQL query can be injected",
			File:            "cmd/server/search.go",
			Line:            42,
			Body:            "User-controlled text is concatenated into SQL.",
			Evidence:        "The diff adds string concatenation inside the query builder.",
			Impact:          "A caller can read unrelated tenant data.",
			ExploitScenario: "Submit crafted search text that changes the WHERE clause.",
			SuggestedFix:    "Use parameterized queries.",
			Tests:           []string{"add injection regression test"},
		}},
	})
	for _, want := range []string{"Review lens: security", "CWE-89", "Evidence:", "Impact:", "Exploit scenario:", "Validate with:"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("comment missing %q:\n%s", want, comment)
		}
	}
}

func TestGitHubInlineReviewCommentsFromFindings(t *testing.T) {
	comments := githubReviewCommentsFromFindings([]codeReviewFinding{
		{
			Severity:        "high",
			Category:        "security",
			Title:           "Auth bypass",
			File:            "cmd/server/auth.go",
			Line:            77,
			Body:            "The new branch trusts caller-supplied identity.",
			Impact:          "Unauthorized Jira writes.",
			ExploitScenario: "Send a forged identity header.",
			SuggestedFix:    "Bind identity to the server-side session.",
			Tests:           []string{"forged header is rejected"},
		},
		{Title: "No location", Body: "Should not become inline."},
	})
	if len(comments) != 1 {
		t.Fatalf("comments = %#v, want one inline comment", comments)
	}
	if comments[0]["path"] != "cmd/server/auth.go" || comments[0]["line"] != 77 || comments[0]["side"] != "RIGHT" {
		t.Fatalf("inline comment = %#v, want path/line/right side", comments[0])
	}
	body, _ := comments[0]["body"].(string)
	for _, want := range []string{"HIGH", "Auth bypass", "Suggested fix", "Exploit scenario"} {
		if !strings.Contains(body, want) {
			t.Fatalf("inline body missing %q:\n%s", want, body)
		}
	}
}

func TestAgentRunCostBudgetBlocksEstimatedOverrun(t *testing.T) {
	t.Setenv("AGENT_COST_BUDGET_CENTS", "10")
	board := newKanbanBoard()
	previousOrchestrator := agentOrchestrator
	agentOrchestrator = nil
	t.Cleanup(func() { agentOrchestrator = previousOrchestrator })

	cardResult, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Review budget PR","status":"Backlog"}`)
	if err != nil || !changed {
		t.Fatalf("create_ticket changed=%v err=%v", changed, err)
	}
	card := cardResult["card"].(kanbanCard)
	runResult, changed, err := board.ApplyToolCallWithMeta("assign_ticket_to_agent", `{"card_id":"`+card.ID+`","objective":"review the PR","repo":"scottmoore/auto-bot","pull_request_number":7}`, toolCallMeta{Dispatcher: "test", SkipConfirmation: true})
	if err != nil || !changed {
		t.Fatalf("assign_ticket_to_agent changed=%v err=%v", changed, err)
	}
	run := runResult["agent_run"].(agentRunView)
	orchestrator := &agentRunOrchestrator{board: board}
	if err := orchestrator.reserveAgentRunCost(run.RunID, "pm", 4); err != nil {
		t.Fatalf("reserve PM cost returned error: %v", err)
	}
	if err := orchestrator.reserveAgentRunCost(run.RunID, "review", 7); err == nil {
		t.Fatal("reserveAgentRunCost should reject estimated cost above budget")
	}
	updated, ok := board.agentRunByID(run.RunID)
	if !ok {
		t.Fatal("run missing")
	}
	if updated.EstimatedCostCents != 4 || updated.ModelCalls != 1 || updated.CostBudgetCents != 10 {
		t.Fatalf("budget state = %#v, want one reserved PM call under ten-cent cap", updated)
	}
}

func TestAgentRunTakeoverTagsPartialWork(t *testing.T) {
	board := newKanbanBoard()
	previousOrchestrator := agentOrchestrator
	agentOrchestrator = nil
	t.Cleanup(func() { agentOrchestrator = previousOrchestrator })

	cardResult, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Implement retry controls","status":"In Progress"}`)
	if err != nil || !changed {
		t.Fatalf("create_ticket changed=%v err=%v", changed, err)
	}
	card := cardResult["card"].(kanbanCard)
	runResult, changed, err := board.ApplyToolCallWithMeta("assign_ticket_to_agent", `{"card_id":"`+card.ID+`","objective":"finish the controls","repo":"scottmoore/auto-bot","pull_request_number":8}`, toolCallMeta{Dispatcher: "test", SkipConfirmation: true})
	if err != nil || !changed {
		t.Fatalf("assign_ticket_to_agent changed=%v err=%v", changed, err)
	}
	run := runResult["agent_run"].(agentRunView)
	result, changed, err := board.ApplyToolCallWithMeta("take_over_agent_run", `{"run_id":"`+run.RunID+`","actor":"Scott","reason":"Scott will finish from here"}`, toolCallMeta{Dispatcher: "test", SkipConfirmation: true})
	if err != nil || !changed {
		t.Fatalf("take_over_agent_run changed=%v err=%v result=%#v", changed, err, result)
	}
	updated, ok := board.agentRunByID(run.RunID)
	if !ok || updated.Status != agentRunTakenOver {
		t.Fatalf("updated run = %#v, want taken_over", updated)
	}
	state := board.SnapshotState()
	var foundTag bool
	for _, candidate := range state.Cards {
		if candidate.ID != card.ID {
			continue
		}
		for _, tag := range candidate.Tags {
			if tag == "partial-agent-work" {
				foundTag = true
			}
		}
	}
	if !foundTag {
		t.Fatalf("card was not tagged partial-agent-work: %#v", state.Cards)
	}
}

func TestAgentRunRetryQueuesReplacementRun(t *testing.T) {
	board := newKanbanBoard()
	previousOrchestrator := agentOrchestrator
	agentOrchestrator = nil
	t.Cleanup(func() { agentOrchestrator = previousOrchestrator })

	cardResult, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Retry review","status":"Backlog"}`)
	if err != nil || !changed {
		t.Fatalf("create_ticket changed=%v err=%v", changed, err)
	}
	card := cardResult["card"].(kanbanCard)
	runResult, changed, err := board.ApplyToolCallWithMeta("assign_ticket_to_agent", `{"card_id":"`+card.ID+`","objective":"review the PR","repo":"scottmoore/auto-bot","pull_request_number":9}`, toolCallMeta{Dispatcher: "test", SkipConfirmation: true})
	if err != nil || !changed {
		t.Fatalf("assign_ticket_to_agent changed=%v err=%v", changed, err)
	}
	run := runResult["agent_run"].(agentRunView)
	retryResult, changed, err := board.ApplyToolCallWithMeta("retry_agent_run", `{"run_id":"`+run.RunID+`","additional_context":"focus on authz regressions"}`, toolCallMeta{Dispatcher: "test", SkipConfirmation: true})
	if err != nil || !changed {
		t.Fatalf("retry_agent_run changed=%v err=%v result=%#v", changed, err, retryResult)
	}
	original, ok := board.agentRunByID(run.RunID)
	if !ok || original.Status != agentRunRetrying {
		t.Fatalf("original run = %#v, want retrying", original)
	}
	retryRun := retryResult["agent_run"].(agentRunView)
	if retryRun.RetryOf != run.RunID || !strings.Contains(retryRun.Objective, "authz regressions") {
		t.Fatalf("retry run = %#v, want retry_of and added constraint", retryRun)
	}
}
