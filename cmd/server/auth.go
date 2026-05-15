package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	authCookieName       = "auto_bot_session"
	defaultSessionTTL    = 12 * time.Hour
	defaultAppRoomID     = "kanban-meeting"
	defaultAppBoardID    = "default"
	defaultLocalAPIToken = "local-dev-only-change-me"
)

var (
	appEnvironment = "production"
	appAuthMode    = "token"
	appRoomID      = defaultAppRoomID
	appBoardID     = defaultAppBoardID
	authStore      = newWebAuthStore(defaultSessionTTL)
)

type requestAuthContext struct {
	SessionID string `json:"-"`
	Identity  string `json:"participant_identity"`
	RoomID    string `json:"room_id"`
	BoardID   string `json:"board_id"`
	AuthMode  string `json:"auth_mode,omitempty"`
}

type webSession struct {
	ID        string
	Identity  string
	RoomID    string
	BoardID   string
	ExpiresAt time.Time
}

type webAuthStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	sessions map[string]webSession
}

func newWebAuthStore(ttl time.Duration) *webAuthStore {
	return &webAuthStore{
		ttl:      ttl,
		sessions: map[string]webSession{},
	}
}

func configureAppSecurity() error {
	appEnvironment = strings.ToLower(strings.TrimSpace(getEnvDefault("APP_ENV", "production")))
	appAuthMode = strings.ToLower(strings.TrimSpace(getEnvDefault("APP_AUTH_MODE", "token")))
	appRoomID = normalizeRuntimeID(getEnvDefault("APP_ROOM_ID", defaultAppRoomID), defaultAppRoomID)
	appBoardID = normalizeRuntimeID(getEnvDefault("APP_BOARD_ID", defaultAppBoardID), defaultAppBoardID)

	if appAuthMode != "token" && appAuthMode != "disabled" {
		return fmt.Errorf("APP_AUTH_MODE must be token or disabled")
	}
	if appAuthMode == "disabled" && appEnvironment != "local" {
		return fmt.Errorf("APP_AUTH_MODE=disabled is only allowed when APP_ENV=local")
	}
	if appAuthMode == "token" {
		if strings.TrimSpace(apiToken) == "" {
			return fmt.Errorf("APP_API_TOKEN is required when APP_AUTH_MODE=token")
		}
		if appEnvironment != "local" && apiToken == defaultLocalAPIToken {
			return fmt.Errorf("refusing default local APP_API_TOKEN outside APP_ENV=local")
		}
	}
	if err := validateLiveKitSecretSafety(); err != nil {
		return err
	}

	return nil
}

func validateLiveKitSecretSafety() error {
	if voiceProvider != "nova-sonic" {
		return nil
	}
	apiKey := strings.TrimSpace(os.Getenv("LIVEKIT_API_KEY"))
	apiSecret := strings.TrimSpace(os.Getenv("LIVEKIT_API_SECRET"))
	if appEnvironment == "local" {
		return nil
	}
	if apiKey == "" || apiSecret == "" {
		return fmt.Errorf("LIVEKIT_API_KEY and LIVEKIT_API_SECRET are required outside APP_ENV=local")
	}
	if apiKey == "devkey" || apiSecret == "secret" {
		return fmt.Errorf("refusing LiveKit dev credentials outside APP_ENV=local")
	}
	return nil
}

func normalizeRuntimeID(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	normalized := builder.String()
	if normalized == "" {
		return fallback
	}
	if len(normalized) > 64 {
		return normalized[:64]
	}
	return normalized
}

func defaultAuthContext(identity string) requestAuthContext {
	identity = normalizeParticipantIdentity(identity)
	if identity == "" {
		identity = "participant"
	}
	return requestAuthContext{
		Identity: identity,
		RoomID:   appRoomID,
		BoardID:  appBoardID,
		AuthMode: appAuthMode,
	}
}

func normalizeParticipantIdentity(identity string) string {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ""
	}
	if !validIdentityRe.MatchString(identity) {
		return ""
	}
	return identity
}

