package main

import (
	"context"
	"net/http"
	"os"
)

// workspaceScope is the JSON shape returned by /workspace/status. The current
// runtime is deployment-scoped; Metadata documents the future workspace split.
type workspaceScope struct {
	WorkspaceID      string               `json:"workspace_id"`
	BoardID          string               `json:"board_id"`
	RoomID           string               `json:"room_id"`
	Region           string               `json:"region"`
	Identity         string               `json:"identity,omitempty"`
	Role             string               `json:"role,omitempty"`
	MeetingID        string               `json:"meeting_id,omitempty"`
	MeetingType      string               `json:"meeting_type,omitempty"`
	AuthMode         string               `json:"auth_mode"`
	IdentityProvider string               `json:"identity_provider"`
	VoiceProvider    string               `json:"voice_provider"`
	ConnectorHealth  []workspaceConnector `json:"connector_health,omitempty"`
	Metadata         map[string]string    `json:"metadata,omitempty"`
}

type workspaceConnector struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
}

func currentWorkspaceScope(authCtx requestAuthContext) workspaceScope {
	scope := workspaceScope{
		WorkspaceID:      normalizeRuntimeID(os.Getenv("APP_WORKSPACE_ID"), "default"),
		BoardID:          normalizeRuntimeID(authCtx.BoardID, appBoardID),
		RoomID:           normalizeRuntimeID(authCtx.RoomID, appRoomID),
		Region:           getEnvDefault("AWS_REGION", "us-east-1"),
		Identity:         authCtx.Identity,
		Role:             authCtx.Role,
		MeetingID:        authCtx.MeetingID,
		MeetingType:      authCtx.MeetingType,
		AuthMode:         firstNonEmpty(authCtx.AuthMode, appAuthMode),
		IdentityProvider: identityProviderMode(),
		VoiceProvider:    firstNonEmpty(voiceProvider, "nova-sonic"),
		Metadata: map[string]string{
			"isolation_model": "single-workspace-local-or-deployment-scoped",
			"future_model":    "workspace-scoped rooms, boards, connector installs, and secrets",
		},
	}
	if extensions != nil && extensions.connectors != nil {
		for _, connector := range extensions.connectors.List() {
			health := connector.Health(context.Background())
			scope.ConnectorHealth = append(scope.ConnectorHealth, workspaceConnector{
				Name:    connector.Name(),
				Display: connector.DisplayName(),
				OK:      health.OK,
				Status:  health.Status,
			})
		}
	}
	return scope
}

func workspaceStatusHandler(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"workspace": currentWorkspaceScope(authCtx),
	})
}
