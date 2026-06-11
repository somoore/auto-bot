package contracttest

import (
	"context"
	"testing"

	"github.com/somoore/auto-bot/internal/core"
)

// ConnectorCase describes one connector execution path for the shared contract
// test helper.
type ConnectorCase struct {
	Name string
	// Action is passed directly to connector.Execute.
	Action core.ConnectorAction
	// ExpectOK is compared with ConnectorResult.OK when execution returns.
	ExpectOK bool
	// ExpectError means Execute should return a Go error. Connector
	// implementations should still return non-error failures as a result with a
	// non-empty Status.
	ExpectError bool
}

// Connector verifies the required metadata, health, and execution behavior for
// a core.Connector implementation.
func Connector(t *testing.T, connector core.Connector, cases []ConnectorCase) {
	t.Helper()
	if connector == nil {
		t.Fatal("connector is nil")
	}
	if connector.Name() == "" {
		t.Fatal("connector name is empty")
	}
	if connector.DisplayName() == "" {
		t.Fatal("connector display name is empty")
	}
	if len(connector.Capabilities()) == 0 {
		t.Fatal("connector must declare at least one capability")
	}
	health := connector.Health(context.Background())
	if health.Status == "" {
		t.Fatal("connector health status is empty")
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.Name, func(t *testing.T) {
			result, err := connector.Execute(context.Background(), testCase.Action)
			if testCase.ExpectError && err == nil {
				t.Fatal("expected connector execution error")
			}
			if !testCase.ExpectError && err != nil {
				t.Fatalf("connector execution failed: %v", err)
			}
			if result.OK != testCase.ExpectOK {
				t.Fatalf("result.OK = %v, want %v", result.OK, testCase.ExpectOK)
			}
			if result.Status == "" {
				t.Fatal("connector result status is empty")
			}
		})
	}
}

// VoiceProviderCase describes one session startup path for the shared voice
// provider contract test helper.
type VoiceProviderCase struct {
	Name string
	// Request is passed directly to provider.StartSession.
	Request core.VoiceSessionRequest
	// ExpectError means StartSession should return a Go error.
	ExpectError bool
}

// VoiceProvider verifies the required metadata, health, session startup, and
// cleanup behavior for a core.VoiceProvider implementation.
func VoiceProvider(t *testing.T, provider core.VoiceProvider, cases []VoiceProviderCase) {
	t.Helper()
	if provider == nil {
		t.Fatal("voice provider is nil")
	}
	if provider.Name() == "" {
		t.Fatal("voice provider name is empty")
	}
	if provider.DisplayName() == "" {
		t.Fatal("voice provider display name is empty")
	}
	if provider.Capabilities().Transport == "" {
		t.Fatal("voice provider transport is empty")
	}
	health := provider.Health(context.Background())
	if health.Status == "" {
		t.Fatal("voice provider health status is empty")
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.Name, func(t *testing.T) {
			session, err := provider.StartSession(context.Background(), testCase.Request)
			if testCase.ExpectError && err == nil {
				t.Fatal("expected voice session error")
			}
			if !testCase.ExpectError && err != nil {
				t.Fatalf("start session failed: %v", err)
			}
			if err == nil {
				if session.ID() == "" {
					t.Fatal("session id is empty")
				}
				if closeErr := session.Close(context.Background()); closeErr != nil {
					t.Fatalf("close session failed: %v", closeErr)
				}
			}
		})
	}
}
