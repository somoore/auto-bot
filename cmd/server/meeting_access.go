package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	meetingRoleHost        = "host"
	meetingRoleParticipant = "participant"

	meetingTypeGeneral      = "general"
	meetingTypeStandup      = "standup"
	meetingTypeOneOnOne     = "one_on_one"
	meetingTypeSprintReview = "sprint_review"
	meetingTypeOpenEnded    = "open_ended"

	joinCodeLength = 16
	joinCodeChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

var (
	errMeetingNotActive = errors.New("meeting is not active")
	errInvalidJoinCode  = errors.New("invalid meeting join code")
	errHostRequired     = errors.New("meeting host access is required")

	meetingAccess = newMeetingAccessManager()
)

type meetingAccessManager struct {
	mu           sync.Mutex
	active       bool
	meetingID    string
	meetingType  string
	joinCode     string
	roomID       string
	boardID      string
	hostIdentity string
	createdAt    time.Time
	updatedAt    time.Time
	sessions     map[string]meetingAccessSession
}

type meetingAccessSession struct {
	SessionID string `json:"-"`
	Identity  string `json:"identity"`
	Role      string `json:"role"`
	JoinedAt  string `json:"joined_at"`
}

type meetingAccessSnapshot struct {
	Active       bool                   `json:"active"`
	MeetingID    string                 `json:"meeting_id,omitempty"`
	MeetingType  string                 `json:"meeting_type,omitempty"`
	JoinCode     string                 `json:"join_code,omitempty"`
	RoomID       string                 `json:"room_id,omitempty"`
	BoardID      string                 `json:"board_id,omitempty"`
	HostIdentity string                 `json:"host_identity,omitempty"`
	CreatedAt    string                 `json:"created_at,omitempty"`
	UpdatedAt    string                 `json:"updated_at,omitempty"`
	Participants []meetingAccessSession `json:"participants,omitempty"`
}

type meetingLeaveResult struct {
	Snapshot          meetingAccessSnapshot
	Left              bool
	Ended             bool
	Role              string
	Identity          string
	RevokedSessionIDs []string
}

type setupMeetingRequest struct {
	MeetingType  string `json:"meeting_type"`
	Role         string `json:"role,omitempty"`
	HostIdentity string `json:"host_identity,omitempty"`
	BoardID      string `json:"board_id,omitempty"`
}

type joinMeetingRequest struct {
	JoinCode            string `json:"join_code"`
	MeetingCode         string `json:"meeting_code,omitempty"`
	Code                string `json:"code,omitempty"`
	Identity            string `json:"identity"`
	ParticipantIdentity string `json:"participant_identity,omitempty"`
	Role                string `json:"role,omitempty"`
	BoardID             string `json:"board_id,omitempty"`
}

type switchMeetingTypeRequest struct {
	MeetingType string `json:"meeting_type"`
	MeetingCode string `json:"meeting_code,omitempty"`
	RoomID      string `json:"room_id,omitempty"`
}

func newMeetingAccessManager() *meetingAccessManager {
	return &meetingAccessManager{
		sessions: map[string]meetingAccessSession{},
	}
}

func setupMeetingHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if !enforceCSRF(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authCtx, ok := authorizeBaseRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if meetingAccess.isActive() && !meetingAccess.isHost(authCtx) {
		http.Error(w, errHostRequired.Error(), http.StatusForbidden)
		return
	}

	var req setupMeetingRequest
	if err := decodeSmallJSON(w, r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	meetingType, err := normalizeMeetingType(req.MeetingType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := meetingAccess.setup(authCtx, meetingType)
	if err != nil {
		log.Errorf("Failed to setup meeting access: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	applyMeetingTypeToBoard(meetingType, "meeting-setup")
	broadcastKanbanEvent("meeting_access", snapshot.withoutJoinCode())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"meeting":       snapshot,
		"code":          snapshot.JoinCode,
		"join_code":     snapshot.JoinCode,
		"meeting_code":  snapshot.JoinCode,
		"room_id":       snapshot.RoomID,
		"board_id":      snapshot.BoardID,
		"meeting_id":    snapshot.MeetingID,
		"meeting_type":  snapshot.MeetingType,
		"host_identity": snapshot.HostIdentity,
		"created_at":    snapshot.CreatedAt,
	})
}

func joinMeetingHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if !enforceCSRF(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !joinMeetingLimiter.Allow(clientAddress(r)) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var req joinMeetingRequest
	if err := decodeSmallJSON(w, r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	identity := normalizeParticipantIdentity(firstNonEmpty(req.Identity, req.ParticipantIdentity))
	if identity == "" {
		http.Error(w, "invalid identity: must be 1-64 alphanumeric/dash/underscore characters", http.StatusBadRequest)
		return
	}
	joinCode := firstNonEmpty(req.JoinCode, req.MeetingCode, req.Code)
	if err := meetingAccess.validateJoinCode(joinCode); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, errMeetingNotActive) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	session, err := authStore.create(identity)
	if err != nil {
		log.Errorf("Failed to create meeting participant session: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	snapshot, err := meetingAccess.joinSessionWithCode(session, joinCode, meetingRoleParticipant)
	if err != nil {
		authStore.delete(session.ID)
		status := http.StatusForbidden
		if errors.Is(err, errMeetingNotActive) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}

	setSessionCookie(w, r, session)
	broadcastKanbanEvent("meeting_access", snapshot.withoutJoinCode())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"participant_identity": identity,
		"role":                 meetingRoleParticipant,
		"room_id":              session.RoomID,
		"board_id":             session.BoardID,
		"meeting_id":           snapshot.MeetingID,
		"meeting_type":         snapshot.MeetingType,
		"joined_at":            time.Now().UTC().Format(time.RFC3339),
		"expires_at":           sessionResponseExpiresAt(session),
	})
}

func leaveMeetingHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if !enforceCSRF(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authCtx, ok := authorizeRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var result meetingLeaveResult
	var err error
	if authCtx.SessionID != "" {
		result, err = meetingAccess.leaveSession(authCtx.SessionID)
	} else {
		// Stateless ALB OIDC caller: key the leave on the authenticated identity.
		// leaveByIdentity treats the caller as host when their identity matches
		// the meeting's recorded hostIdentity (the meeting creator), so host-vs-
		// participant leave semantics work without a session cookie or a
		// pre-assigned role.
		result, err = meetingAccess.leaveByIdentity(authCtx.Identity, authCtx.Role == meetingRoleHost)
	}
	if err != nil {
		if errors.Is(err, errMeetingNotActive) {
			snapshot := meetingAccess.snapshot(false)
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":       true,
				"left":     false,
				"ended":    true,
				"identity": authCtx.Identity,
				"meeting":  snapshot.withoutJoinCode(),
			})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, sessionID := range result.RevokedSessionIDs {
		authStore.delete(sessionID)
	}
	if result.Role == meetingRoleParticipant {
		clearSessionCookie(w, r)
	}
	if result.Ended {
		applyMeetingEndedToBoard("meeting-host-left")
		// Tear down the LiveKit room/agent so it doesn't keep running after the
		// host leaves (otherwise a "new" meeting rejoins the old room with the
		// agent still talking).
		if novaSonic != nil {
			novaSonic.LeaveConferenceRoom("meeting ended by host")
		}
	}
	broadcastKanbanEvent("meeting_access", result.Snapshot.withoutJoinCode())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"left":     result.Left,
		"ended":    result.Ended,
		"role":     result.Role,
		"identity": result.Identity,
		"meeting":  result.Snapshot.withoutJoinCode(),
	})
}

func meetingStatusHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authCtx, ok := authorizeBaseRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	authorizedCtx, accessOK := meetingAccess.authorize(authCtx)
	snapshot := meetingAccess.snapshot(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"access_granted": accessOK,
		"role":           authorizedCtx.Role,
		"meeting_id":     authorizedCtx.MeetingID,
		"meeting_type":   authorizedCtx.MeetingType,
		"meeting":        snapshot,
		"agent_runs":     recentAgentRunViews(sharedBoard, 20),
	})
}

func switchMeetingTypeHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if !enforceCSRF(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authCtx, ok := authorizeRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !meetingAccess.isHost(authCtx) {
		http.Error(w, errHostRequired.Error(), http.StatusForbidden)
		return
	}

	var req switchMeetingTypeRequest
	if err := decodeSmallJSON(w, r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	meetingType, err := normalizeMeetingType(req.MeetingType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := meetingAccess.switchType(meetingType)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMeetingNotActive) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	applyMeetingTypeToBoard(meetingType, "meeting-type")
	broadcastKanbanEvent("meeting_access", snapshot.withoutJoinCode())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"meeting":      snapshot.withoutJoinCode(),
		"room_id":      snapshot.RoomID,
		"board_id":     snapshot.BoardID,
		"meeting_id":   snapshot.MeetingID,
		"meeting_type": snapshot.MeetingType,
	})
}

func decodeSmallJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain exactly one JSON value")
	}
	return nil
}

func (m *meetingAccessManager) setup(ctx requestAuthContext, meetingType string) (meetingAccessSnapshot, error) {
	code, err := randomJoinCode()
	if err != nil {
		return meetingAccessSnapshot{}, err
	}
	now := time.Now().UTC()
	meetingID := fmt.Sprintf("%s-%s", meetingType, now.Format("20060102T150405Z"))

	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = true
	m.meetingID = meetingID
	m.meetingType = meetingType
	m.joinCode = code
	m.roomID = appRoomID
	m.boardID = appBoardID
	m.hostIdentity = ctx.Identity
	m.createdAt = now
	m.updatedAt = now
	m.sessions = map[string]meetingAccessSession{}
	// Record the host session. Under ALB OIDC auth there is no SessionID
	// (identity is per-request and stateless), so key the host session by
	// identity instead — otherwise the host session map stays empty and the
	// voice host-access gate falls through to "not host". Safe post-C1: the
	// identity is server-minted, not client-supplied.
	hostKey := ctx.SessionID
	if hostKey == "" {
		hostKey = "identity:" + ctx.Identity
	}
	m.sessions[hostKey] = meetingAccessSession{
		SessionID: ctx.SessionID,
		Identity:  ctx.Identity,
		Role:      meetingRoleHost,
		JoinedAt:  now.Format(time.RFC3339),
	}
	return m.snapshotLocked(true), nil
}

func (m *meetingAccessManager) validateJoinCode(rawCode string) error {
	code := normalizeJoinCode(rawCode)
	if code == "" {
		return errInvalidJoinCode
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return errMeetingNotActive
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(m.joinCode)) != 1 {
		return errInvalidJoinCode
	}
	return nil
}

func (m *meetingAccessManager) joinSession(session webSession, role string) (meetingAccessSnapshot, error) {
	return m.joinSessionLocked(session, role, "")
}

func (m *meetingAccessManager) joinSessionWithCode(session webSession, rawCode string, role string) (meetingAccessSnapshot, error) {
	return m.joinSessionLocked(session, role, normalizeJoinCode(rawCode))
}

func (m *meetingAccessManager) joinSessionLocked(session webSession, role string, requiredCode string) (meetingAccessSnapshot, error) {
	if role == "" {
		role = meetingRoleParticipant
	}
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return meetingAccessSnapshot{}, errMeetingNotActive
	}
	if requiredCode != "" && subtle.ConstantTimeCompare([]byte(requiredCode), []byte(m.joinCode)) != 1 {
		return meetingAccessSnapshot{}, errInvalidJoinCode
	}
	if requiredCode == "" && role == meetingRoleParticipant {
		return meetingAccessSnapshot{}, errInvalidJoinCode
	}
	m.sessions[session.ID] = meetingAccessSession{
		SessionID: session.ID,
		Identity:  session.Identity,
		Role:      role,
		JoinedAt:  now.Format(time.RFC3339),
	}
	m.updatedAt = now
	return m.snapshotLocked(false), nil
}

