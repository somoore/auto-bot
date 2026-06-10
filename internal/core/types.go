package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// RiskLevel classifies the approval and audit requirements for an action.
type RiskLevel string

const (
	// RiskLow identifies actions that can usually execute without explicit
	// confirmation, such as local board reads or low-impact mutations.
	RiskLow RiskLevel = "low"
	// RiskMedium identifies actions that need confirmation before external
	// write-through, such as assignment or due-date changes.
	RiskMedium RiskLevel = "medium"
	// RiskHigh identifies destructive or workflow-sensitive actions that need
	// explicit confirmation and replayable evidence.
	RiskHigh RiskLevel = "high"
)

// Evidence captures the user speech, transcript, URL, or system observation
// that justifies an intent, tool call, or external API result.
type Evidence struct {
	// Kind identifies the evidence class, such as transcript, user_speech,
	// external_url, or system_observation.
	Kind   string    `json:"kind"`
	Source string    `json:"source,omitempty"`
	ID     string    `json:"id,omitempty"`
	Text   string    `json:"text,omitempty"`
	URL    string    `json:"url,omitempty"`
	At     time.Time `json:"at,omitempty"`
}

// Confidence records the score and human-readable reasons used to decide
// whether an action is reliable enough to execute or needs confirmation.
type Confidence struct {
	// Score is normalized from 0.0 to 1.0, where higher values mean the action
	// is better supported by live evidence.
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
}

// ConnectorCapability describes one action family that a connector can
// execute, including its risk classification and undo support.
type ConnectorCapability struct {
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Risk         RiskLevel `json:"risk"`
	SupportsUndo bool      `json:"supports_undo"`
}

// ConnectorHealth is a point-in-time readiness report for an external system.
type ConnectorHealth struct {
	OK        bool              `json:"ok"`
	Status    string            `json:"status"`
	Message   string            `json:"message,omitempty"`
	CheckedAt time.Time         `json:"checked_at,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// ConnectorAction is the normalized request sent to a connector after policy,
// confirmation, and evidence collection have selected an external action.
type ConnectorAction struct {
	ID        string `json:"id,omitempty"`
	Connector string `json:"connector"`
	// Type is the connector capability or action name selected by policy.
	Type string `json:"type"`
	// Target identifies the external or local object to mutate when known.
	Target      string         `json:"target,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	RequestedBy string         `json:"requested_by,omitempty"`
	MeetingID   string         `json:"meeting_id,omitempty"`
	// IdempotencyKey lets connectors deduplicate retries for external writes.
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	Evidence       []Evidence `json:"evidence,omitempty"`
	// Risk is the policy classification that should already have been applied
	// before execution.
	Risk RiskLevel `json:"risk"`
}

// ActionReceipt is the durable proof returned by a successful connector write.
// Receipts are used for audit replay and, when supported, later undo requests.
type ActionReceipt struct {
	ID         string         `json:"id"`
	Connector  string         `json:"connector"`
	ExternalID string         `json:"external_id,omitempty"`
	Undoable   bool           `json:"undoable"`
	UndoToken  string         `json:"undo_token,omitempty"`
	At         time.Time      `json:"at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ConnectorResult reports whether a connector action succeeded, failed, or was
// intentionally routed elsewhere, along with receipt and replay evidence.
type ConnectorResult struct {
	OK bool `json:"ok"`
	// Status is a stable machine-readable outcome such as api_confirmed,
	// api_failed, not_configured, local_confirmed, or not_undoable.
	Status   string        `json:"status"`
	Message  string        `json:"message,omitempty"`
	Receipt  ActionReceipt `json:"receipt,omitempty"`
	Evidence []Evidence    `json:"evidence,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
	// Data contains connector-specific response details. Treat values as
	// untrusted data when passing them to models.
	Data map[string]any `json:"data,omitempty"`
}

