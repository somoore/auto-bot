package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeChatTranslationModel struct {
	t        *testing.T
	response []byte
	err      error
	calls    int
	prompts  []string
}

func (f *fakeChatTranslationModel) CompleteJSON(ctx context.Context, modelID string, system string, prompt string, maxTokens int) ([]byte, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func TestNormalizeMeetingTextEnglishNoop(t *testing.T) {
	model := &fakeChatTranslationModel{
		t:        t,
		response: []byte(`{"language":"es","english_text":"should not be used"}`),
	}

	got := normalizeMeetingText(context.Background(), "I finished the Jira sync work.", "en-US", model)

	if got.OriginalText != "I finished the Jira sync work." || got.EnglishText != got.OriginalText {
		t.Fatalf("normalized text = %#v, want original English unchanged", got)
	}
	if got.Language != "en-us" {
		t.Fatalf("language = %q, want en-us", got.Language)
	}
	if got.Translated {
		t.Fatalf("translated = true, want false")
	}
	if got.TranslationStatus != "not_needed" {
		t.Fatalf("translation status = %q, want not_needed", got.TranslationStatus)
	}
	if model.calls != 0 {
		t.Fatalf("translation model calls = %d, want 0", model.calls)
	}
}

func TestNormalizeMeetingTextExplicitSpanishHintUsesModel(t *testing.T) {
	model := &fakeChatTranslationModel{
		t:        t,
		response: []byte(`{"language":"es-DO","english_text":"I finished EMAL-14 and need review."}`),
	}

	got := normalizeMeetingText(context.Background(), "Termine EMAL-14 y necesito revision.", "es-DO", model)

	if model.calls != 1 {
		t.Fatalf("translation model calls = %d, want 1", model.calls)
	}
	if got.OriginalText != "Termine EMAL-14 y necesito revision." {
		t.Fatalf("original text = %q, want Spanish source", got.OriginalText)
	}
	if got.EnglishText != "I finished EMAL-14 and need review." {
		t.Fatalf("English text = %q, want translated text", got.EnglishText)
	}
	if got.Language != "es-do" || !got.Translated || got.TranslationStatus != "translated" {
		t.Fatalf("normalization metadata = %#v, want translated es-do", got)
	}
	if len(model.prompts) != 1 || !strings.Contains(model.prompts[0], "Language hint: es-do") {
		t.Fatalf("translation prompt = %#v, want sanitized Spanish hint", model.prompts)
	}
}

func TestNormalizeMeetingTextAutoDetectedSpanishAndIcelandicRequireTranslation(t *testing.T) {
	tests := []struct {
		name     string
		original string
		response string
		language string
		english  string
	}{
		{
			name:     "spanish",
			original: "Hola, necesito mover EMAL-14 a en progreso.",
			response: `{"language":"es","english_text":"Hi, I need to move EMAL-14 to in progress."}`,
			language: "es",
			english:  "Hi, I need to move EMAL-14 to in progress.",
		},
		{
			name:     "icelandic",
			original: "Ég þarf að færa EMAL-15 í lokið.",
			response: `{"language":"is","english_text":"I need to move EMAL-15 to done."}`,
			language: "is",
			english:  "I need to move EMAL-15 to done.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &fakeChatTranslationModel{
				t:        t,
				response: []byte(tt.response),
			}

			got := normalizeMeetingText(context.Background(), tt.original, "auto", model)

			if model.calls != 1 {
				t.Fatalf("translation model calls = %d, want 1", model.calls)
			}
			if got.EnglishText != tt.english {
				t.Fatalf("English text = %q, want %q", got.EnglishText, tt.english)
			}
			if got.OriginalText != tt.original {
				t.Fatalf("original text = %q, want %q", got.OriginalText, tt.original)
			}
			if got.Language != tt.language || !got.Translated || got.TranslationStatus != "translated" {
				t.Fatalf("normalization metadata = %#v, want translated %s", got, tt.language)
			}
		})
	}
}

func TestNormalizeMeetingTextBedrockUnavailableFallback(t *testing.T) {
	got := normalizeMeetingText(context.Background(), "Hola, necesito ayuda con EMAL-14.", "auto", nil)

	if got.OriginalText != "Hola, necesito ayuda con EMAL-14." || got.EnglishText != got.OriginalText {
		t.Fatalf("normalized text = %#v, want untranslated fallback", got)
	}
	if got.Language != "auto" {
		t.Fatalf("language = %q, want auto", got.Language)
	}
	if got.Translated {
		t.Fatalf("translated = true, want false")
	}
	if got.TranslationStatus != "unavailable" {
		t.Fatalf("translation status = %q, want unavailable", got.TranslationStatus)
	}
	if !strings.Contains(got.TranslationWarning, "Bedrock translation model is not configured") {
		t.Fatalf("translation warning = %q, want Bedrock unavailable message", got.TranslationWarning)
	}
}

