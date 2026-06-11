package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLocalAWSRefreshRequiresNovaProvider(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	appEnvironment = "local"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	// The refresh broker is Nova-only; an unknown/non-Nova provider must be
	// rejected before any broker call.
	voiceProvider = "some-other-provider"
	meetingAccess = newMeetingAccessManager()

	req := httptest.NewRequest(http.MethodPost, "http://localhost:3001/setup/aws/refresh", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	localAWSCredentialRefreshHandler(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("local AWS refresh status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestLocalAWSRefreshProxiesToBrokerForNova(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	appEnvironment = "local"
	appAuthMode = "disabled"
	appRoomID = "team-room"
	appBoardID = "team-board"
	voiceProvider = "nova-sonic"
	meetingAccess = newMeetingAccessManager()

	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("broker method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer refresh-token" {
			t.Fatalf("broker authorization = %q", got)
		}
		writeJSON(w, http.StatusAccepted, localAWSRefreshBrokerResponse{
			OK:      true,
			Started: true,
			Running: true,
			Message: "started",
		})
	}))
	defer broker.Close()
	t.Setenv("APP_LOCAL_AWS_REFRESH_URL", broker.URL)
	t.Setenv("APP_LOCAL_AWS_REFRESH_TOKEN", "refresh-token")
	localAWSRefreshLastStart = time.Time{}

	req := httptest.NewRequest(http.MethodPost, "http://localhost:3001/setup/aws/refresh", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	localAWSCredentialRefreshHandler(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("local AWS refresh status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response localAWSRefreshBrokerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.Started {
		t.Fatalf("response = %+v", response)
	}
}
