package mocks

import (
	"context"
	"fmt"
	"time"

	"github.com/somoore/auto-bot/internal/core"
)

// VoiceProvider is a configurable in-memory implementation of
// core.VoiceProvider for tests.
type VoiceProvider struct {
	// NameValue is returned by Name.
	NameValue string
	// DisplayNameValue is returned by DisplayName.
	DisplayNameValue string
	// CapabilitiesValue is returned by Capabilities.
	CapabilitiesValue core.VoiceCapabilities
	// HealthValue is returned by Health.
	HealthValue core.VoiceHealth
	// StartFunc overrides the default mock session creation when set.
	StartFunc func(context.Context, core.VoiceSessionRequest) (core.VoiceSession, error)
}

// NewVoiceProvider returns a ready mock full-duplex voice provider.
func NewVoiceProvider(name string) *VoiceProvider {
	return &VoiceProvider{
		NameValue:        name,
		DisplayNameValue: name,
		CapabilitiesValue: core.VoiceCapabilities{
			FullDuplex:      true,
			Transport:       "mock",
			Modalities:      []string{"audio", "transcript"},
			SupportsBargeIn: true,
		},
		HealthValue: core.VoiceHealth{
			OK:        true,
			Status:    "ready",
			CheckedAt: time.Now().UTC(),
		},
	}
}

// Name returns the configured mock provider name.
func (provider *VoiceProvider) Name() string {
	return provider.NameValue
}

// DisplayName returns the configured mock provider display name.
func (provider *VoiceProvider) DisplayName() string {
	return provider.DisplayNameValue
}

// Capabilities returns the configured mock provider capabilities.
func (provider *VoiceProvider) Capabilities() core.VoiceCapabilities {
	return provider.CapabilitiesValue
}

// Health returns the configured mock provider health response.
func (provider *VoiceProvider) Health(context.Context) core.VoiceHealth {
	return provider.HealthValue
}

// StartSession invokes StartFunc when set, otherwise creates a mock session for
// requests with a non-empty meeting ID.
func (provider *VoiceProvider) StartSession(ctx context.Context, request core.VoiceSessionRequest) (core.VoiceSession, error) {
	if provider.StartFunc != nil {
		return provider.StartFunc(ctx, request)
	}
	if request.MeetingID == "" {
		return nil, fmt.Errorf("meeting_id is required")
	}
	return newVoiceSession("mock-session-" + request.MeetingID), nil
}

type voiceSession struct {
	id     string
	events chan core.VoiceSessionEvent
}

func newVoiceSession(id string) *voiceSession {
	return &voiceSession{id: id, events: make(chan core.VoiceSessionEvent)}
}

func (session *voiceSession) ID() string {
	return session.id
}

func (session *voiceSession) Events() <-chan core.VoiceSessionEvent {
	return session.events
}

func (session *voiceSession) Close(context.Context) error {
	close(session.events)
	return nil
}
