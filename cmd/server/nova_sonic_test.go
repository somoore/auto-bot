package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNovaSonicBoardContextRefreshUsesApplicationDataRole(t *testing.T) {
	events := novaSonicBoardContextRefreshEvents(newKanbanBoard(), "prompt-1", "content-1")
	if len(events) != 3 {
		t.Fatalf("refresh event count = %d, want 3", len(events))
	}

	var start struct {
		Event map[string]struct {
			Role        string `json:"role"`
			Interactive bool   `json:"interactive"`
			Type        string `json:"type"`
		} `json:"event"`
	}
	if err := json.Unmarshal(events[0], &start); err != nil {
		t.Fatalf("unmarshal contentStart: %v", err)
	}
	contentStart, ok := start.Event["contentStart"]
	if !ok {
		t.Fatalf("first refresh event = %s, want contentStart", string(events[0]))
	}
	if contentStart.Role != "USER" {
		t.Fatalf("refresh role = %q, want USER so Bedrock does not see duplicate SYSTEM content", contentStart.Role)
	}
	if contentStart.Interactive {
		t.Fatal("refresh content is interactive; want application-supplied non-interactive data")
	}
	if contentStart.Type != "TEXT" {
		t.Fatalf("refresh type = %q, want TEXT", contentStart.Type)
	}

	var text struct {
		Event map[string]struct {
			Content string `json:"content"`
		} `json:"event"`
	}
	if err := json.Unmarshal(events[1], &text); err != nil {
		t.Fatalf("unmarshal textInput: %v", err)
	}
	content := text.Event["textInput"].Content
	for _, required := range []string{"Application-supplied", "reference data only", "Current sanitized Kanban board JSON"} {
		if !strings.Contains(content, required) {
			t.Fatalf("refresh content missing %q: %s", required, content)
		}
	}
}

func TestNovaSonicSessionInstructionsAvoidFilterTriggerTerms(t *testing.T) {
	instructions := newKanbanBoard().NovaSonicSessionInstructions()
	for _, blocked := range []string{"prompt injection", "malicious"} {
		if strings.Contains(strings.ToLower(instructions), blocked) {
			t.Fatalf("Nova Sonic instructions contain %q", blocked)
		}
	}
	for _, required := range []string{"Only live participant speech", "reference data only", "require confirmation"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("Nova Sonic instructions missing %q", required)
		}
	}
}

func TestBrowserLiveKitURLPrefersExplicitBrowserURL(t *testing.T) {
	t.Setenv("LIVEKIT_BROWSER_URL", "wss://voice.example.com")
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")

	req := httptest.NewRequest("GET", "http://localhost:3001/livekit-token", nil)
	if got := browserLiveKitURL(req); got != "wss://voice.example.com" {
		t.Fatalf("browserLiveKitURL = %q, want explicit browser URL", got)
	}
}

func TestBrowserLiveKitURLUsesIPv4LoopbackForLocalhost(t *testing.T) {
	t.Setenv("LIVEKIT_BROWSER_URL", "")
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")

	req := httptest.NewRequest("GET", "http://localhost:3001/livekit-token", nil)
	if got := browserLiveKitURL(req); got != "ws://127.0.0.1:7880" {
		t.Fatalf("browserLiveKitURL = %q, want IPv4 loopback URL", got)
	}
}

func TestContentSecurityPolicyAllowsLiveKitValidationOrigin(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("LIVEKIT_BROWSER_URL", "wss://voice.example.com")
	t.Setenv("LIVEKIT_URL", "ws://livekit:7880")

	policy := contentSecurityPolicy()
	for _, required := range []string{"connect-src", "http://127.0.0.1:7880", "http://localhost:7880", "https://voice.example.com"} {
		if !strings.Contains(policy, required) {
			t.Fatalf("contentSecurityPolicy missing %q: %s", required, policy)
		}
	}
	if strings.Contains(policy, "http://livekit:7880") {
		t.Fatalf("contentSecurityPolicy exposes docker-internal LiveKit origin: %s", policy)
	}
}

func TestUnwrapAudioREDReturnsPrimaryOpusPayload(t *testing.T) {
	payload := []byte{
		0x80 | 111,
		0x00,
		0x00,
		0x03,
		111,
		0xaa,
		0xbb,
		0xcc,
		0x11,
		0x22,
	}
	got, err := unwrapAudioRED(payload)
	if err != nil {
		t.Fatalf("unwrapAudioRED returned error: %v", err)
	}
	if string(got) != string([]byte{0x11, 0x22}) {
		t.Fatalf("primary payload = %#v, want %#v", got, []byte{0x11, 0x22})
	}
}

func TestUnwrapAudioREDRejectsMissingPrimaryData(t *testing.T) {
	if _, err := unwrapAudioRED([]byte{0x80 | 111, 0x00, 0x00, 0x03, 111, 0xaa, 0xbb, 0xcc}); err == nil {
		t.Fatal("unwrapAudioRED returned nil error for payload without primary data")
	}
}

func TestNovaSonicTextOutputUsesContentStartRoleAndStage(t *testing.T) {
	board := newKanbanBoard()
	app := newNovaSonicApp(board)
	app.speakerMu.Lock()
	app.activeSpeakers = []string{"scottmoore"}
	app.speakerMu.Unlock()

	app.handleContentStart(json.RawMessage(`{
		"contentId":"content-1",
		"type":"TEXT",
		"role":"USER",
		"additionalModelFields":"{\"generationStage\":\"FINAL\"}"
	}`))
	app.handleTextOutput(json.RawMessage(`{
		"contentId":"content-1",
		"content":"start the standup"
	}`))

	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lastTranscripts) != 1 {
		t.Fatalf("transcript count = %d, want 1", len(board.lastTranscripts))
	}
	got := board.lastTranscripts[0]
	if got.Role != "user" {
		t.Fatalf("transcript role = %q, want user", got.Role)
	}
	if got.Speaker != "scottmoore" {
		t.Fatalf("transcript speaker = %q, want scottmoore", got.Speaker)
	}
	if got.Text != "start the standup" {
		t.Fatalf("transcript text = %q, want start the standup", got.Text)
	}
}

func TestNovaSonicTextOutputSkipsSpeculativeAssistantPreview(t *testing.T) {
	board := newKanbanBoard()
	app := newNovaSonicApp(board)

	app.handleContentStart(json.RawMessage(`{
		"contentId":"content-2",
		"type":"TEXT",
		"role":"ASSISTANT",
		"additionalModelFields":"{\"generationStage\":\"SPECULATIVE\"}"
	}`))
	app.handleTextOutput(json.RawMessage(`{
		"contentId":"content-2",
		"content":"I might say this, but it is only a preview."
	}`))

	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lastTranscripts) != 0 {
		t.Fatalf("transcript count = %d, want 0 for speculative text", len(board.lastTranscripts))
	}
}