func (m *meetingAccessManager) authorize(ctx requestAuthContext) (requestAuthContext, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return ctx, true
	}
	ctx.MeetingID = m.meetingID
	ctx.MeetingType = m.meetingType

	// Under ALB OIDC auth the request is sessionless: role is NOT carried on the
	// context, so derive it from whether this identity is the meeting's recorded
	// host (the creator). Everyone else is a participant. Identity is server-
	// minted (post-C1), so this is safe.
	if albAuthEnabled && ctx.SessionID == "" {
		if identityEqual(ctx.Identity, m.hostIdentity) {
			ctx.Role = meetingRoleHost
		} else {
			ctx.Role = meetingRoleParticipant
		}
		return ctx, true
	}

	if appAuthMode == "disabled" || ctx.SessionID == "" {
		ctx.Role = meetingRoleHost
		return ctx, true
	}

	session, ok := m.sessions[ctx.SessionID]
	if !ok {
		return requestAuthContext{}, false
	}
	ctx.Role = session.Role
	return ctx, true
}

func (m *meetingAccessManager) isHost(ctx requestAuthContext) bool {
	// Under ALB OIDC auth the request is sessionless; host is the identity that
	// created the meeting (recorded as m.hostIdentity), not a pre-assigned role.
	if albAuthEnabled && ctx.SessionID == "" {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.active && identityEqual(ctx.Identity, m.hostIdentity)
	}
	if appAuthMode == "disabled" || ctx.SessionID == "" || ctx.Role == meetingRoleHost {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[ctx.SessionID]
	return ok && session.Role == meetingRoleHost
}

func (m *meetingAccessManager) voiceSpeakerHasHostAccess(speakerLabel string) bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if appAuthMode == "disabled" || !m.active {
		return true
	}

	speakers := speakerIdentitiesFromLabel(speakerLabel)
	var ok bool
	switch len(speakers) {
	case 1:
		ok = m.identityIsHostLocked(speakers[0])
	case 0:
		ok = m.onlyHostSessionLocked()
	default:
		ok = false
	}
	if !ok {
		log.Errorf("voice host gate denied: speakerLabel=%q hostIdentity=%q sessions=%d", speakerLabel, m.hostIdentity, len(m.sessions))
	}
	return ok
}

func (m *meetingAccessManager) identityIsHostLocked(identity string) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false
	}
	if identityEqual(identity, m.hostIdentity) {
		return true
	}
	for _, session := range m.sessions {
		if session.Role == meetingRoleHost && identityEqual(identity, session.Identity) {
			return true
		}
	}
	return false
}

func (m *meetingAccessManager) onlyHostSessionLocked() bool {
	if len(m.sessions) == 0 {
		return false
	}
	for _, session := range m.sessions {
		if session.Role != meetingRoleHost {
			return false
		}
	}
	return true
}

func speakerIdentitiesFromLabel(label string) []string {
	seen := map[string]string{}
	for _, part := range strings.Split(label, ",") {
		identity := normalizeParticipantIdentity(strings.TrimSpace(part))
		if identity == "" {
			continue
		}
		key := strings.ToLower(identity)
		if _, ok := seen[key]; !ok {
			seen[key] = identity
		}
	}
	out := make([]string, 0, len(seen))
	for _, identity := range seen {
		out = append(out, identity)
	}
	return out
}

func identityEqual(left string, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func (m *meetingAccessManager) isActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *meetingAccessManager) switchType(meetingType string) (meetingAccessSnapshot, error) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return meetingAccessSnapshot{}, errMeetingNotActive
	}
	m.meetingType = meetingType
	m.updatedAt = now
	return m.snapshotLocked(false), nil
}

func (m *meetingAccessManager) leaveSession(sessionID string) (meetingLeaveResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return meetingLeaveResult{Snapshot: m.snapshotLocked(false)}, errMeetingNotActive
	}
	session, ok := m.sessions[sessionID]
	if !ok {
		return meetingLeaveResult{Snapshot: m.snapshotLocked(false)}, errInvalidJoinCode
	}

	result := meetingLeaveResult{
		Snapshot: m.snapshotLocked(false),
		Left:     true,
		Role:     session.Role,
		Identity: session.Identity,
	}
	if session.Role == meetingRoleHost {
		result.Ended = true
		for id, existing := range m.sessions {
			if existing.Role == meetingRoleParticipant {
				result.RevokedSessionIDs = append(result.RevokedSessionIDs, id)
			}
		}
		m.active = false
		m.joinCode = ""
		m.sessions = map[string]meetingAccessSession{}
		m.updatedAt = now
		result.Snapshot = m.snapshotLocked(false)
		return result, nil
	}

	delete(m.sessions, sessionID)
	result.RevokedSessionIDs = append(result.RevokedSessionIDs, sessionID)
	m.updatedAt = now
	result.Snapshot = m.snapshotLocked(false)
	return result, nil
}

