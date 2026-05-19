package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoiceStatusReportsOpenAIReady(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	voiceProvider = "openai"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"

	req := httptest.NewRequest(http.MethodGet, "/voice/status?room_id=team-room&board_id=team-board", nil)
	rec := httptest.NewRecorder()
	voiceStatusHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("voiceStatusHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response voiceReadinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Ready || !response.AWSReady || !response.AgentReady {
		t.Fatalf("voice readiness = %+v, want fully ready", response)
	}
}

func TestVoiceStatusReportsNovaMissingAWS(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	voiceProvider = "nova-sonic"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	validateAWSRuntimeCredentials = func(context.Context) awsCredentialPreflightResult {
		return awsCredentialPreflightResult{
			Ready:  false,
			Region: "us-east-1",
			Error:  "ExpiredToken: temporary credentials expired",
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/voice/status?room_id=team-room&board_id=team-board", nil)
	rec := httptest.NewRecorder()
	voiceStatusHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("voiceStatusHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response voiceReadinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Ready || response.AWSReady || response.AgentReady {
		t.Fatalf("voice readiness = %+v, want nova blocked before join", response)
	}
	if !response.RequiresRestart || response.RecoveryCommand != "scripts/local-up.sh" {
		t.Fatalf("recovery fields = %+v", response)
	}
	if !strings.Contains(response.Message, "scripts/local-up.sh") {
		t.Fatalf("message = %q, want local-up recovery", response.Message)
	}
}

func TestLiveKitTokenRefusesWhenNovaIsNotReady(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	voiceProvider = "nova-sonic"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	validateAWSRuntimeCredentials = func(context.Context) awsCredentialPreflightResult {
		return awsCredentialPreflightResult{
			Ready:  false,
			Region: "us-east-1",
			Error:  "AWS credentials are not available",
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/livekit-token?identity=Scott_1&room_id=team-room&board_id=team-board", nil)
	rec := httptest.NewRecorder()
	livekitTokenHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("livekitTokenHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response voiceReadinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Ready {
		t.Fatalf("livekit token readiness = %+v, want not ready", response)
	}
}
