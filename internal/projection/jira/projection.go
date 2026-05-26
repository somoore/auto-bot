// Package jira implements the internal/projection.Projection contract for
// Jira Cloud. The HTTP client + JQL machinery still lives in cmd/server during
// Sprint 3.0 — this package wraps that client behind a narrow Client interface
// so the projection logic can move incrementally without duplicating Jira API
// code.
package jira

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/projection"
)

// Client is the narrow Jira HTTP surface that JiraProjection needs.
// cmd/server's *jiraClient satisfies this via duck typing — the existing
// methods already use board.Card and board.Status (via the kanbanCard /
// kanbanStatus aliases in cmd/server/board.go), so no adapter is required.
type Client interface {
	SearchKanbanCards(ctx context.Context) ([]board.Card, error)
	CreateIssue(ctx context.Context, card board.Card) (string, error)
	UpdateIssue(ctx context.Context, cardID string, title string, notes string) error
	CloseIssue(ctx context.Context, cardID string) error
	TransitionIssue(ctx context.Context, cardID string, status board.Status) error
	AssignIssue(ctx context.Context, cardID string, accountID string) error
}

// Config describes the Jira connection in a projection-shaped way.
type Config struct {
	BaseURL    string
	ProjectKey string
	Email      string
}

// JiraProjection adapts a Jira HTTP Client to the Projection contract.
type JiraProjection struct {
	client Client
	config Config
	now    func() time.Time
}

// NewProjection wires a Jira Client and Config into a JiraProjection.
func NewProjection(client Client, config Config) *JiraProjection {
	return &JiraProjection{client: client, config: config, now: time.Now}
}

// Name returns the stable projection name.
func (proj *JiraProjection) Name() string { return "jira" }

// Capabilities declares Jira's supported projection operations.
func (proj *JiraProjection) Capabilities() projection.Capabilities {
	return projection.Capabilities{
		SupportsCreate: true, SupportsUpdate: true, SupportsDelete: true,
		SupportsWebhook: true, BiDirectional: true,
	}
}

// Project upserts Changed cards and closes Deleted card IDs.
func (proj *JiraProjection) Project(ctx context.Context, delta projection.BoardDelta) error {
	if proj.client == nil {
		return errors.New("jira projection: client is not configured")
	}
	for _, card := range delta.Changed {
		if err := proj.upsertChanged(ctx, card); err != nil {
			return err
		}
	}
	for _, cardID := range delta.Deleted {
		if strings.TrimSpace(cardID) == "" {
			continue
		}
		if err := proj.client.CloseIssue(ctx, cardID); err != nil {
			return err
		}
	}
	return nil
}

func (proj *JiraProjection) upsertChanged(ctx context.Context, card board.Card) error {
	if strings.TrimSpace(card.ID) == "" {
		if _, err := proj.client.CreateIssue(ctx, card); err != nil {
			return err
		}
		return nil
	}
	if err := proj.client.UpdateIssue(ctx, card.ID, card.Title, card.Notes); err != nil {
		return err
	}
	if strings.TrimSpace(string(card.Status)) != "" {
		if err := proj.client.TransitionIssue(ctx, card.ID, card.Status); err != nil {
			return err
		}
	}
	return proj.applyAssignee(ctx, card)
}

func (proj *JiraProjection) applyAssignee(ctx context.Context, card board.Card) error {
	if card.Assignee == nil {
		return proj.client.AssignIssue(ctx, card.ID, "")
	}
	if card.Assignee.Kind == board.ActorKindHuman {
		return proj.client.AssignIssue(ctx, card.ID, card.Assignee.ID)
	}
	return proj.client.AssignIssue(ctx, card.ID, "")
}

// Reconcile returns the current set of Jira cards mapped onto board.Card.
func (proj *JiraProjection) Reconcile(ctx context.Context) ([]board.Card, error) {
	if proj.client == nil {
		return nil, errors.New("jira projection: client is not configured")
	}
	return proj.client.SearchKanbanCards(ctx)
}

// ResolveConflict returns the default Jira policy: prefer remote.
func (proj *JiraProjection) ResolveConflict(_ context.Context, _ projection.Conflict) (projection.Resolution, error) {
	return projection.Resolution{Strategy: projection.ResolutionKeepRemote, Merged: nil}, nil
}

// Health reports whether the projection has a configured client.
func (proj *JiraProjection) Health(_ context.Context) projection.Health {
	ok := proj.client != nil
	details := map[string]string{}
	if proj.config.BaseURL != "" {
		details["base_url"] = proj.config.BaseURL
	}
	if proj.config.ProjectKey != "" {
		details["project_key"] = proj.config.ProjectKey
	}
	status := "ready"
	if !ok {
		status = "not_configured"
	}
	return projection.Health{OK: ok, Status: status, CheckedAt: proj.now().UTC(), Details: details}
}
