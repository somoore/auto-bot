//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveAgentRunCodeReview(t *testing.T) {
	if strings.TrimSpace(getEnvDefault("AGENT_RUN_LIVE_TEST", "")) != "1" {
		t.Skip("set AGENT_RUN_LIVE_TEST=1 to run the live Bedrock/GitHub/Jira agent test")
	}

	repo := normalizeRepoSpecifier(os.Getenv("GITHUB_DEFAULT_REPO"))
	if repo == "" {
		t.Fatal("GITHUB_DEFAULT_REPO is required")
	}
	objective := strings.TrimSpace(os.Getenv("AGENT_RUN_TEST_OBJECTIVE"))
	if objective == "" {
		objective = "Run an autonomous code review on the linked pull request and report findings to Jira and the PR."
	}
	prNumber, _ := asInt(os.Getenv("AGENT_RUN_TEST_PR_NUMBER"))
	if prNumber <= 0 {
		t.Fatal("AGENT_RUN_TEST_PR_NUMBER is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadJiraConfig(ctx, strings.TrimSpace(getEnvDefault("JIRA_CONFIG_PATH", "")))
	if err != nil {
		t.Fatalf("load Jira config: %v", err)
	}

	board := newKanbanBoard()
	syncer := &jiraSyncer{
		board:  board,
		client: newJiraClient(config),
		config: config,
	}

	title := "auto-bot agent review smoke " + time.Now().UTC().Format("20060102T150405Z")
	createArgs := fmt.Sprintf(`{"title":%q,"notes":"Created by live autonomous agent integration test.","tags":["auto-bot-agent","agent-review-smoke"],"status":"Backlog"}`, title)
	result, changed, err := board.ApplyToolCall("create_ticket", createArgs)
	if err != nil {
		t.Fatalf("create_ticket local mutation: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket did not mutate")
	}
	if err := syncer.ApplyToolCall(ctx, "create_ticket", createArgs, result); err != nil {
		t.Fatalf("create_ticket Jira sync: %v", err)
	}
	issueKey := findCardIDByTitle(board.SnapshotState(), title)
	if issueKey == "" {
		t.Fatal("created card was not renamed to Jira issue key")
	}
	t.Logf("created Jira issue %s", issueKey)

	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", repo, prNumber)
	linkArgs := fmt.Sprintf(`{"card_id":%q,"url":%q,"title":"Smoke test pull request","summary":"Linked for live autonomous agent review."}`, issueKey, prURL)
	result, changed, err = board.ApplyToolCall("add_remote_link", linkArgs)
	if err != nil {
		t.Fatalf("add_remote_link local mutation: %v", err)
	}
	if !changed {
		t.Fatal("add_remote_link did not mutate")
	}
	if err := syncer.ApplyToolCall(ctx, "add_remote_link", linkArgs, result); err != nil {
		t.Fatalf("add_remote_link Jira sync: %v", err)
	}

	orchestrator := setupAgentRunOrchestrator(ctx, board, syncer)
	if orchestrator.model == nil {
		t.Fatal("Bedrock model client is not configured")
	}
	if orchestrator.github == nil || !orchestrator.github.Configured() {
		t.Fatal("GitHub App client is not configured")
	}

	assignArgs := fmt.Sprintf(`{"card_id":%q,"objective":%q,"repo":%q,"pull_request_number":%d}`, issueKey, objective, repo, prNumber)
	runResult, changed, err := board.ApplyToolCall("assign_ticket_to_agent", assignArgs)
	if err != nil {
		t.Fatalf("assign_ticket_to_agent local mutation: %v", err)
	}
	if !changed {
		t.Fatal("assign_ticket_to_agent did not mutate")
	}
	run := runResult["agent_run"].(agentRunView)
	orchestrator.Start(run.RunID)

	finalRun, ok := board.agentRunByID(run.RunID)
	if !ok {
		t.Fatalf("agent run %s not found", run.RunID)
	}
	if finalRun.Status != agentRunCompleted {
		t.Fatalf("agent run status = %q, error = %q, checkpoints = %+v", finalRun.Status, finalRun.Error, finalRun.Checkpoints)
	}
	if !finalRun.JiraCommentPosted {
		t.Fatalf("agent run did not post a Jira comment: %+v", finalRun)
	}
	if orchestrator.github.PRCommentsEnabled() && !finalRun.PRReviewPosted {
		t.Fatalf("GITHUB_PR_COMMENTS_ENABLED=true but PR review was not posted: %+v", finalRun.Checkpoints)
	}
	t.Logf("agent run %s completed with %d findings; Jira comment posted=%v PR review posted=%v", finalRun.RunID, len(finalRun.Findings), finalRun.JiraCommentPosted, finalRun.PRReviewPosted)
}
