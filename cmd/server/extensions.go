package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/core"
)

type extensionRuntimeState struct {
	voice      *core.VoiceRegistry
	connectors *core.ConnectorRegistry
	models     *core.ModelRegistry
}

var extensions *extensionRuntimeState

func setupExtensionRuntime(board *kanbanBoard, syncer *jiraSyncer) *extensionRuntimeState {
	runtime := &extensionRuntimeState{
		voice:      core.NewVoiceRegistry(),
		connectors: core.NewConnectorRegistry(),
		models:     core.NewModelRegistry(),
	}
	_ = runtime.voice.Register(serverVoiceProviderDescriptor{
		name:        "nova-sonic",
		displayName: "AWS Nova Sonic",
		capabilities: core.VoiceCapabilities{
			FullDuplex:      true,
			Transport:       "LiveKit",
			Modalities:      []string{"audio", "transcript"},
			SupportsBargeIn: true,
		},
		enabled: voiceProvider == "nova-sonic",
	})
	_ = runtime.voice.Register(serverVoiceProviderDescriptor{
		name:        "livekit-cloud",
		displayName: "LiveKit Cloud",
		capabilities: core.VoiceCapabilities{
			FullDuplex:      true,
			Transport:       "LiveKit Cloud",
			Modalities:      []string{"audio", "transcript"},
			SupportsBargeIn: true,
		},
		enabled: strings.EqualFold(getEnvDefault("LIVEKIT_DEPLOYMENT_MODE", "self-hosted"), "cloud"),
	})
	if board != nil {
		_ = runtime.connectors.Register(boardToolConnector{board: board})
	}
	_ = runtime.connectors.Register(jiraConnectorDescriptor{syncer: syncer})
	_ = runtime.connectors.Register(githubConnectorDescriptor{})
	return runtime
}

func registerAgentModelProvider(client agentModelClient) {
	if extensions == nil || extensions.models == nil || client == nil {
		return
	}
	_ = extensions.models.Register(bedrockModelProvider{client: client})
}

func registeredVoiceProviderOptions() []voiceProviderOption {
	if extensions == nil || extensions.voice == nil {
		return nil
	}
	providers := extensions.voice.List()
	options := make([]voiceProviderOption, 0, len(providers))
	for _, provider := range providers {
		capabilities := provider.Capabilities()
		health := provider.Health(context.Background())
		notes := provider.DisplayName()
		if profile := health.Details["profile"]; profile != "" {
			notes = notes + " / " + profile
		}
		if model := health.Details["model"]; model != "" {
			notes = notes + " / " + model
		}
		options = append(options, voiceProviderOption{
			Name:       provider.Name(),
			Enabled:    health.OK,
			FullDuplex: capabilities.FullDuplex,
			Transport:  capabilities.Transport,
			Notes:      notes,
		})
	}
	return options
}

type serverVoiceProviderDescriptor struct {
	name         string
	displayName  string
	capabilities core.VoiceCapabilities
	enabled      bool
	details      map[string]string
}

func (provider serverVoiceProviderDescriptor) Name() string {
	return provider.name
}

func (provider serverVoiceProviderDescriptor) DisplayName() string {
	return provider.displayName
}

func (provider serverVoiceProviderDescriptor) Capabilities() core.VoiceCapabilities {
	return provider.capabilities
}

func (provider serverVoiceProviderDescriptor) Health(context.Context) core.VoiceHealth {
	status := "available"
	if provider.enabled {
		status = "active"
	}
	details := map[string]string{
		"transport": provider.capabilities.Transport,
	}
	for key, value := range provider.details {
		details[key] = value
	}
	switch provider.name {
	case "nova-sonic":
		details["model"] = selectedNovaSonicModel()
		details["profile"] = "voice-agent"
	}
	return core.VoiceHealth{
		OK:        provider.enabled,
		Status:    status,
		CheckedAt: time.Now().UTC(),
		Details:   details,
	}
}

func (provider serverVoiceProviderDescriptor) StartSession(context.Context, core.VoiceSessionRequest) (core.VoiceSession, error) {
	return nil, fmt.Errorf("%s sessions are owned by the server HTTP/WebSocket runtime", provider.name)
}

