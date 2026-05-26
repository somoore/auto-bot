package intake

import (
	"errors"
	"strings"
	"time"
)

// Source describes how an Intake entered the system. Stored as a string on
// Intake so the JSON shape stays stable; callers branch on the constants.
type Source string

// Canonical intake sources. SourceForm is the React intake form, SourceSlack
// is the Slack webhook adapter, SourceAPI is any direct API caller.
const (
	SourceForm  Source = "form"
	SourceSlack Source = "slack"
	SourceAPI   Source = "api"
)

// ErrEmptyIntake is returned by Normalize when an intake carries no usable
// content (no yesterday, today, blockers, or raw text). Callers should reject
// such intakes rather than persisting an empty record.
var ErrEmptyIntake = errors.New("intake: requires at least one of yesterday, today, blockers, or raw_text")

// ErrMissingSubmitter is returned by Normalize when the submitter identity
// is blank. Async intake without a submitter is unanchored — the agent
// loop has no one to assign blocker cards to and no one to attribute
// transcript-style mentions to.
var ErrMissingSubmitter = errors.New("intake: submitter is required")

// BlockerItem is a single blocker mentioned in an async intake. CardID and
// Due are optional. The text is the human-authored sentence; CardID points
// at an existing kanban card when the blocker references one; Due is the
// caller-supplied target unblock date as a free-form string (ISO-8601 is
// preferred but not enforced — the board owns date parsing).
type BlockerItem struct {
	Text   string `json:"text"`
	CardID string `json:"card_id,omitempty"`
	Due    string `json:"due,omitempty"`
}

// Intake is the structured async-standup record. The shape mirrors what a
// voice standup produces per participant: yesterday/today summary, a list
// of blockers, and references to cards mentioned in the update. Source
// records the channel the intake came in on so downstream consumers can
// branch (the parser, for example, only runs Bedrock-assisted parsing on
// Source=="api" free-text bodies).
type Intake struct {
	TenantID       string        `json:"tenant_id,omitempty"`
	BoardID        string        `json:"board_id,omitempty"`
	Submitter      string        `json:"submitter"`
	SubmittedAt    time.Time     `json:"submitted_at,omitempty"`
	Yesterday      string        `json:"yesterday,omitempty"`
	Today          string        `json:"today,omitempty"`
	Blockers       []BlockerItem `json:"blockers,omitempty"`
	MentionedCards []string      `json:"mentioned_cards,omitempty"`
	Source         Source        `json:"source,omitempty"`
	RawText        string        `json:"raw_text,omitempty"`
}

// Normalize cleans an Intake in place and returns the cleaned copy. It
// trims whitespace, collapses blank blockers, deduplicates MentionedCards,
// stamps SubmittedAt if blank, and validates the minimum-content invariant
// (ErrEmptyIntake / ErrMissingSubmitter).
//
// The function never mutates the caller's slice headers but does reuse
// backing arrays for blockers when possible — callers should not rely on
// pointer identity of the returned slice elements.
func Normalize(in Intake, now time.Time) (Intake, error) {
	out := in
	out.TenantID = strings.TrimSpace(out.TenantID)
	out.BoardID = strings.TrimSpace(out.BoardID)
	out.Submitter = strings.TrimSpace(out.Submitter)
	out.Yesterday = strings.TrimSpace(out.Yesterday)
	out.Today = strings.TrimSpace(out.Today)
	out.RawText = strings.TrimSpace(out.RawText)
	out.Source = normalizeSource(out.Source)

	if out.Submitter == "" {
		return Intake{}, ErrMissingSubmitter
	}

	cleanBlockers := make([]BlockerItem, 0, len(out.Blockers))
	for _, b := range out.Blockers {
		text := strings.TrimSpace(b.Text)
		if text == "" {
			continue
		}
		cleanBlockers = append(cleanBlockers, BlockerItem{
			Text:   text,
			CardID: strings.TrimSpace(b.CardID),
			Due:    strings.TrimSpace(b.Due),
		})
	}
	out.Blockers = cleanBlockers

	seen := map[string]struct{}{}
	cleanCards := make([]string, 0, len(out.MentionedCards))
	for _, ref := range out.MentionedCards {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		cleanCards = append(cleanCards, ref)
	}
	out.MentionedCards = cleanCards

	if out.Yesterday == "" && out.Today == "" && out.RawText == "" && len(out.Blockers) == 0 {
		return Intake{}, ErrEmptyIntake
	}

	if out.SubmittedAt.IsZero() {
		out.SubmittedAt = now.UTC()
	} else {
		out.SubmittedAt = out.SubmittedAt.UTC()
	}

	return out, nil
}

func normalizeSource(s Source) Source {
	switch Source(strings.ToLower(strings.TrimSpace(string(s)))) {
	case SourceSlack:
		return SourceSlack
	case SourceAPI:
		return SourceAPI
	case SourceForm, "":
		return SourceForm
	default:
		return SourceForm
	}
}