// leaveByIdentity ends/leaves a meeting for a stateless (ALB OIDC) caller that
// has no SessionID. If the identity is the host, the meeting is ended and all
// participant sessions are revoked; otherwise any participant sessions matching
// the identity are removed.
func (m *meetingAccessManager) leaveByIdentity(identity string, isHost bool) (meetingLeaveResult, error) {
	identity = strings.TrimSpace(identity)
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return meetingLeaveResult{Snapshot: m.snapshotLocked(false)}, errMeetingNotActive
	}

	if isHost || identityEqual(identity, m.hostIdentity) {
		result := meetingLeaveResult{
			Left:     true,
			Ended:    true,
			Role:     meetingRoleHost,
			Identity: identity,
		}
		for id, existing := range m.sessions {
			if existing.Role == meetingRoleParticipant && existing.SessionID != "" {
				result.RevokedSessionIDs = append(result.RevokedSessionIDs, id)
			}
		}
		m.active = false
		m.joinCode = ""
		m.sessions = map[string]meetingAccessSession{}
		m.hostIdentity = ""
		m.updatedAt = now
		result.Snapshot = m.snapshotLocked(false)
		return result, nil
	}

	// Participant: drop any sessions for this identity.
	result := meetingLeaveResult{
		Left:     true,
		Role:     meetingRoleParticipant,
		Identity: identity,
	}
	for id, existing := range m.sessions {
		if existing.Role == meetingRoleParticipant && identityEqual(existing.Identity, identity) {
			delete(m.sessions, id)
			if existing.SessionID != "" {
				result.RevokedSessionIDs = append(result.RevokedSessionIDs, id)
			}
		}
	}
	m.updatedAt = now
	result.Snapshot = m.snapshotLocked(false)
	return result, nil
}

func (m *meetingAccessManager) snapshot(includeJoinCode bool) meetingAccessSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(includeJoinCode)
}

func (m *meetingAccessManager) snapshotLocked(includeJoinCode bool) meetingAccessSnapshot {
	snapshot := meetingAccessSnapshot{
		Active:       m.active,
		MeetingID:    m.meetingID,
		MeetingType:  m.meetingType,
		RoomID:       m.roomID,
		BoardID:      m.boardID,
		HostIdentity: m.hostIdentity,
		Participants: make([]meetingAccessSession, 0, len(m.sessions)),
	}
	if includeJoinCode {
		snapshot.JoinCode = m.joinCode
	}
	if !m.createdAt.IsZero() {
		snapshot.CreatedAt = m.createdAt.Format(time.RFC3339)
	}
	if !m.updatedAt.IsZero() {
		snapshot.UpdatedAt = m.updatedAt.Format(time.RFC3339)
	}
	for _, session := range m.sessions {
		session.SessionID = ""
		snapshot.Participants = append(snapshot.Participants, session)
	}
	return snapshot
}

func (snapshot meetingAccessSnapshot) withoutJoinCode() meetingAccessSnapshot {
	snapshot.JoinCode = ""
	return snapshot
}

func randomJoinCode() (string, error) {
	var builder strings.Builder
	builder.Grow(joinCodeLength)
	max := big.NewInt(int64(len(joinCodeChars)))
	for builder.Len() < joinCodeLength {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		builder.WriteByte(joinCodeChars[index.Int64()])
	}
	return builder.String(), nil
}

func normalizeJoinCode(code string) string {
	var builder strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func normalizeMeetingType(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "", "general", "general_meeting":
		return meetingTypeGeneral, nil
	case "standup", "daily", "daily_standup", "scrum":
		return meetingTypeStandup, nil
	case "1:1", "one_on_one", "one_to_one", "oneonone", "one_one":
		return meetingTypeOneOnOne, nil
	case "sprint_review", "review":
		return meetingTypeSprintReview, nil
	case "open_ended", "openended", "open":
		return meetingTypeOpenEnded, nil
	default:
		return "", fmt.Errorf("meeting_type must be one of: general, standup, one_on_one, sprint_review, open_ended")
	}
}

