package core_test

import (
	"testing"

	"github.com/somoore/auto-bot/internal/core"
	"github.com/somoore/auto-bot/internal/core/contracttest"
	"github.com/somoore/auto-bot/internal/mocks"
)

func TestConnectorRegistryRejectsDuplicatesAndListsDeterministically(t *testing.T) {
	registry := core.NewConnectorRegistry()
	if err := registry.Register(mocks.NewConnector("jira")); err != nil {
		t.Fatalf("register jira: %v", err)
	}
	if err := registry.Register(mocks.NewConnector("github")); err != nil {
		t.Fatalf("register github: %v", err)
	}
	if err := registry.Register(mocks.NewConnector("jira")); err == nil {
		t.Fatal("expected duplicate connector registration to fail")
	}
	names := registry.Names()
	if len(names) != 2 || names[0] != "github" || names[1] != "jira" {
		t.Fatalf("names = %#v, want sorted github,jira", names)
	}
	if _, ok := registry.Get("JIRA"); !ok {
		t.Fatal("registry lookup should be case-insensitive")
	}
}

func TestMockConnectorSatisfiesContract(t *testing.T) {
	connector := mocks.NewConnector("jira")
	contracttest.Connector(t, connector, []contracttest.ConnectorCase{
		{
			Name: "create action",
			Action: core.ConnectorAction{
				Connector: "jira",
				Type:      "create",
				Target:    "EMAL",
				Risk:      core.RiskLow,
			},
			ExpectOK: true,
		},
	})
}

func TestVoiceRegistryAndMockProviderContract(t *testing.T) {
	registry := core.NewVoiceRegistry()
	provider := mocks.NewVoiceProvider("nova-sonic")
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	if err := registry.Register(provider); err == nil {
		t.Fatal("expected duplicate voice provider registration to fail")
	}
	if _, ok := registry.Get("NOVA-SONIC"); !ok {
		t.Fatal("voice registry lookup should be case-insensitive")
	}
	contracttest.VoiceProvider(t, provider, []contracttest.VoiceProviderCase{
		{
			Name:    "meeting session",
			Request: core.VoiceSessionRequest{MeetingID: "meeting-1", Identity: "scott"},
		},
	})
}

func TestModelRegistry(t *testing.T) {
	registry := core.NewModelRegistry()
	if err := registry.Register(mocks.NewModelProvider("bedrock")); err != nil {
		t.Fatalf("register model provider: %v", err)
	}
	if _, ok := registry.Get("BEDROCK"); !ok {
		t.Fatal("model registry lookup should be case-insensitive")
	}
}