func TestNormalizeMeetingTextTranslationErrorFallback(t *testing.T) {
	model := &fakeChatTranslationModel{
		t:   t,
		err: errors.New("bedrock offline"),
	}

	got := normalizeMeetingText(context.Background(), "Necesito cerrar EMAL-22.", "es", model)

	if model.calls != 1 {
		t.Fatalf("translation model calls = %d, want 1", model.calls)
	}
	if got.EnglishText != got.OriginalText {
		t.Fatalf("English text = %q, want original fallback %q", got.EnglishText, got.OriginalText)
	}
	if got.TranslationStatus != "unavailable" || !strings.Contains(got.TranslationWarning, "bedrock offline") {
		t.Fatalf("fallback metadata = %#v, want unavailable with model error", got)
	}
}

func TestRecordTranscriptEntryPreservesChatTranslationMetadata(t *testing.T) {
	board := newKanbanBoard()
	board.RecordTranscriptEntry(transcriptEntry{
		Role:           "user",
		Speaker:        "Ana",
		Text:           "I finished EMAL-14 and need review.",
		OriginalText:   "Termine EMAL-14 y necesito revision.",
		TranslatedText: "I finished EMAL-14 and need review.",
		Language:       "ES-DO",
		InputMode:      "CHAT",
		CreatedAt:      "2026-05-19T10:00:00Z",
	})

	board.mu.Lock()
	if len(board.lastTranscripts) != 1 {
		board.mu.Unlock()
		t.Fatalf("transcript count = %d, want 1", len(board.lastTranscripts))
	}
	got := board.lastTranscripts[0]
	evidence := board.transcriptEvidenceLocked("")
	board.mu.Unlock()

	if got.OriginalText != "Termine EMAL-14 y necesito revision." {
		t.Fatalf("original text = %q, want Spanish source", got.OriginalText)
	}
	if got.TranslatedText != "I finished EMAL-14 and need review." {
		t.Fatalf("translated text = %q, want English translation", got.TranslatedText)
	}
	if got.Text != "I finished EMAL-14 and need review." {
		t.Fatalf("working text = %q, want English text", got.Text)
	}
	if got.Language != "es-do" || got.InputMode != "chat" {
		t.Fatalf("metadata language/input_mode = %q/%q, want es-do/chat", got.Language, got.InputMode)
	}
	if len(evidence.Entries) != 1 {
		t.Fatalf("evidence entries = %d, want 1", len(evidence.Entries))
	}
	if evidence.Entries[0].OriginalText != got.OriginalText || evidence.Entries[0].TranslatedText != got.TranslatedText || evidence.Entries[0].InputMode != "chat" || evidence.Entries[0].Language != "es-do" {
		t.Fatalf("evidence entry = %#v, want preserved transcript metadata", evidence.Entries[0])
	}
	if !strings.Contains(evidence.Summary, "I finished EMAL-14 and need review.") {
		t.Fatalf("evidence summary = %q, want English working text", evidence.Summary)
	}
	if strings.Contains(evidence.Summary, "Termine EMAL-14") {
		t.Fatalf("evidence summary = %q, should not use original non-English text", evidence.Summary)
	}
}

func TestMeetingChatPromptRequiresBilingualReplyForTranslatedInput(t *testing.T) {
	prompt := meetingChatPrompt("Scott", normalizedMeetingText{
		OriginalText:      "Necesito crear una tarea nueva.",
		EnglishText:       "I need to create a new task.",
		Language:          "es-DO",
		Translated:        true,
		TranslationStatus: "translated",
	})

	for _, want := range []string{"reply_language", "es-do", "For the room:", "every assistant message", "English-only follow-up fragments", "Use English for all Jira"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestResponseLanguagePolicyAnnotatesToolResult(t *testing.T) {
	board := newKanbanBoard()
	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "es-DO",
		OriginalText:      "Necesito crear una tarea nueva.",
		EnglishText:       "I need to create a new task.",
		TranslationStatus: "translated",
	})

	result := map[string]any{"ok": true, "card_id": "EMAL-31"}
	board.annotateResponseLanguagePolicy(result)

	raw := mustMarshalJSON(modelSafeToolResult(result))
	lowered := strings.ToLower(raw)
	for _, want := range []string{"response_language_policy", "es-do", "For the room:", "every assistant message", "English-only follow-up fragment"} {
		if !strings.Contains(lowered, strings.ToLower(want)) {
			t.Fatalf("model-safe result missing %q: %s", want, raw)
		}
	}
}
