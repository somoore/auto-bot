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
