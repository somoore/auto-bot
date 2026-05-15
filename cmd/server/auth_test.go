package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestQueryTokenDoesNotAuthenticateRequest(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "test-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/websocket?token=test-secret", nil)
	if _, ok := authorizeRequest(req); ok {
		t.Fatal("query-string token authenticated request; want cookie or Bearer token only")
	}
}

func TestSessionCookieAuthenticatesRoomAndBoard(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "test-secret"
	appAuthMode = "token"
	appRoomID = "team-room"
	appBoardID = "team-board"
	authStore = newWebAuthStore(time.Hour)

	body, err := json.Marshal(createSessionRequest{
		Token:    "test-secret",
		Identity: "Scott_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/session", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	createSessionHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createSessionHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies length = %d, want 1", len(cookies))
	}
	if !cookies[0].HttpOnly {
		t.Fatal("session cookie is not HttpOnly")
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/websocket?room_id=team-room&board_id=team-board", nil)
	authorizedReq.AddCookie(cookies[0])
	ctx, ok := authorizeRequest(authorizedReq)
	if !ok {
		t.Fatal("session cookie did not authenticate matching room/board request")
	}
	if ctx.Identity != "Scott_1" || ctx.RoomID != "team-room" || ctx.BoardID != "team-board" {
		t.Fatalf("auth context = %+v", ctx)
	}

	wrongBoardReq := httptest.NewRequest(http.MethodGet, "/websocket?room_id=team-room&board_id=other-board", nil)
	wrongBoardReq.AddCookie(cookies[0])
	if _, ok := authorizeRequest(wrongBoardReq); ok {
		t.Fatal("session cookie authenticated a different board")
	}
}

func TestConfigureSecurityRejectsProductionLiveKitDevCredentials(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_AUTH_MODE", "token")
	t.Setenv("APP_ROOM_ID", "kanban-meeting")
	t.Setenv("APP_BOARD_ID", "default")
	t.Setenv("LIVEKIT_API_KEY", "devkey")
	t.Setenv("LIVEKIT_API_SECRET", "secret")
	voiceProvider = "nova-sonic"
	apiToken = "strong-test-token"

	if err := configureAppSecurity(); err == nil {
		t.Fatal("configureAppSecurity accepted LiveKit dev credentials outside APP_ENV=local")
	}
}

func TestConfigureSecurityRejectsDisabledAuthOutsideLocal(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_AUTH_MODE", "disabled")
	voiceProvider = "openai"
	apiToken = ""

	if err := configureAppSecurity(); err == nil {
		t.Fatal("configureAppSecurity accepted disabled auth outside APP_ENV=local")
	}
}

func TestFrontendDoesNotReferenceBrowserVisibleAppToken(t *testing.T) {
	for _, path := range []string{"../../web/index.html", "../../web/index_livekit.html"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		html := string(raw)
		for _, forbidden := range []string{"__APP_TOKEN__", "{{.Token}}", "token="} {
			if strings.Contains(html, forbidden) {
				t.Fatalf("%s contains browser-visible token marker %q", path, forbidden)
			}
		}
	}
}

func snapshotAuthGlobals() func() {
	oldAPIToken := apiToken
	oldAuthMode := appAuthMode
	oldEnvironment := appEnvironment
	oldRoomID := appRoomID
	oldBoardID := appBoardID
	oldAuthStore := authStore
	oldVoiceProvider := voiceProvider
	return func() {
		apiToken = oldAPIToken
		appAuthMode = oldAuthMode
		appEnvironment = oldEnvironment
		appRoomID = oldRoomID
		appBoardID = oldBoardID
		authStore = oldAuthStore
		voiceProvider = oldVoiceProvider
	}
}
