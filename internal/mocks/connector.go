package mocks

import (
	"context"
	"fmt"
	"time"

	"github.com/openai/openai-realtime-meeting-assistant/internal/core"
)

// Connector is a configurable in-memory implementation of core.Connector for
// tests.
type Connector struct {
	// NameValue is returned by Name.
	NameValue string
	// DisplayNameValue is returned by DisplayName.
	DisplayNameValue string
	// CapabilitiesValue is copied by Capabilities.
	CapabilitiesValue []core.ConnectorCapability
	// HealthValue is returned by Health.
	HealthValue core.ConnectorHealth
	// ExecuteFunc overrides the default successful mock execution when set.
	ExecuteFunc func(context.Context, core.ConnectorAction) (core.ConnectorResult, error)
	// UndoFunc overrides the default undo behavior when set.
	UndoFunc func(context.Context, core.ActionReceipt) (core.ConnectorResult, error)
}

// NewConnector returns a ready mock connector with one low-risk capability.
func NewConnector(name string) *Connector {
	return &Connector{
		NameValue:        name,
		DisplayNameValue: name,
		CapabilitiesValue: []core.ConnectorCapability{
			{Name: "create", Description: "Create a mock record.", Risk: core.RiskLow, SupportsUndo: true},
		},
		HealthValue: core.ConnectorHealth{
			OK:        true,
			Status:    "ready",
			CheckedAt: time.Now().UTC(),
		},
	}
}

// Name returns the configured mock connector name.
func (connector *Connector) Name() string {
	return connector.NameValue
}

// DisplayName returns the configured mock connector display name.
func (connector *Connector) DisplayName() string {
	return connector.DisplayNameValue
}

// Capabilities returns a defensive copy of configured mock capabilities.
func (connector *Connector) Capabilities() []core.ConnectorCapability {
	return append([]core.ConnectorCapability{}, connector.CapabilitiesValue...)
}

// Health returns the configured mock health response.
func (connector *Connector) Health(context.Context) core.ConnectorHealth {
	return connector.HealthValue
}

// Execute invokes ExecuteFunc when set, otherwise returns a successful mock
// receipt for actions with a non-empty type.
func (connector *Connector) Execute(ctx context.Context, action core.ConnectorAction) (core.ConnectorResult, error) {
	if connector.ExecuteFunc != nil {
		return connector.ExecuteFunc(ctx, action)
	}
	if action.Type == "" {
		return core.ConnectorResult{}, fmt.Errorf("action type is required")
	}
	return core.ConnectorResult{
		OK:     true,
		Status: "mock_confirmed",
		Receipt: core.ActionReceipt{
			ID:         "mock-receipt-" + action.Type,
			Connector:  connector.NameValue,
			ExternalID: action.Target,
			Undoable:   true,
			UndoToken:  "mock-undo-" + action.Type,
			At:         time.Now().UTC(),
		},
	}, nil
}

// Undo invokes UndoFunc when set, otherwise succeeds only for undoable receipts.
func (connector *Connector) Undo(ctx context.Context, receipt core.ActionReceipt) (core.ConnectorResult, error) {
	if connector.UndoFunc != nil {
		return connector.UndoFunc(ctx, receipt)
	}
	if !receipt.Undoable {
		return core.ConnectorResult{OK: false, Status: "not_undoable"}, nil
	}
	return core.ConnectorResult{OK: true, Status: "mock_undone", Receipt: receipt}, nil
}
