package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	voiceProviderOpenAI    = "openai"
	voiceProviderNovaSonic = "nova-sonic"
)

type runtimeVoiceModelSelection struct {
	mu     sync.RWMutex
	models map[string]string
}

var voiceModels = &runtimeVoiceModelSelection{models: map[string]string{}}

func (selection *runtimeVoiceModelSelection) get(provider string) string {
	selection.mu.RLock()
	defer selection.mu.RUnlock()
	return selection.models[normalizeVoiceProviderID(provider)]
}

func (selection *runtimeVoiceModelSelection) set(provider string, model string) {
	selection.mu.Lock()
	defer selection.mu.Unlock()
	selection.models[normalizeVoiceProviderID(provider)] = strings.TrimSpace(model)
}

type voiceModelOption struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	Label           string `json:"label"`
	Description     string `json:"description,omitempty"`
	Transport       string `json:"transport"`
	FullDuplex      bool   `json:"full_duplex"`
	ToolCalling     bool   `json:"tool_calling"`
	Reasoning       bool   `json:"reasoning,omitempty"`
	CostHint        string `json:"cost_hint,omitempty"`
	Current         bool   `json:"current"`
	Selectable      bool   `json:"selectable"`
	RequiresRestart bool   `json:"requires_restart"`
	DisabledReason  string `json:"disabled_reason,omitempty"`
}

// voiceModelStatus is the JSON payload for the active model, selectable model
// options, and restart-required provider changes.
type voiceModelStatus struct {
	OK                 bool               `json:"ok"`
	ActiveProvider     string             `json:"active_provider"`
	CurrentProvider    string             `json:"current_provider"`
	CurrentModel       string             `json:"current_model"`
	CurrentModelID     string             `json:"current_model_id"`
	CurrentLabel       string             `json:"current_label"`
	TranscriptionModel string             `json:"transcription_model,omitempty"`
	Options            []voiceModelOption `json:"options"`
	Message            string             `json:"message,omitempty"`
	RequiresRestart    bool               `json:"requires_restart,omitempty"`
	RestartStarted     bool               `json:"restart_started,omitempty"`
	RestartProvider    string             `json:"restart_provider,omitempty"`
	RestartModel       string             `json:"restart_model,omitempty"`
	StreamRestarted    bool               `json:"stream_restarted,omitempty"`
}

type updateVoiceModelRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	ModelID  string `json:"model_id,omitempty"`
	ID       string `json:"id,omitempty"`
}

func normalizeVoiceProviderID(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "", "openai-realtime", "openai":
		return voiceProviderOpenAI
	case "nova", "nova-sonic", "aws-nova-sonic", "aws":
		return voiceProviderNovaSonic
	default:
		return value
	}
}

func activeVoiceProviderID() string {
	return normalizeVoiceProviderID(firstNonEmpty(voiceProvider, voiceProviderOpenAI))
}

func selectedNovaSonicModel() string {
	if model := voiceModels.get(voiceProviderNovaSonic); model != "" {
		return model
	}
	return getEnvDefault("NOVA_SONIC_MODEL", "amazon.nova-2-sonic-v1:0")
}

func currentVoiceModelID(provider string) string {
	switch normalizeVoiceProviderID(provider) {
	case voiceProviderNovaSonic:
		if novaSonic != nil {
			return novaSonic.CurrentModelID()
		}
		return selectedNovaSonicModel()
	case voiceProviderOpenAI:
		return realtimeModel()
	default:
		return ""
	}
}

func voiceModelHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	switch r.Method {
	case http.MethodGet:
		if _, ok := authorizeVoiceModelRequest(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, buildVoiceModelStatus(""))
	case http.MethodPost:
		handleVoiceModelUpdate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func authorizeVoiceModelRequest(r *http.Request) (requestAuthContext, bool) {
	authCtx, ok := authorizeBaseRequest(r)
	if !ok {
		return requestAuthContext{}, false
	}
	if meetingAccess == nil || !meetingAccess.isActive() {
		return authCtx, true
	}
	return meetingAccess.authorize(authCtx)
}

func handleVoiceModelUpdate(w http.ResponseWriter, r *http.Request) {
	authCtx, ok := authorizeVoiceModelRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if meetingAccess != nil && meetingAccess.isActive() && !meetingAccess.isHost(authCtx) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"ok":      false,
			"message": "Only the meeting host can change the voice model.",
		})
		return
	}

	var req updateVoiceModelRequest
	if err := decodeSmallJSON(w, r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	option, err := resolveVoiceModelOption(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}
	if option.RequiresRestart {
		status, started, err := maybeStartLocalVoiceProviderRestart(r, option)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok":      false,
				"message": scrubStatusError(err),
			})
			return
		}
		if started {
			writeJSON(w, http.StatusAccepted, status)
			return
		}
		status = buildVoiceModelStatus(option.DisabledReason)
		status.OK = false
		status.RequiresRestart = true
		status.RestartProvider = option.Provider
		status.RestartModel = option.Model
		writeJSON(w, http.StatusConflict, status)
		return
	}
	if !option.Selectable {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": firstNonEmpty(option.DisabledReason, "This voice model is not selectable."),
		})
		return
	}

	var streamRestarted bool
	switch option.Provider {
	case voiceProviderNovaSonic:
		voiceModels.set(option.Provider, option.Model)
		if novaSonic != nil {
			streamRestarted = novaSonic.SetModel(option.Model)
		}
	case voiceProviderOpenAI:
		if kanbanApp != nil {
			status, started, err := maybeStartLocalVoiceProviderRestart(r, option)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"ok":      false,
					"message": scrubStatusError(err),
				})
				return
			}
			if started {
				writeJSON(w, http.StatusAccepted, status)
				return
			}
			status = buildVoiceModelStatus("OpenAI Realtime model changes require restarting the server because the WebRTC peer is created at process startup.")
			status.OK = false
			status.RequiresRestart = true
			status.RestartProvider = option.Provider
			status.RestartModel = option.Model
			writeJSON(w, http.StatusConflict, status)
			return
		}
		voiceModels.set(option.Provider, option.Model)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "Unsupported voice provider.",
		})
		return
	}

	status := buildVoiceModelStatus(fmt.Sprintf("Voice model set to %s.", option.Label))
	status.StreamRestarted = streamRestarted
	if streamRestarted {
		status.Message = fmt.Sprintf("Voice model set to %s. The active Bedrock stream is restarting on the new model.", option.Label)
	}
	broadcastKanbanEvent("voice_status", currentVoiceReadiness(context.Background(), false))
	broadcastKanbanEvent("status", status.Message)
	writeJSON(w, http.StatusOK, status)
}

func resolveVoiceModelOption(req updateVoiceModelRequest) (voiceModelOption, error) {
	provider := normalizeVoiceProviderID(req.Provider)
	model := strings.TrimSpace(firstNonEmpty(req.Model, req.ModelID))
	id := strings.TrimSpace(req.ID)
	for _, option := range voiceModelOptions() {
		if id != "" && option.ID == id {
			return option, nil
		}
		if provider != "" && option.Provider == provider && strings.EqualFold(option.Model, model) {
			return option, nil
		}
	}
	if provider == "" || model == "" {
		return voiceModelOption{}, fmt.Errorf("provider and model are required")
	}
	return voiceModelOption{}, fmt.Errorf("voice model %s/%s is not in the allowed model list", provider, model)
}

func buildVoiceModelStatus(message string) voiceModelStatus {
	activeProvider := activeVoiceProviderID()
	currentModel := currentVoiceModelID(activeProvider)
	status := voiceModelStatus{
		OK:              true,
		ActiveProvider:  activeProvider,
		CurrentProvider: activeProvider,
		CurrentModel:    currentModel,
		CurrentModelID:  activeProvider + ":" + currentModel,
		Options:         voiceModelOptions(),
		Message:         message,
	}
	if activeProvider == voiceProviderOpenAI {
		status.TranscriptionModel = realtimeTranscriptionModel()
	}
	for _, option := range status.Options {
		if option.Current {
			status.CurrentLabel = option.Label
			break
		}
	}
	if status.CurrentLabel == "" {
		status.CurrentLabel = currentModel
	}
	return status
}

func maybeStartLocalVoiceProviderRestart(r *http.Request, option voiceModelOption) (voiceModelStatus, bool, error) {
	if appEnvironment != "local" || !requestHostIsLocalhost(r) {
		return voiceModelStatus{}, false, nil
	}
	return startLocalVoiceProviderRestart(r, option)
}

func startLocalVoiceProviderRestart(r *http.Request, option voiceModelOption) (voiceModelStatus, bool, error) {
	refreshURL := strings.TrimSpace(getEnvDefault("APP_LOCAL_AWS_REFRESH_URL", ""))
	refreshToken := strings.TrimSpace(getEnvDefault("APP_LOCAL_AWS_REFRESH_TOKEN", ""))
	if refreshURL == "" || refreshToken == "" {
		return voiceModelStatus{}, false, nil
	}
	if err := validateLocalAWSRefreshURL(refreshURL); err != nil {
		return voiceModelStatus{}, false, err
	}

	localAWSRefreshMu.Lock()
	if time.Since(localAWSRefreshLastStart) < 20*time.Second {
		localAWSRefreshMu.Unlock()
		status := buildVoiceModelStatus("Local app restart is already in progress.")
		status.RequiresRestart = true
		status.RestartStarted = true
		status.RestartProvider = option.Provider
		status.RestartModel = option.Model
		return status, true, nil
	}
	localAWSRefreshLastStart = time.Now()
	localAWSRefreshMu.Unlock()

	brokerResponse, err := requestLocalRuntimeRestart(r.Context(), refreshURL, refreshToken, localRuntimeRestartRequest{
		Reason:        "voice_provider_switch",
		VoiceProvider: option.Provider,
		VoiceModel:    option.Model,
	})
	if err != nil {
		return voiceModelStatus{}, false, err
	}
	if !brokerResponse.OK {
		return voiceModelStatus{}, false, fmt.Errorf("%s", firstNonEmpty(brokerResponse.Message, "local restart broker rejected the request"))
	}

	status := buildVoiceModelStatus(fmt.Sprintf("Restarting local app with %s.", option.Label))
	status.RequiresRestart = true
	status.RestartStarted = true
	status.RestartProvider = option.Provider
	status.RestartModel = option.Model
	status.Message = firstNonEmpty(brokerResponse.Message, status.Message)
	return status, true, nil
}

