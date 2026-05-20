package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVoiceStatusReportsOpenAIReady(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	t.Setenv("OPENAI_REALTIME_MODEL", defaultRealtimeModel)
	t.Setenv("OPENAI_REALTIME_TRANSCRIPTION_MODEL", defaultRealtimeTranscriptionModel)

	voiceProvider = "openai"
	voiceModels.set(voiceProviderOpenAI, defaultRealtimeModel)
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	kanbanApp = newKanbanBoardApp(newKanbanBoard())
	kanbanApp.mu.Lock()
	kanbanApp.connected = true
	kanbanApp.connectedAt = time.Date(2026, 5, 19, 15, 0, 0, 0, time.UTC)
	kanbanApp.mu.Unlock()

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
	if response.VoiceModel != defaultRealtimeModel || response.TranscriptionModel != defaultRealtimeTranscriptionModel {
		t.Fatalf("voice models = %s/%s, want %s/%s", response.VoiceModel, response.TranscriptionModel, defaultRealtimeModel, defaultRealtimeTranscriptionModel)
	}
	if response.AgentConnectedAt == "" {
		t.Fatalf("AgentConnectedAt empty, response = %+v", response)
	}
}

func TestVoiceStatusReportsInvalidOpenAIModel(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	t.Setenv("OPENAI_REALTIME_MODEL", "gpt-realtime-translate")
	t.Setenv("OPENAI_REALTIME_TRANSCRIPTION_MODEL", defaultRealtimeTranscriptionModel)

	voiceProvider = "openai"
	voiceModels.set(voiceProviderOpenAI, "gpt-realtime-translate")
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
	if response.Ready || response.AgentReady || !response.RequiresRestart {
		t.Fatalf("voice readiness = %+v, want blocked OpenAI model", response)
	}
	if !strings.Contains(response.Message, "cannot run Jira/GitHub tools") {
		t.Fatalf("message = %q, want model guidance", response.Message)
	}
}

func TestVoiceStatusReportsOpenAIRealtimeConnectionError(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	t.Setenv("OPENAI_REALTIME_MODEL", defaultRealtimeModel)
	t.Setenv("OPENAI_REALTIME_TRANSCRIPTION_MODEL", defaultRealtimeTranscriptionModel)

	voiceProvider = "openai"
	voiceModels.set(voiceProviderOpenAI, defaultRealtimeModel)
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	kanbanApp = newKanbanBoardApp(newKanbanBoard())
	kanbanApp.recordRealtimeJoinError(errors.New("Realtime session failed: status=429 Too Many Requests body={\"error\":{\"code\":\"insufficient_quota\"}}"))

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
	if response.Ready || response.AgentReady || response.AgentParticipantPresent {
		t.Fatalf("voice readiness = %+v, want OpenAI peer blocked", response)
	}
	if !strings.Contains(response.LastError, "insufficient_quota") {
		t.Fatalf("last error = %q, want quota detail", response.LastError)
	}
	if !strings.Contains(strings.ToLower(response.Message), "quota") {
		t.Fatalf("message = %q, want quota guidance", response.Message)
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
