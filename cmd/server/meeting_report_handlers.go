package main

import (
	"net/http"
	"os"
	"strconv"
)

func postMeetingPageHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := os.ReadFile("web/post_meeting.html")
	if err != nil {
		http.Error(w, "post-meeting page not available", http.StatusInternalServerError)
		log.Errorf("Failed to read post-meeting page: %v", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(raw)
}

func meetingIntelligenceHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authCtx, ok := authorizeRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if sharedBoard == nil {
		http.Error(w, "board not initialized", http.StatusServiceUnavailable)
		return
	}

	meetingID := r.URL.Query().Get("meeting_id")
	boardID := normalizeRuntimeID(r.URL.Query().Get("board_id"), authCtx.BoardID)
	if meetingID != "" {
		report, found, err := loadMeetingReportFromStore(boardID, meetingID)
		if err != nil {
			http.Error(w, "failed to load meeting report", http.StatusInternalServerError)
			log.Errorf("Load meeting report failed: %v", err)
			return
		}
		if !found {
			http.Error(w, "meeting report not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, report)
		return
	}

	writeJSON(w, http.StatusOK, sharedBoard.BuildMeetingIntelligenceReport("api"))
}

func meetingsListHandler(w http.ResponseWriter, r *http.Request) {
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
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	boardID := normalizeRuntimeID(r.URL.Query().Get("board_id"), authCtx.BoardID)
	reports, err := listMeetingReportsFromStore(boardID, limit)
	if err != nil {
		http.Error(w, "failed to list meeting reports", http.StatusInternalServerError)
		log.Errorf("List meeting reports failed: %v", err)
		return
	}
	current := meetingReportSummary{}
	if sharedBoard != nil {
		current = sharedBoard.BuildMeetingIntelligenceReport("api").SummaryView()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"reports": reports,
		"current": current,
	})
}

func setupStatusHandler(w http.ResponseWriter, r *http.Request) {
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
	status := buildSetupReadinessReport()
	status.Metadata["identity"] = authCtx.Identity
	status.Metadata["role"] = authCtx.Role
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "setup": status})
}

func observabilityStatusHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sequence := int64(0)
	if sharedBoard != nil {
		sequence = sharedBoard.SnapshotState().SequenceNumber
	}
	access := meetingAccessSnapshot{}
	if meetingAccess != nil {
		access = meetingAccess.snapshot(false)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"observability": buildMeetingObservability(sequence, access),
	})
}

func voiceProvidersHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"active":    firstNonEmpty(voiceProvider, "openai"),
		"providers": voiceProviderOptions(),
	})
}

func identityStatusHandler(w http.ResponseWriter, r *http.Request) {
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
	authorizedCtx, meetingOK := meetingAccess.authorize(authCtx)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"identity":       authCtx.Identity,
		"role":           authorizedCtx.Role,
		"meeting_access": meetingOK,
		"provider":       identityProviderMode(),
		"permissions": map[string]bool{
			"can_host_meeting":    meetingAccess.isHost(authorizedCtx),
			"can_switch_type":     meetingAccess.isHost(authorizedCtx),
			"can_confirm_actions": meetingAccess.isHost(authorizedCtx),
			"can_view_reports":    true,
		},
	})
}