func meetingTypeToScrumMode(meetingType string) scrumMeetingMode {
	switch meetingType {
	case meetingTypeGeneral:
		return scrumMeetingModeGeneral
	case meetingTypeOneOnOne:
		return scrumMeetingModeOneOnOne
	case meetingTypeSprintReview:
		return scrumMeetingModeReview
	case meetingTypeOpenEnded:
		return scrumMeetingModeOpenEnded
	default:
		return scrumMeetingModeStandup
	}
}

func (board *kanbanBoard) switchMeetingType(args map[string]any) (map[string]any, bool, error) {
	meetingType, err := normalizeMeetingType(firstNonEmptyString(args, "meeting_type", "mode", "type"))
	if err != nil {
		return nil, false, err
	}
	mode := meetingTypeToScrumMode(meetingType)
	now := time.Now().UTC()

	board.mu.Lock()
	previousMode := board.meeting.Mode
	changed := previousMode != mode || !board.meeting.Active || board.meeting.MeetingID == "" || board.meeting.StartedAt == ""
	if board.meeting.MeetingID == "" {
		board.meeting.MeetingID = fmt.Sprintf("%s-%s", meetingType, now.Format("20060102T150405Z"))
	}
	board.meeting.Mode = mode
	board.meeting.Active = true
	if board.meeting.StartedAt == "" {
		board.meeting.StartedAt = now.Format(time.RFC3339)
	}
	if changed {
		board.touchLocked()
	}
	meeting := cloneScrumMeetingState(board.meeting)
	board.mu.Unlock()

	if meetingAccess != nil && meetingAccess.isActive() {
		if _, switchErr := meetingAccess.switchType(meetingType); switchErr != nil && !errors.Is(switchErr, errMeetingNotActive) {
			return nil, false, switchErr
		}
	}

	return map[string]any{
		"ok":                    true,
		"meeting_type":          meetingType,
		"mode":                  mode,
		"previous_mode":         previousMode,
		"meeting":               meeting,
		"facilitation_guidance": meetingTypeGuidance(meetingType),
	}, changed, nil
}

func meetingTypeGuidance(meetingType string) string {
	switch meetingType {
	case meetingTypeGeneral:
		return "Facilitate a balanced discussion, capture decisions, risks, owners, and follow-ups."
	case meetingTypeOneOnOne:
		return "Keep the conversation focused on one participant, commitments, support needed, and follow-up ownership."
	case meetingTypeSprintReview:
		return "Drive demo flow, stakeholder feedback, accepted work, open questions, and follow-up actions."
	case meetingTypeOpenEnded:
		return "Keep a lightweight agenda, preserve useful context, and avoid forcing scrum ceremony."
	default:
		return "Run a concise standup: yesterday, today, blockers, owners, and next speaker."
	}
}

func applyMeetingTypeToBoard(meetingType string, source string) {
	if sharedBoard == nil {
		return
	}
	result, changed, err := sharedBoard.ApplyToolCallWithMeta("switch_meeting_type", mustMarshalJSON(map[string]any{
		"meeting_type": meetingType,
	}), toolCallMeta{Source: source})
	if err != nil {
		log.Errorf("Failed to apply meeting type %q to board: %v", meetingType, err)
		return
	}
	if !changed {
		return
	}
	state := sharedBoard.SnapshotState()
	auditBoardMutation(source, "switch_meeting_type", result, state)
	broadcastKanbanEvent("board", state)
}

func applyMeetingEndedToBoard(source string) {
	if sharedBoard == nil {
		return
	}
	result, changed, err := sharedBoard.ApplyToolCallWithMeta("end_meeting", mustMarshalJSON(map[string]any{
		"decision": "Host left the meeting; meeting ended.",
	}), toolCallMeta{Source: source})
	if err != nil {
		log.Errorf("Failed to end board meeting: %v", err)
		return
	}
	if !changed {
		return
	}
	state := sharedBoard.SnapshotState()
	auditBoardMutation(source, "end_meeting", result, state)
	broadcastKanbanEvent("board", state)
}