type boardToolConnector struct {
	board *kanbanBoard
}

func (connector boardToolConnector) Name() string {
	return "local-board"
}

func (connector boardToolConnector) DisplayName() string {
	return "Local Board Tool Broker"
}

func (connector boardToolConnector) Capabilities() []core.ConnectorCapability {
	return []core.ConnectorCapability{
		{Name: "create_ticket", Description: "Create local board work.", Risk: core.RiskLow, SupportsUndo: true},
		{Name: "move_ticket", Description: "Move local board work.", Risk: core.RiskLow, SupportsUndo: true},
		{Name: "assign_ticket", Description: "Assign local board work.", Risk: core.RiskMedium, SupportsUndo: true},
		{Name: "record_meeting_memory", Description: "Persist meeting intelligence.", Risk: core.RiskLow, SupportsUndo: false},
	}
}

func (connector boardToolConnector) Health(context.Context) core.ConnectorHealth {
	return core.ConnectorHealth{
		OK:        connector.board != nil,
		Status:    extensionBoolStatus(connector.board != nil, "ready", "not_configured"),
		CheckedAt: time.Now().UTC(),
	}
}

func (connector boardToolConnector) Execute(ctx context.Context, action core.ConnectorAction) (core.ConnectorResult, error) {
	if connector.board == nil {
		return core.ConnectorResult{OK: false, Status: "not_configured"}, nil
	}
	rawArgs, err := json.Marshal(action.Parameters)
	if err != nil {
		return core.ConnectorResult{}, fmt.Errorf("marshal connector action parameters: %w", err)
	}
	result, changed, err := connector.board.ApplyToolCallWithMeta(action.Type, string(rawArgs), toolCallMeta{
		Source:     "connector:" + action.Connector,
		Actor:      action.RequestedBy,
		Transcript: evidenceText(action.Evidence),
	})
	if err != nil {
		return core.ConnectorResult{OK: false, Status: "local_failed", Message: err.Error()}, nil
	}
	status := "unchanged"
	if changed {
		status = "local_confirmed"
	}
	return core.ConnectorResult{
		OK:     true,
		Status: status,
		Receipt: core.ActionReceipt{
			ID:         asString(result["audit_event_id"]),
			Connector:  connector.Name(),
			ExternalID: firstNonEmptyString(result, "card_id", "id"),
			Undoable:   changed,
			At:         time.Now().UTC(),
			Metadata:   result,
		},
		Data: result,
	}, nil
}

func (connector boardToolConnector) Undo(ctx context.Context, receipt core.ActionReceipt) (core.ConnectorResult, error) {
	if connector.board == nil {
		return core.ConnectorResult{OK: false, Status: "not_configured"}, nil
	}
	result, changed, err := connector.board.undoLastMutation(map[string]any{}, toolCallMeta{
		Source: "connector:" + connector.Name(),
	})
	if err != nil {
		return core.ConnectorResult{OK: false, Status: "undo_failed", Message: err.Error()}, nil
	}
	return core.ConnectorResult{
		OK:      changed,
		Status:  extensionBoolStatus(changed, "undone", "unchanged"),
		Receipt: receipt,
		Data:    result,
	}, nil
}

type jiraConnectorDescriptor struct {
	syncer *jiraSyncer
}

func (connector jiraConnectorDescriptor) Name() string {
	return "jira"
}

func (connector jiraConnectorDescriptor) DisplayName() string {
	return "Jira Cloud"
}

func (connector jiraConnectorDescriptor) Capabilities() []core.ConnectorCapability {
	return []core.ConnectorCapability{
		{Name: "issue_read", Description: "Hydrate board issues.", Risk: core.RiskLow, SupportsUndo: false},
		{Name: "issue_write", Description: "Write issue mutations after policy approval.", Risk: core.RiskMedium, SupportsUndo: true},
		{Name: "user_search", Description: "Find assignable Jira users.", Risk: core.RiskLow, SupportsUndo: false},
	}
}

