package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func resetVoiceModelsForTest(t *testing.T) {
	t.Helper()
	previous := voiceModels
	voiceModels = &runtimeVoiceModelSelection{models: map[string]string{}}
	t.Cleanup(func() { voiceModels = previous })
}

func TestVoiceModelOptionsExposeActiveNovaSonicAsSelectable(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	resetVoiceModelsForTest(t)

	voiceProvider = "nova-sonic"

	options := voiceModelOptions()
	var novaSelectable bool
	for _, option := range options {
		if option.Provider == voiceProviderNovaSonic && option.Selectable && !option.RequiresRestart {
			novaSelectable = true
		}
	}
	if !novaSelectable {
		t.Fatalf("expected active Nova Sonic options to be selectable: %#v", options)
	}
}

func TestHostCanUpdateActiveNovaSonicModel(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	resetVoiceModelsForTest(t)

	voiceProvider = "nova-sonic"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()

	req := httptest.NewRequest(http.MethodPost, "/voice/model", strings.NewReader(`{"provider":"nova-sonic","model":"amazon.nova-sonic-v1:0"}`))
	rec := httptest.NewRecorder()
	voiceModelHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("voiceModelHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response voiceModelStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.CurrentModel != "amazon.nova-sonic-v1:0" {
		t.Fatalf("voice model response = %+v", response)
	}
	if got := selectedNovaSonicModel(); got != "amazon.nova-sonic-v1:0" {
		t.Fatalf("selectedNovaSonicModel() = %q", got)
	}
}

func TestParticipantCannotUpdateVoiceModelDuringMeeting(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	resetVoiceModelsForTest(t)

	apiToken = "host-token"
	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	voiceProvider = "nova-sonic"
	authStore = newWebAuthStore(time.Hour)
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")
	setupReq := httptest.NewRequest(http.MethodPost, "/meeting/setup", strings.NewReader(`{"meeting_type":"standup"}`))
	setupReq.AddCookie(hostCookie)
	setupRec := httptest.NewRecorder()
	setupMeetingHandler(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", setupRec.Code, setupRec.Body.String())
	}
	var setupResponse struct {
		Meeting meetingAccessSnapshot `json:"meeting"`
	}
	if err := json.Unmarshal(setupRec.Body.Bytes(), &setupResponse); err != nil {
		t.Fatal(err)
	}

	joinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"`+setupResponse.Meeting.JoinCode+`","identity":"Sarah_1"}`))
	joinRec := httptest.NewRecorder()
	joinMeetingHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusOK {
		t.Fatalf("join status = %d, body = %s", joinRec.Code, joinRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPost, "/voice/model", strings.NewReader(`{"provider":"nova-sonic","model":"amazon.nova-sonic-v1:0"}`))
	updateReq.AddCookie(joinRec.Result().Cookies()[0])
	updateRec := httptest.NewRecorder()
	voiceModelHandler(updateRec, updateReq)
	if updateRec.Code != http.StatusForbidden {
		t.Fatalf("participant update status = %d, body = %s", updateRec.Code, updateRec.Body.String())
	}
}

func TestUnknownProviderModelSelectionIsRejected(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	resetVoiceModelsForTest(t)

	voiceProvider = "nova-sonic"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()

	req := httptest.NewRequest(http.MethodPost, "/voice/model", strings.NewReader(`{"provider":"some-other-provider","model":"mystery"}`))
	rec := httptest.NewRecorder()
	voiceModelHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown provider status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