func voiceModelOptions() []voiceModelOption {
	activeProvider := activeVoiceProviderID()
	currentNova := selectedNovaSonicModel()
	currentOpenAI := realtimeModel()

	options := []voiceModelOption{
		{
			Provider:    voiceProviderNovaSonic,
			Model:       "amazon.nova-2-sonic-v1:0",
			Label:       "AWS Nova Sonic 2",
			Description: "Default Bedrock full-duplex meeting model.",
			Transport:   "LiveKit + Bedrock",
			FullDuplex:  true,
			ToolCalling: true,
			CostHint:    "AWS Bedrock pricing",
		},
		{
			Provider:    voiceProviderNovaSonic,
			Model:       "amazon.nova-sonic-v1:0",
			Label:       "AWS Nova Sonic",
			Description: "Legacy Nova Sonic profile for comparison testing.",
			Transport:   "LiveKit + Bedrock",
			FullDuplex:  true,
			ToolCalling: true,
			CostHint:    "AWS Bedrock pricing",
		},
		{
			Provider:    voiceProviderOpenAI,
			Model:       "gpt-realtime-2",
			Label:       "OpenAI gpt-realtime-2",
			Description: "Most capable OpenAI realtime voice-to-action model.",
			Transport:   "WebRTC",
			FullDuplex:  true,
			ToolCalling: true,
			Reasoning:   true,
			CostHint:    "$4 text input / $24 text output per 1M tokens; audio billed separately",
		},
		{
			Provider:    voiceProviderOpenAI,
			Model:       "gpt-realtime-1.5",
			Label:       "OpenAI gpt-realtime-1.5",
			Description: "Flagship audio-in/audio-out model for voice agents.",
			Transport:   "WebRTC",
			FullDuplex:  true,
			ToolCalling: true,
			CostHint:    "$4 text input / $16 text output per 1M tokens; audio billed separately",
		},
		{
			Provider:    voiceProviderOpenAI,
			Model:       "gpt-realtime-mini",
			Label:       "OpenAI gpt-realtime-mini",
			Description: "Lower-cost OpenAI realtime voice model for cost-sensitive runs.",
			Transport:   "WebRTC",
			FullDuplex:  true,
			ToolCalling: true,
			CostHint:    "$0.60 text input / $2.40 text output per 1M tokens; audio billed separately",
		},
	}

	options = appendConfiguredModelOption(options, voiceProviderNovaSonic, currentNova, "Configured AWS Nova Sonic model", "LiveKit + Bedrock")
	options = appendConfiguredModelOption(options, voiceProviderOpenAI, currentOpenAI, "Configured OpenAI Realtime model", "WebRTC")

	for i := range options {
		options[i].Provider = normalizeVoiceProviderID(options[i].Provider)
		options[i].ID = options[i].Provider + ":" + options[i].Model
		options[i].Current = options[i].Provider == activeProvider && strings.EqualFold(options[i].Model, currentVoiceModelID(activeProvider))
		options[i].Selectable = options[i].FullDuplex && options[i].ToolCalling
		options[i].RequiresRestart = options[i].Provider != activeProvider
		if options[i].RequiresRestart {
			options[i].DisabledReason = fmt.Sprintf("Restart the app with VOICE_PROVIDER=%s to use this media path.", options[i].Provider)
		}
		if !options[i].ToolCalling {
			options[i].Selectable = false
			options[i].DisabledReason = "This model profile does not support Jira/GitHub tool calling."
		}
	}
	return options
}

func appendConfiguredModelOption(options []voiceModelOption, provider string, model string, label string, transport string) []voiceModelOption {
	model = strings.TrimSpace(model)
	if model == "" {
		return options
	}
	for _, option := range options {
		if normalizeVoiceProviderID(option.Provider) == normalizeVoiceProviderID(provider) && strings.EqualFold(option.Model, model) {
			return options
		}
	}
	if normalizeVoiceProviderID(provider) == voiceProviderOpenAI {
		if err := validateRealtimeConversationModel(model); err != nil {
			return options
		}
	}
	return append(options, voiceModelOption{
		Provider:    provider,
		Model:       model,
		Label:       label,
		Description: "Model supplied by environment or runtime configuration.",
		Transport:   transport,
		FullDuplex:  true,
		ToolCalling: true,
	})
}
