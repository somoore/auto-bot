package main

import (
	"strings"
	"testing"
)

func TestRealtimeModelDefaultsToGPTRealtime2(t *testing.T) {
	resetVoiceModelsForTest(t)
	t.Setenv("OPENAI_REALTIME_MODEL", "")

	got := realtimeModel()
	if got != defaultRealtimeModel {
		t.Fatalf("realtimeModel() = %q, want %q", got, defaultRealtimeModel)
	}
	if err := validateRealtimeConversationModel(got); err != nil {
		t.Fatalf("default realtime model should be valid: %v", err)
	}
	if !usesAdvancedCommandProfile(got) {
		t.Fatal("gpt-realtime-2 should use the advanced command profile")
	}
}

func TestRealtimeTranscriptionDefaultsToGPTRealtimeWhisper(t *testing.T) {
	resetVoiceModelsForTest(t)
	t.Setenv("OPENAI_REALTIME_TRANSCRIPTION_MODEL", "")

	got := realtimeTranscriptionModel()
	if got != defaultRealtimeTranscriptionModel {
		t.Fatalf("realtimeTranscriptionModel() = %q, want %q", got, defaultRealtimeTranscriptionModel)
	}
	if err := validateRealtimeTranscriptionModel(got); err != nil {
		t.Fatalf("default transcription model should be valid: %v", err)
	}
}

func TestRealtimeConversationRejectsSpecializedNonToolModels(t *testing.T) {
	for _, model := range []string{"gpt-realtime-whisper", "gpt-realtime-translate"} {
		t.Run(model, func(t *testing.T) {
			err := validateRealtimeConversationModel(model)
			if err == nil {
				t.Fatal("expected specialized non-tooling model to be rejected")
			}
			if !strings.Contains(err.Error(), "cannot run Jira/GitHub tools") {
				t.Fatalf("error = %q, want tool-calling guidance", err.Error())
			}
		})
	}
}

func TestRealtimeTranscriptionRejectsConversationAndTranslationModels(t *testing.T) {
	for _, model := range []string{"gpt-realtime-2", "gpt-realtime-translate"} {
		t.Run(model, func(t *testing.T) {
			err := validateRealtimeTranscriptionModel(model)
			if err == nil {
				t.Fatal("expected non-transcription model to be rejected")
			}
			if !strings.Contains(err.Error(), "not a transcription model") {
				t.Fatalf("error = %q, want transcription guidance", err.Error())
			}
		})
	}
}

func TestRealtimeTranscriptionRejectsLatestAliases(t *testing.T) {
	model := "gpt-4o-transcribe-" + "lat" + "est"
	err := validateRealtimeTranscriptionModel(model)
	if err == nil {
		t.Fatal("expected floating latest alias to be rejected")
	}
	if !strings.Contains(err.Error(), "floating latest alias") {
		t.Fatalf("error = %q, want latest alias guidance", err.Error())
	}
}

func TestSessionConfigUsesRealtimeWhisperByDefault(t *testing.T) {
	resetVoiceModelsForTest(t)
	t.Setenv("OPENAI_REALTIME_TRANSCRIPTION_MODEL", "")

	board := newKanbanBoard()
	app := newKanbanBoardApp(board)
	session := app.sessionConfig(defaultRealtimeModel)

	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("audio config has type %T", session["audio"])
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("audio.input config has type %T", audio["input"])
	}
	transcription, ok := input["transcription"].(map[string]any)
	if !ok {
		t.Fatalf("audio.input.transcription config has type %T", input["transcription"])
	}
	if got := transcription["model"]; got != defaultRealtimeTranscriptionModel {
		t.Fatalf("transcription model = %v, want %s", got, defaultRealtimeTranscriptionModel)
	}
}

func TestOpenAIRealtimeModelProfilesCoverNewAudioModels(t *testing.T) {
	want := map[string]bool{
		"gpt-realtime-2":         false,
		"gpt-realtime-1.5":       false,
		"gpt-realtime-mini":      false,
		"gpt-realtime-whisper":   false,
		"gpt-realtime-translate": false,
	}
	for _, profile := range openAIRealtimeModelProfiles() {
		if _, ok := want[profile.Model]; ok {
			want[profile.Model] = true
		}
	}
	for model, found := range want {
		if !found {
			t.Fatalf("missing OpenAI realtime profile for %s", model)
		}
	}
}