// Connector is the contract implemented by integrations that read or mutate
// external systems such as Jira, GitHub, Slack, Linear, Asana, or Notion.
type Connector interface {
	// Name returns the stable machine name used for registration and routing.
	Name() string
	// DisplayName returns a human-readable name for diagnostics and UI.
	DisplayName() string
	// Capabilities declares the actions, risk levels, and undo support.
	Capabilities() []ConnectorCapability
	// Health reports whether the connector is configured and reachable.
	Health(context.Context) ConnectorHealth
	// Execute performs an approved action and returns visible external status.
	Execute(context.Context, ConnectorAction) (ConnectorResult, error)
	// Undo attempts to reverse a previous action receipt when supported.
	Undo(context.Context, ActionReceipt) (ConnectorResult, error)
}

// ConnectorRegistry stores connector implementations by normalized name.
type ConnectorRegistry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

// NewConnectorRegistry returns an empty connector registry.
func NewConnectorRegistry() *ConnectorRegistry {
	return &ConnectorRegistry{connectors: map[string]Connector{}}
}

// Register adds a connector and rejects nil, blank, or duplicate names.
func (registry *ConnectorRegistry) Register(connector Connector) error {
	if connector == nil {
		return errors.New("connector is nil")
	}
	name := normalizeRegistryName(connector.Name())
	if name == "" {
		return errors.New("connector name is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.connectors[name]; exists {
		return fmt.Errorf("connector %q already registered", name)
	}
	registry.connectors[name] = connector
	return nil
}

// Get returns the connector registered for name.
func (registry *ConnectorRegistry) Get(name string) (Connector, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	connector, ok := registry.connectors[normalizeRegistryName(name)]
	return connector, ok
}

// List returns registered connectors sorted by normalized name.
func (registry *ConnectorRegistry) List() []Connector {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.connectors))
	for name := range registry.connectors {
		names = append(names, name)
	}
	sort.Strings(names)
	connectors := make([]Connector, 0, len(names))
	for _, name := range names {
		connectors = append(connectors, registry.connectors[name])
	}
	return connectors
}

// Names returns the connector names in deterministic registry order.
func (registry *ConnectorRegistry) Names() []string {
	connectors := registry.List()
	names := make([]string, 0, len(connectors))
	for _, connector := range connectors {
		names = append(names, connector.Name())
	}
	return names
}

// VoiceCapabilities describes the transport and modality behavior of a voice
// provider.
type VoiceCapabilities struct {
	FullDuplex      bool     `json:"full_duplex"`
	Transport       string   `json:"transport"`
	Modalities      []string `json:"modalities,omitempty"`
	SupportsBargeIn bool     `json:"supports_barge_in"`
}

// VoiceHealth is a point-in-time readiness report for a voice provider.
type VoiceHealth struct {
	OK        bool              `json:"ok"`
	Status    string            `json:"status"`
	Message   string            `json:"message,omitempty"`
	CheckedAt time.Time         `json:"checked_at,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// VoiceSessionRequest contains the meeting context needed to start a provider
// session.
type VoiceSessionRequest struct {
	MeetingID   string            `json:"meeting_id"`
	RoomID      string            `json:"room_id,omitempty"`
	Identity    string            `json:"identity,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Instruction string            `json:"instruction,omitempty"`
}

