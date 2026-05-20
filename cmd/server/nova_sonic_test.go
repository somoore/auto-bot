package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	for _, required := range []string{"For the room:", "every assistant message", "English-only follow-up fragments"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("Nova Sonic instructions missing multilingual guard %q", required)
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

func TestNovaSonicUpsampleUsesLinearInterpolation(t *testing.T) {
	got := upsample16kMonoTo48kStereo([]int16{0, 3000})
	want := []int16{0, 0, 1000, 1000, 2000, 2000, 3000, 3000, 3000, 3000, 3000, 3000}
	if len(got) != len(want) {
		t.Fatalf("upsampled length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upsampled[%d] = %d, want %d (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestNovaSonicDownsampleAveragesWindow(t *testing.T) {
	got := downsample48kStereoTo16kMono([]int16{
		300, 100,
		600, 200,
		900, 300,
	})
	if len(got) != 1 {
		t.Fatalf("downsampled length = %d, want 1", len(got))
	}
	if got[0] != 400 {
		t.Fatalf("downsampled sample = %d, want averaged sample 400", got[0])
	}
}

func TestNovaSonicOutputFramesPadPartialFrame(t *testing.T) {
	frames := novaSonicOutputFramesFromMono16(make([]int16, novaSonicFrameSize+1))
	if len(frames) != 2 {
		t.Fatalf("output frame count = %d, want 2", len(frames))
	}
	for i, frame := range frames {
		if len(frame) != roomAudioMixFrameSize {
			t.Fatalf("frame[%d] length = %d, want %d", i, len(frame), roomAudioMixFrameSize)
		}
	}
}

func TestNovaSonicOutputPacerCapsQueueAndReportsDrops(t *testing.T) {
	pacer := &novaSonicOutputPacer{
		stats: novaSonicOutputStats{
			PreRollFrames:  novaSonicOutputPreRollFrames,
			MaxQueueFrames: novaSonicOutputMaxQueue,
		},
	}
	pacer.EnqueueMono16(make([]int16, novaSonicFrameSize*(novaSonicOutputMaxQueue+10)))
	stats := pacer.Stats()
	if stats.QueueDepthFrames != novaSonicOutputMaxQueue {
		t.Fatalf("queue depth = %d, want cap %d", stats.QueueDepthFrames, novaSonicOutputMaxQueue)
	}
	if stats.DroppedFrames != 10 {
		t.Fatalf("dropped frames = %d, want 10", stats.DroppedFrames)
	}
}

func TestNovaSonicOutputPacerPreRollCanTimeOutForShortUtterance(t *testing.T) {
	pacer := &novaSonicOutputPacer{
		stats: novaSonicOutputStats{
			PreRollFrames:  novaSonicOutputPreRollFrames,
			MaxQueueFrames: novaSonicOutputMaxQueue,
		},
	}
	pacer.EnqueueMono16(make([]int16, novaSonicFrameSize))
	if frame := pacer.nextFrame(time.Now()); len(frame) != 0 {
		t.Fatalf("nextFrame returned %d samples before pre-roll timeout, want none", len(frame))
	}

	pacer.mu.Lock()
	firstFrame := pacer.firstFrame
	pacer.mu.Unlock()
	frame := pacer.nextFrame(firstFrame.Add(novaSonicOutputMaxPreRoll + time.Millisecond))
	if len(frame) != roomAudioMixFrameSize {
		t.Fatalf("nextFrame after timeout returned %d samples, want %d", len(frame), roomAudioMixFrameSize)
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

func TestNovaSonicTextOutputTranslatesAudioUserTranscriptForRoom(t *testing.T) {
	board := newKanbanBoard()
	app := newNovaSonicApp(board)
	app.speakerMu.Lock()
	app.activeSpeakers = []string{"scottmoore"}
	app.speakerMu.Unlock()
	model := useFakeAgentTranslationModel(t, `{"language":"es-DO","english_text":"I need to create a new task."}`)

	app.handleContentStart(json.RawMessage(`{
		"contentId":"content-es-user",
		"type":"TEXT",
		"role":"USER",
		"additionalModelFields":"{\"generationStage\":\"FINAL\"}"
	}`))
	app.handleTextOutput(json.RawMessage(`{
		"contentId":"content-es-user",
		"content":"Necesito crear una tarea."
	}`))

	if model.calls != 1 {
		t.Fatalf("translation model calls = %d, want 1", model.calls)
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lastTranscripts) != 1 {
		t.Fatalf("transcript count = %d, want 1", len(board.lastTranscripts))
	}
	got := board.lastTranscripts[0]
	if got.Text != "I need to create a new task." {
		t.Fatalf("working transcript text = %q, want English translation", got.Text)
	}
	if got.OriginalText != "Necesito crear una tarea." || got.TranslatedText != "I need to create a new task." {
		t.Fatalf("translation metadata = %#v, want original Spanish and English translation", got)
	}
	if got.Language != "es-do" || got.InputMode != "audio" {
		t.Fatalf("language/input mode = %q/%q, want es-do/audio", got.Language, got.InputMode)
	}
}

func TestNovaSonicAssistantTranscriptGetsEnglishFallbackForRoom(t *testing.T) {
	board := newKanbanBoard()
	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "es-DO",
		OriginalText:      "Necesito crear una tarea.",
		EnglishText:       "I need to create a new task.",
		TranslationStatus: "translated",
	})
	app := newNovaSonicApp(board)
	model := useFakeAgentTranslationModel(t, `{"language":"es-DO","english_text":"Perfect! I created the task."}`)

	app.handleContentStart(json.RawMessage(`{
		"contentId":"content-es-assistant",
		"type":"TEXT",
		"role":"ASSISTANT",
		"additionalModelFields":"{\"generationStage\":\"FINAL\"}"
	}`))
	app.handleTextOutput(json.RawMessage(`{
		"contentId":"content-es-assistant",
		"content":"Perfecto! He creado la tarea."
	}`))

	if model.calls != 1 {
		t.Fatalf("translation model calls = %d, want 1", model.calls)
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	if len(board.lastTranscripts) != 1 {
		t.Fatalf("transcript count = %d, want 1", len(board.lastTranscripts))
	}
	got := board.lastTranscripts[0]
	if got.Role != "assistant" {
		t.Fatalf("transcript role = %q, want assistant", got.Role)
	}
	if got.Text != "Perfect! I created the task." {
		t.Fatalf("working transcript text = %q, want English fallback", got.Text)
	}
	if got.OriginalText != "Perfecto! He creado la tarea." || got.TranslatedText != "Perfect! I created the task." {
		t.Fatalf("assistant translation metadata = %#v, want original Spanish and English fallback", got)
	}
	if got.Language != "es-do" || got.InputMode != "audio" {
		t.Fatalf("language/input mode = %q/%q, want es-do/audio", got.Language, got.InputMode)
	}
}

func TestNovaSonicResponseLanguageRefreshEventsResetEnglishTurn(t *testing.T) {
	events := novaSonicResponseLanguageRefreshEvents("prompt-1", "language-1", "Scott", normalizedMeetingText{
		OriginalText:      "hello",
		EnglishText:       "hello",
		Language:          "en",
		InputMode:         "audio",
		TranslationStatus: "not_needed",
	})
	if len(events) != 3 {
		t.Fatalf("refresh event count = %d, want 3", len(events))
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
	for _, want := range []string{"reply in English only", "Do not continue any previous non-English language", "not a participant request"} {
		if !strings.Contains(content, want) {
			t.Fatalf("English language refresh missing %q: %s", want, content)
		}
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

func useFakeAgentTranslationModel(t *testing.T, response string) *fakeChatTranslationModel {
	t.Helper()
	previous := agentOrchestrator
	model := &fakeChatTranslationModel{
		t:        t,
		response: []byte(response),
	}
	agentOrchestrator = &agentRunOrchestrator{model: model}
	t.Cleanup(func() { agentOrchestrator = previous })
	return model
}
