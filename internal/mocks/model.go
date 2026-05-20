package mocks

import (
	"context"
	"fmt"

	"github.com/openai/openai-realtime-meeting-assistant/internal/core"
)

// ModelProvider is a configurable in-memory implementation of
// core.ModelProvider for tests.
type ModelProvider struct {
	// NameValue is returned by Name.
	NameValue string
	// DisplayNameValue is returned by DisplayName.
	DisplayNameValue string
	// CapabilitiesValue is returned by Capabilities.
	CapabilitiesValue core.ModelCapabilities
	// CompleteFunc overrides the default mock text completion when set.
	CompleteFunc func(context.Context, core.ModelRequest) (core.ModelResponse, error)
}

// NewModelProvider returns a ready mock model provider.
func NewModelProvider(name string) *ModelProvider {
	return &ModelProvider{
		NameValue:        name,
		DisplayNameValue: name,
		CapabilitiesValue: core.ModelCapabilities{
			JSON:       true,
			Streaming:  false,
			Modalities: []string{"text"},
		},
	}
}

// Name returns the configured mock provider name.
func (provider *ModelProvider) Name() string {
	return provider.NameValue
}

// DisplayName returns the configured mock provider display name.
func (provider *ModelProvider) DisplayName() string {
	return provider.DisplayNameValue
}

// Capabilities returns the configured mock provider capabilities.
func (provider *ModelProvider) Capabilities() core.ModelCapabilities {
	return provider.CapabilitiesValue
}

// Complete invokes CompleteFunc when set, otherwise returns a successful mock
// text response for requests with a non-empty prompt.
func (provider *ModelProvider) Complete(ctx context.Context, request core.ModelRequest) (core.ModelResponse, error) {
	if provider.CompleteFunc != nil {
		return provider.CompleteFunc(ctx, request)
	}
	if request.Prompt == "" {
		return core.ModelResponse{}, fmt.Errorf("prompt is required")
	}
	return core.ModelResponse{Text: "mock response", ModelID: request.ModelID}, nil
}