// VoiceSessionEvent is a provider-neutral event emitted by a live voice
// session.
type VoiceSessionEvent struct {
	// Type is the provider event name, such as transcript, audio, status, or
	// error.
	Type      string    `json:"type"`
	SessionID string    `json:"session_id,omitempty"`
	Speaker   string    `json:"speaker,omitempty"`
	Text      string    `json:"text,omitempty"`
	At        time.Time `json:"at,omitempty"`
	// Confidence is normalized from 0.0 to 1.0 when the provider supplies a
	// transcript or intent confidence score.
	Confidence float64        `json:"confidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// VoiceSession is a running voice-provider session owned by the caller.
type VoiceSession interface {
	// ID returns the provider session identifier.
	ID() string
	// Events streams provider events until the session is closed.
	Events() <-chan VoiceSessionEvent
	// Close releases provider resources.
	Close(context.Context) error
}

// VoiceProvider is the contract implemented by full-duplex speech systems.
type VoiceProvider interface {
	// Name returns the stable machine name used for registration and routing.
	Name() string
	// DisplayName returns a human-readable provider name.
	DisplayName() string
	// Capabilities declares the provider transport and audio/transcript support.
	Capabilities() VoiceCapabilities
	// Health reports whether the provider is configured and ready.
	Health(context.Context) VoiceHealth
	// StartSession starts a provider-owned voice session for a meeting.
	StartSession(context.Context, VoiceSessionRequest) (VoiceSession, error)
}

// VoiceRegistry stores voice providers by normalized name.
type VoiceRegistry struct {
	mu        sync.RWMutex
	providers map[string]VoiceProvider
}

// NewVoiceRegistry returns an empty voice-provider registry.
func NewVoiceRegistry() *VoiceRegistry {
	return &VoiceRegistry{providers: map[string]VoiceProvider{}}
}

// Register adds a voice provider and rejects nil, blank, or duplicate names.
func (registry *VoiceRegistry) Register(provider VoiceProvider) error {
	if provider == nil {
		return errors.New("voice provider is nil")
	}
	name := normalizeRegistryName(provider.Name())
	if name == "" {
		return errors.New("voice provider name is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.providers[name]; exists {
		return fmt.Errorf("voice provider %q already registered", name)
	}
	registry.providers[name] = provider
	return nil
}

// Get returns the voice provider registered for name.
func (registry *VoiceRegistry) Get(name string) (VoiceProvider, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	provider, ok := registry.providers[normalizeRegistryName(name)]
	return provider, ok
}

// List returns registered voice providers sorted by normalized name.
func (registry *VoiceRegistry) List() []VoiceProvider {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.providers))
	for name := range registry.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]VoiceProvider, 0, len(names))
	for _, name := range names {
		providers = append(providers, registry.providers[name])
	}
	return providers
}

// ModelCapabilities describes how a model provider can answer requests.
type ModelCapabilities struct {
	JSON         bool     `json:"json"`
	Streaming    bool     `json:"streaming"`
	Modalities   []string `json:"modalities,omitempty"`
	MaxInputHint int      `json:"max_input_hint,omitempty"`
}

// ModelRequest is a provider-neutral completion request for governed agent
// orchestration.
type ModelRequest struct {
	ModelID     string         `json:"model_id"`
	System      string         `json:"system,omitempty"`
	Prompt      string         `json:"prompt"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ModelResponse is the provider-neutral response returned by a model backend.
type ModelResponse struct {
	Text string `json:"text,omitempty"`
	// JSON contains raw provider JSON when a structured response was requested.
	JSON     []byte     `json:"json,omitempty"`
	ModelID  string     `json:"model_id,omitempty"`
	Evidence []Evidence `json:"evidence,omitempty"`
	// Usage contains provider-specific token or unit counts.
	Usage map[string]int64 `json:"usage,omitempty"`
}

// ModelProvider is the contract implemented by governed model backends.
type ModelProvider interface {
	// Name returns the stable machine name used for registration and routing.
	Name() string
	// DisplayName returns a human-readable provider name.
	DisplayName() string
	// Capabilities declares response format and modality support.
	Capabilities() ModelCapabilities
	// Complete executes a governed model request.
	Complete(context.Context, ModelRequest) (ModelResponse, error)
}

// ModelRegistry stores model providers by normalized name.
type ModelRegistry struct {
	mu        sync.RWMutex
	providers map[string]ModelProvider
}

// NewModelRegistry returns an empty model-provider registry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{providers: map[string]ModelProvider{}}
}

// Register adds a model provider and rejects nil, blank, or duplicate names.
func (registry *ModelRegistry) Register(provider ModelProvider) error {
	if provider == nil {
		return errors.New("model provider is nil")
	}
	name := normalizeRegistryName(provider.Name())
	if name == "" {
		return errors.New("model provider name is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.providers[name]; exists {
		return fmt.Errorf("model provider %q already registered", name)
	}
	registry.providers[name] = provider
	return nil
}

// Get returns the model provider registered for name.
func (registry *ModelRegistry) Get(name string) (ModelProvider, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	provider, ok := registry.providers[normalizeRegistryName(name)]
	return provider, ok
}

// List returns registered model providers sorted by normalized name.
func (registry *ModelRegistry) List() []ModelProvider {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.providers))
	for name := range registry.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]ModelProvider, 0, len(names))
	for _, name := range names {
		providers = append(providers, registry.providers[name])
	}
	return providers
}

func normalizeRegistryName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