func (connector jiraConnectorDescriptor) Health(context.Context) core.ConnectorHealth {
	configured := connector.syncer != nil
	return core.ConnectorHealth{
		OK:        configured,
		Status:    extensionBoolStatus(configured, "ready", "not_configured"),
		CheckedAt: time.Now().UTC(),
	}
}

func (connector jiraConnectorDescriptor) Execute(context.Context, core.ConnectorAction) (core.ConnectorResult, error) {
	return core.ConnectorResult{
		OK:      false,
		Status:  "routed_through_board_tool_broker",
		Message: "Jira writes currently execute through the board tool broker so local mutation, policy, confirmation, audit, and Jira confirmation stay atomic.",
	}, nil
}

func (connector jiraConnectorDescriptor) Undo(context.Context, core.ActionReceipt) (core.ConnectorResult, error) {
	return core.ConnectorResult{
		OK:      false,
		Status:  "routed_through_board_tool_broker",
		Message: "Use board undo so local state and Jira confirmation evidence are replayed together.",
	}, nil
}

type githubConnectorDescriptor struct{}

func (connector githubConnectorDescriptor) Name() string {
	return "github"
}

func (connector githubConnectorDescriptor) DisplayName() string {
	return "GitHub App"
}

func (connector githubConnectorDescriptor) Capabilities() []core.ConnectorCapability {
	return []core.ConnectorCapability{
		{Name: "repo_read", Description: "Read repository and pull request context through GitHub App installation tokens.", Risk: core.RiskLow, SupportsUndo: false},
		{Name: "pr_review_comment", Description: "Publish PR review comments after review completion.", Risk: core.RiskMedium, SupportsUndo: false},
	}
}

func (connector githubConnectorDescriptor) Health(context.Context) core.ConnectorHealth {
	configured := strings.TrimSpace(getEnvDefault("GITHUB_APP_ID", "")) != "" ||
		strings.TrimSpace(getEnvDefault("GITHUB_APP_ID_FILE", "")) != ""
	return core.ConnectorHealth{
		OK:        configured,
		Status:    extensionBoolStatus(configured, "ready", "not_configured"),
		CheckedAt: time.Now().UTC(),
	}
}

func (connector githubConnectorDescriptor) Execute(context.Context, core.ConnectorAction) (core.ConnectorResult, error) {
	return core.ConnectorResult{
		OK:      false,
		Status:  "routed_through_agent_orchestrator",
		Message: "GitHub operations are executed by agent runs so findings, publish warnings, and Jira writeback share one audit trail.",
	}, nil
}

func (connector githubConnectorDescriptor) Undo(context.Context, core.ActionReceipt) (core.ConnectorResult, error) {
	return core.ConnectorResult{OK: false, Status: "not_undoable", Message: "GitHub PR review comments are append-only in this runtime."}, nil
}

type bedrockModelProvider struct {
	client agentModelClient
}

func (provider bedrockModelProvider) Name() string {
	return "bedrock"
}

func (provider bedrockModelProvider) DisplayName() string {
	return "AWS Bedrock"
}

func (provider bedrockModelProvider) Capabilities() core.ModelCapabilities {
	return core.ModelCapabilities{
		JSON:       true,
		Streaming:  false,
		Modalities: []string{"text"},
	}
}

func (provider bedrockModelProvider) Complete(ctx context.Context, request core.ModelRequest) (core.ModelResponse, error) {
	if provider.client == nil {
		return core.ModelResponse{}, fmt.Errorf("bedrock agent model client is not configured")
	}
	raw, err := provider.client.CompleteJSON(ctx, request.ModelID, request.System, request.Prompt, request.MaxTokens)
	if err != nil {
		return core.ModelResponse{}, err
	}
	return core.ModelResponse{JSON: raw, Text: string(raw), ModelID: request.ModelID}, nil
}

func evidenceText(evidence []core.Evidence) string {
	parts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if strings.TrimSpace(item.Text) != "" {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func extensionBoolStatus(ok bool, okStatus string, notOKStatus string) string {
	if ok {
		return okStatus
	}
	return notOKStatus
}
