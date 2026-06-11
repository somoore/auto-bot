package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const localAWSRefreshTimeout = 5 * time.Second

var (
	localAWSRefreshHTTPClient = &http.Client{Timeout: localAWSRefreshTimeout}
	localAWSRefreshMu         sync.Mutex
	localAWSRefreshLastStart  time.Time
)

type localAWSRefreshBrokerResponse struct {
	OK             bool   `json:"ok"`
	Started        bool   `json:"started,omitempty"`
	Running        bool   `json:"running,omitempty"`
	Message        string `json:"message,omitempty"`
	VoiceProvider  string `json:"voice_provider,omitempty"`
	VoiceModel     string `json:"voice_model,omitempty"`
	RequiresReload bool   `json:"requires_reload,omitempty"`
}

type localRuntimeRestartRequest struct {
	Reason        string `json:"reason,omitempty"`
	VoiceProvider string `json:"voice_provider,omitempty"`
	VoiceModel    string `json:"voice_model,omitempty"`
	RoomID        string `json:"room_id,omitempty"`
	BoardID       string `json:"board_id,omitempty"`
}

func localAWSCredentialRefreshHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if appEnvironment != "local" || !requestHostIsLocalhost(r) {
		http.NotFound(w, r)
		return
	}
	if activeVoiceProviderID() != voiceProviderNovaSonic {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok":      false,
			"message": "AWS credential refresh is only available when the active voice provider is AWS Nova Sonic.",
		})
		return
	}

	authCtx, ok := authorizeVoiceModelRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if meetingAccess != nil && meetingAccess.isActive() && !meetingAccess.isHost(authCtx) {
		http.Error(w, "meeting host access is required", http.StatusForbidden)
		return
	}

	refreshURL := strings.TrimSpace(os.Getenv("APP_LOCAL_AWS_REFRESH_URL"))
	refreshToken := strings.TrimSpace(os.Getenv("APP_LOCAL_AWS_REFRESH_TOKEN"))
	if refreshURL == "" || refreshToken == "" {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"ok":      false,
			"message": "Local AWS refresh broker is not configured. Start the app with scripts/local-up.sh, then click Join Room again.",
		})
		return
	}
	if err := validateLocalAWSRefreshURL(refreshURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}

	localAWSRefreshMu.Lock()
	if time.Since(localAWSRefreshLastStart) < 20*time.Second {
		localAWSRefreshMu.Unlock()
		writeJSON(w, http.StatusAccepted, localAWSRefreshBrokerResponse{
			OK:      true,
			Running: true,
			Message: "AWS credential refresh is already in progress.",
		})
		return
	}
	localAWSRefreshLastStart = time.Now()
	localAWSRefreshMu.Unlock()

	brokerResponse, err := requestLocalRuntimeRestart(r.Context(), refreshURL, refreshToken, localRuntimeRestartRequest{
		Reason:        "aws_credentials_expired",
		VoiceProvider: voiceProviderNovaSonic,
		VoiceModel:    selectedNovaSonicModel(),
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"message": scrubStatusError(err),
		})
		return
	}
	status := http.StatusAccepted
	if !brokerResponse.OK {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, brokerResponse)
}

func validateLocalAWSRefreshURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid local AWS refresh URL")
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("local AWS refresh URL must use http")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("local AWS refresh URL must include a host")
	}
	if strings.EqualFold(host, "host.docker.internal") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("local AWS refresh URL must target localhost or host.docker.internal")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("local AWS refresh URL must target localhost or host.docker.internal")
}

func requestLocalRuntimeRestart(ctx context.Context, refreshURL string, refreshToken string, restart localRuntimeRestartRequest) (brokerResponse localAWSRefreshBrokerResponse, err error) {
	payload, err := json.Marshal(restart)
	if err != nil {
		return localAWSRefreshBrokerResponse{}, err
	}
	body := bytes.NewBuffer(payload)
	// #nosec G107,G704 -- local-only endpoint validates refreshURL as localhost or host.docker.internal before this request.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, body)
	if err != nil {
		return localAWSRefreshBrokerResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+refreshToken)

	resp, err := localAWSRefreshHTTPClient.Do(req) // #nosec G107,G704 -- request target is constrained by validateLocalAWSRefreshURL.
	if err != nil {
		return localAWSRefreshBrokerResponse{}, fmt.Errorf("request local AWS refresh broker: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close local AWS refresh broker response body: %w", closeErr)
		}
	}()

	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4096))
	if err := decoder.Decode(&brokerResponse); err != nil {
		return localAWSRefreshBrokerResponse{}, fmt.Errorf("decode local AWS refresh broker response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if brokerResponse.Message == "" {
			brokerResponse.Message = fmt.Sprintf("local AWS refresh broker returned %d", resp.StatusCode)
		}
		return brokerResponse, fmt.Errorf("%s", brokerResponse.Message)
	}
	return brokerResponse, nil
}
