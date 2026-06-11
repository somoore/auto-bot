package main

import "testing"

func TestCurrentWorkspaceScopeUsesDeploymentDefaults(t *testing.T) {
	t.Setenv("APP_WORKSPACE_ID", "platform-team")
	t.Setenv("AWS_REGION", "us-east-1")
	previousVoiceProvider := voiceProvider
	voiceProvider = "nova-sonic"
	t.Cleanup(func() { voiceProvider = previousVoiceProvider })

	scope := currentWorkspaceScope(requestAuthContext{
		Identity: "scott",
		Role:     "host",
		BoardID:  "emal-board",
		RoomID:   "standup-room",
	})

	if scope.WorkspaceID != "platform-team" {
		t.Fatalf("WorkspaceID = %q, want platform-team", scope.WorkspaceID)
	}
	if scope.BoardID != "emal-board" || scope.RoomID != "standup-room" {
		t.Fatalf("scope board/room = %q/%q", scope.BoardID, scope.RoomID)
	}
	if scope.Region != "us-east-1" || scope.VoiceProvider != "nova-sonic" {
		t.Fatalf("scope region/provider = %q/%q", scope.Region, scope.VoiceProvider)
	}
	if scope.Metadata["future_model"] == "" {
		t.Fatal("workspace scope should document future isolation model")
	}
}
