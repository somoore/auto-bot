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
	if !strings.Contains(err.Error(), "Bedrock") {
		t.Fatalf("classifyRun error = %q, want Bedrock setup error", err.Error())
	}
}
