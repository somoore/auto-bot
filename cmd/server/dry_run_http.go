package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// pendingActionsHandler serves GET /tenant/pending_actions returning the
// current dry-run queue for the request's tenant + board pair. Terminal rows
// are excluded by default; include_terminal=true returns the audit-log view.
func pendingActionsHandler(w http.ResponseWriter, r *http.Request) {
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
	store := globalPendingActionStore()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "pending action store not configured"})
		return
	}
	tenantID, boardID := resolveTenantBoardFromRequest(r)
	includeTerminal := r.URL.Query().Get("include_terminal") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	actions, err := store.ListPendingActions(ctx, tenantID, boardID, includeTerminal, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

// pendingActionDecisionHandler accepts POST /tenant/pending_actions/{id}/approve
// or /reject and resolves the action accordingly.
func pendingActionDecisionHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authCtx, ok := authorizeBaseRequest(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Path: /tenant/pending_actions/{id}/{approve|reject}
	path := strings.TrimPrefix(r.URL.Path, "/tenant/pending_actions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		http.Error(w, "expected /tenant/pending_actions/{id}/{approve|reject}", http.StatusBadRequest)
		return
	}
	actionID, verb := parts[0], parts[1]
	if actionID == "" || (verb != "approve" && verb != "reject" && verb != "preview") {
		http.Error(w, "invalid action verb", http.StatusBadRequest)
		return
	}

	board := sharedBoard
	if board == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "board not initialized"})
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	switch verb {
	case "approve":
		action, result, _, err := board.approvePendingAction(ctx, actionID, authCtx.Identity, "ui", body.Note)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrPendingActionNotFound) {
				status = http.StatusNotFound
			} else if errors.Is(err, ErrPendingActionTerminal) {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"error": err.Error(), "action": action})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": action, "result": result})
	case "reject":
		action, err := board.rejectPendingAction(ctx, actionID, authCtx.Identity, "ui", body.Note)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrPendingActionNotFound) {
				status = http.StatusNotFound
			} else if errors.Is(err, ErrPendingActionTerminal) {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"error": err.Error(), "action": action})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": action})
	case "preview":
		diff, err := board.PreviewPendingAction(ctx, actionID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrPendingActionNotFound) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, diff)
	}
}

// tenantSettingsHandler serves GET /tenant/settings and POST /tenant/settings
// (and the two convenience toggles below). The POST body is the tenantSettings
// JSON shape; the response is the freshly-persisted record.
func tenantSettingsHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if _, ok := authorizeBaseRequest(r); !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mgr := globalTenantSettingsManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "tenant settings manager not configured"})
		return
	}
	tenantID, _ := resolveTenantBoardFromRequest(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, mgr.Get(ctx, tenantID))
	case http.MethodPost:
		var body tenantSettings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("decode body: %v", err), http.StatusBadRequest)
			return
		}
		body.TenantID = tenantID
		previous := mgr.Get(ctx, tenantID)
		updated, err := mgr.Set(ctx, body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		// Sprint 4.0: when AgentsPaused transitions from false -> true,
		// fire the kill-switch fanout so in-flight runs and queued
		// dispatchers see the change. From true -> false, resume queued
		// runs. The helper is no-op if the orchestrator is unset.
		if previous.AgentsPaused != updated.AgentsPaused {
			handleAgentsPausedTransition(ctx, tenantID, updated.AgentsPaused)
		}
		broadcastKanbanEventForBoard(tenantID, defaultAppBoardID, "tenant_settings", updated)
		writeJSON(w, http.StatusOK, updated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAgentsPausedTransition is implemented in pause_all.go as part of
// Sprint 4.0 commit 4. The stub here exists so commit 2 (this commit) can
// reference it without a forward dep; the override below is a no-op until
// the kill-switch lands.
var handleAgentsPausedTransition = func(ctx context.Context, tenantID string, paused bool) {
	// Default no-op; overridden by pause_all.go once that file lands.
	_ = ctx
	_ = tenantID
	_ = paused
}

// resolveTenantBoardFromRequest returns the tenant/board pair the request is
// scoped to. Today auto-bot runs as a single tenant, so we return the default
// pair; query overrides are honored for multi-tenant testing.
func resolveTenantBoardFromRequest(r *http.Request) (string, string) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	boardID := strings.TrimSpace(r.URL.Query().Get("board_id"))
	if tenantID == "" {
		tenantID = defaultTenantID
	}
	if boardID == "" {
		boardID = defaultAppBoardID
	}
	return tenantID, boardID
}