func authorizeRequest(r *http.Request) (requestAuthContext, bool) {
	if appAuthMode == "disabled" {
		ctx := defaultAuthContext("local-user")
		return ctx, requestMatchesAuthorizedRoomBoard(r, ctx)
	}

	if bearerToken(r) != "" && secureTokenEqual(bearerToken(r), apiToken) {
		identity := normalizeParticipantIdentity(r.Header.Get("X-Participant-Identity"))
		if identity == "" {
			identity = "api-token"
		}
		ctx := defaultAuthContext(identity)
		return ctx, requestMatchesAuthorizedRoomBoard(r, ctx)
	}

	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return requestAuthContext{}, false
	}
	session, ok := authStore.lookup(cookie.Value)
	if !ok {
		return requestAuthContext{}, false
	}
	ctx := requestAuthContext{
		SessionID: session.ID,
		Identity:  session.Identity,
		RoomID:    session.RoomID,
		BoardID:   session.BoardID,
		AuthMode:  appAuthMode,
	}
	return ctx, requestMatchesAuthorizedRoomBoard(r, ctx)
}

func requestMatchesAuthorizedRoomBoard(r *http.Request, ctx requestAuthContext) bool {
	requestedRoomID := strings.TrimSpace(r.URL.Query().Get("room_id"))
	if requestedRoomID != "" && requestedRoomID != ctx.RoomID {
		return false
	}
	requestedBoardID := strings.TrimSpace(r.URL.Query().Get("board_id"))
	if requestedBoardID != "" && requestedBoardID != ctx.BoardID {
		return false
	}
	return ctx.RoomID == appRoomID && ctx.BoardID == appBoardID
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[len("bearer "):])
	}
	return ""
}

func secureTokenEqual(got string, want string) bool {
	if got == "" || want == "" {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (store *webAuthStore) create(identity string) (webSession, error) {
	id, err := randomHex(32)
	if err != nil {
		return webSession{}, err
	}
	session := webSession{
		ID:        id,
		Identity:  identity,
		RoomID:    appRoomID,
		BoardID:   appBoardID,
		ExpiresAt: time.Now().UTC().Add(store.ttl),
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneExpiredLocked(time.Now().UTC())
	store.sessions[id] = session
	return session, nil
}

func (store *webAuthStore) lookup(id string) (webSession, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return webSession{}, false
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	now := time.Now().UTC()
	store.pruneExpiredLocked(now)
	session, ok := store.sessions[id]
	if !ok || !session.ExpiresAt.After(now) {
		delete(store.sessions, id)
		return webSession{}, false
	}
	return session, true
}

func (store *webAuthStore) delete(id string) {
	store.mu.Lock()
	delete(store.sessions, strings.TrimSpace(id))
	store.mu.Unlock()
}

func (store *webAuthStore) pruneExpiredLocked(now time.Time) {
	for id, session := range store.sessions {
		if !session.ExpiresAt.After(now) {
			delete(store.sessions, id)
		}
	}
}

func randomHex(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type createSessionRequest struct {
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

func sessionStatusHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, ok := authorizeRequest(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"participant_identity": ctx.Identity,
		"room_id":              ctx.RoomID,
		"board_id":             ctx.BoardID,
	})
}

func createSessionHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if appAuthMode == "disabled" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                   true,
			"participant_identity": "local-user",
			"room_id":              appRoomID,
			"board_id":             appBoardID,
		})
		return
	}

	var req createSessionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if !secureTokenEqual(strings.TrimSpace(req.Token), apiToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	identity := normalizeParticipantIdentity(req.Identity)
	if identity == "" {
		identity = "participant"
	}

	session, err := authStore.create(identity)
	if err != nil {
		log.Errorf("Failed to create auth session: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		MaxAge:   int(authStore.ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"participant_identity": session.Identity,
		"room_id":              session.RoomID,
		"board_id":             session.BoardID,
		"expires_at":           session.ExpiresAt.Format(time.RFC3339),
	})
}

func deleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(authCookieName); err == nil {
		authStore.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
