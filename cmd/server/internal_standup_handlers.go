package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// internalStandupAgendaHandler serves the live standup agenda assembled by
// internal/standup.AgendaBuilder. The handler is the HTTP face of the same
// builder the cron scheduler + voice agent share, so the React drawer and
// the meeting agent read the same shape. Gated by APP_API_TOKEN via
// authorizeBaseRequest like the rest of /internal/*.
//
// GET /internal/standup/agenda  →  internal/standup.Agenda JSON
// Optional `since_seconds` query param overrides the 24h default window.
func internalStandupAgendaHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if sharedBoard == nil {
		writeDispatchError(w, http.StatusServiceUnavailable, "board not initialized")
		return
	}
	builder := agendaBuilderFor(sharedBoard)
	if builder == nil {
		writeDispatchError(w, http.StatusServiceUnavailable, "agenda builder unavailable")
		return
	}

	since := time.Duration(0) // 0 → DefaultWindow (24h)
	if raw := strings.TrimSpace(r.URL.Query().Get("since_seconds")); raw != "" {
		var seconds int
		if _, err := fmt.Sscanf(raw, "%d", &seconds); err == nil && seconds > 0 {
			since = time.Duration(seconds) * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	agenda, err := builder.BuildAgenda(ctx, sharedBoard.tenantID, sharedBoard.boardID, since)
	if err != nil {
		log.Errorf("BuildAgenda failed: %v", err)
		writeDispatchError(w, http.StatusInternalServerError, "failed to build agenda")
		return
	}
	writeJSON(w, http.StatusOK, agenda)
}

// meetingReportByIDHandler serves a single archived meetingIntelligenceReport
// by id. /meetings/{id} mirrors the loadMeetingReportFromStore path that
// /meeting/intelligence?meeting_id=... uses, but exposes a REST-shaped URL
// the React PostMeetingSummary can fetch directly. The list shape lives at
// /meetings (meetingsListHandler) — overloading that handler would conflate
// the two response shapes, so this is a separate route.
//
// GET /meetings/{id}            → meetingIntelligenceReport JSON
// GET /meetings/{id}?board_id=… → same, scoped to a non-default board.
func meetingReportByIDHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authCtx, ok := authorizeBaseRequest(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	meetingID := strings.TrimPrefix(r.URL.Path, "/meetings/")
	meetingID = strings.TrimSpace(strings.Trim(meetingID, "/"))
	if meetingID == "" {
		writeDispatchError(w, http.StatusBadRequest, "meeting_id is required")
		return
	}
	boardID := normalizeRuntimeID(r.URL.Query().Get("board_id"), authCtx.BoardID)
	report, found, err := loadMeetingReportFromStore(boardID, meetingID)
	if err != nil {
		log.Errorf("Load meeting report %s failed: %v", meetingID, err)
		writeDispatchError(w, http.StatusInternalServerError, "failed to load meeting report")
		return
	}
	if !found {
		writeDispatchError(w, http.StatusNotFound, fmt.Sprintf("meeting report %s not found", meetingID))
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// devSeedRunQuestionHandler manufactures a Run that pauses on a RunQuestion
// so the D1.3 "Run waiting on human" UI state can be verified visually
// without AWS-driven Bedrock. Refuses to mount outside APP_ENV=local: a
// production caller sees a 404 (route not found semantics) so the seed path
// is invisible in deployed environments.
//
// POST /internal/dev/seed-run-question
//
//	{
//	  "card_id":           "card-002",
//	  "prompt":            "Should I rebase or merge?",
//	  "suggested_answers": ["rebase", "merge"],   // optional
//	  "agent_profile":     "swe-1"                // optional, defaults to "swe-1"
//	}
//
// Response: { run_id, question_id, card_id, status }
func devSeedRunQuestionHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if appEnvironment != "local" {
		// 404 (not 403) keeps the route invisible in non-local
		// deployments — there is no admission path that would let
		// this succeed outside dev so we mirror the behavior of an
		// unregistered handler.
		http.NotFound(w, r)
		return
	}
	if sharedBoard == nil {
		writeDispatchError(w, http.StatusServiceUnavailable, "board not initialized")
		return
	}
	store, ok := sharedBoard.store.(agent.RunStore)
	if !ok || store == nil {
		writeDispatchError(w, http.StatusServiceUnavailable, "board store does not implement agent.RunStore")
		return
	}

	var input struct {
		CardID           string   `json:"card_id"`
		Prompt           string   `json:"prompt"`
		SuggestedAnswers []string `json:"suggested_answers"`
		AgentProfile     string   `json:"agent_profile"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	cardID := strings.TrimSpace(input.CardID)
	prompt := strings.TrimSpace(input.Prompt)
	if cardID == "" {
		writeDispatchError(w, http.StatusBadRequest, "card_id is required")
		return
	}
	if prompt == "" {
		writeDispatchError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if _, found := lookupCardFromBoard(cardID); !found {
		writeDispatchError(w, http.StatusNotFound, fmt.Sprintf("unknown card_id: %s", cardID))
		return
	}
	profile := strings.TrimSpace(input.AgentProfile)
	if profile == "" {
		profile = "swe-1"
	}

	coord := agent.NewSimpleRunCoordinator(store, nil)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	run, err := coord.Start(ctx, agent.RunRequest{
		TenantID:     sharedBoard.tenantID,
		BoardID:      sharedBoard.boardID,
		CardID:       cardID,
		Objective:    fmt.Sprintf("D1.3 seed: %s", prompt),
		RequestedBy:  "dev-seed",
		AgentProfile: profile,
		RequestType:  "seed",
	})
	if err != nil {
		log.Errorf("dev seed: Start failed: %v", err)
		writeDispatchError(w, http.StatusInternalServerError, fmt.Sprintf("start run: %v", err))
		return
	}

	question := agent.RunQuestion{
		Prompt:      prompt,
		Suggestions: append([]string(nil), input.SuggestedAnswers...),
	}
	questionID, err := coord.AskHuman(ctx, run.RunID, question)
	if err != nil {
		log.Errorf("dev seed: AskHuman failed: %v", err)
		writeDispatchError(w, http.StatusInternalServerError, fmt.Sprintf("ask human: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":      run.RunID,
		"question_id": questionID,
		"card_id":     cardID,
		"status":      string(agent.StatusWaitingOnHuman),
	})
}
