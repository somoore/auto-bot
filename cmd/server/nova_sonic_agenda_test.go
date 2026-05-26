package main

import (
	"strings"
	"testing"

	"github.com/somoore/auto-bot/internal/standup"
)

func TestFormatAgendaBriefing(t *testing.T) {
	cases := []struct {
		name    string
		agenda  standup.Agenda
		wantHas []string
	}{
		{
			name:    "empty",
			agenda:  standup.Agenda{},
			wantHas: nil,
		},
		{
			name: "summary plus blockers",
			agenda: standup.Agenda{
				Summary:  "We have 2 blockers today.",
				Blockers: []standup.AgendaBlocker{{Title: "Auth", Reason: "Waiting on infra"}, {Title: "DB"}},
			},
			wantHas: []string{"Blockers", "Auth", "DB", "Waiting on infra"},
		},
		{
			name: "speaker order is truncated",
			agenda: standup.Agenda{
				Summary:              "summary",
				ProposedSpeakerOrder: []string{"a", "b", "c", "d", "e", "f"},
			},
			wantHas: []string{"a;", "b;", "c;", "d;", "e", "more"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatAgendaBriefing(c.agenda)
			if len(c.wantHas) == 0 {
				if got != "" {
					t.Fatalf("expected empty briefing, got %q", got)
				}
				return
			}
			for _, want := range c.wantHas {
				if !strings.Contains(got, want) {
					t.Fatalf("briefing %q missing %q", got, want)
				}
			}
		})
	}
}
