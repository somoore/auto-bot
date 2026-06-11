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

func TestNormalizeMeetingTextAutoEnglishNoop(t *testing.T) {
	model := &fakeChatTranslationModel{
		t:        t,
		response: []byte(`{"language":"en","english_text":"should not be used"}`),
	}

	got := normalizeMeetingText(context.Background(), "I need to move EMAL-14 to in progress.", "auto", model)

	if got.OriginalText != "I need to move EMAL-14 to in progress." || got.EnglishText != got.OriginalText {
		t.Fatalf("normalized text = %#v, want original English unchanged", got)
	}
	if got.Language != "en" {
		t.Fatalf("language = %q, want en", got.Language)
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

func TestNormalizeMeetingTextAutoEnglishQuotedTitleNoop(t *testing.T) {
	model := &fakeChatTranslationModel{
		t:        t,
		response: []byte(`{"language":"es","english_text":"should not be used"}`),
	}

	for _, original := range []string{
		"Create a task named 'say hello to annie' and assign it to me.",
		"I'm moving EMAL-38 to done.",
	} {
		got := normalizeMeetingText(context.Background(), original, "auto", model)

		if got.OriginalText != original || got.EnglishText != original {
			t.Fatalf("normalized text = %#v, want original English unchanged", got)
		}
		if got.Language != "en" || got.Translated || got.TranslationStatus != "not_needed" {
			t.Fatalf("normalization metadata = %#v, want English not_needed", got)
		}
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

func TestNormalizeMeetingTextQuotedEnglishTitleDoesNotHideNonEnglishCommand(t *testing.T) {
	model := &fakeChatTranslationModel{
		t:        t,
		response: []byte(`{"language":"de","english_text":"Create a new task named 'say hello to annie'. Assign it to me."}`),
	}

	original := "Erstelle eine neue Aufgabe mit dem Namen 'say hello to annie'. Weise sie mir zu"
	got := normalizeMeetingText(context.Background(), original, "auto", model)

	if model.calls != 1 {
		t.Fatalf("translation model calls = %d, want 1", model.calls)
	}
	if got.OriginalText != original {
		t.Fatalf("original text = %q, want source text preserved", got.OriginalText)
	}
	if got.EnglishText != "Create a new task named 'say hello to annie'. Assign it to me." {
		t.Fatalf("English text = %q, want canonical English command", got.EnglishText)
	}
	if got.Language != "de" || !got.Translated || got.TranslationStatus != "translated" {
		t.Fatalf("normalization metadata = %#v, want translated non-English command", got)
	}
}

func TestNormalizeMeetingTextAutoDetectedNonEnglishRequiresTranslation(t *testing.T) {
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
		{
			name:     "turkish",
			original: "Merhaba, yeni bir gorev olusturmam gerekiyor.",
			response: `{"language":"tr","english_text":"Hello, I need to create a new task."}`,
			language: "tr",
			english:  "Hello, I need to create a new task.",
		},
		{
			name:     "hindi",
			original: "मुझे EMAL-15 को done में ले जाना है।",
			response: `{"language":"hi","english_text":"I need to move EMAL-15 to done."}`,
			language: "hi",
			english:  "I need to move EMAL-15 to done.",
		},
		{
			name:     "finnish",
			original: "Tarvitsen uuden tehtavan CrowdStrike-skannausta varten.",
			response: `{"language":"fi","english_text":"I need a new task for CrowdStrike scanning."}`,
			language: "fi",
			english:  "I need a new task for CrowdStrike scanning.",
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

	for _, want := range []string{"reply_language", "es-do", "For the room:", "every assistant message", "English-only follow-up fragments", "english_text as the canonical board/Jira command", "Default every create"} {
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

func TestResponseLanguagePolicyDoesNotSwitchOnShortConfirmationWithPendingAction(t *testing.T) {
	board := newKanbanBoard()
	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "es-DO",
		OriginalText:      "Necesito crear una tarea nueva.",
		EnglishText:       "I need to create a new task.",
		TranslationStatus: "translated",
	})
	board.pendingConfirmations["confirm-1"] = pendingConfirmation{ConfirmationID: "confirm-1"}

	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "pt",
		OriginalText:      "sim",
		EnglishText:       "yes",
		TranslationStatus: "translated",
	})

	policy := board.activeResponseLanguagePolicy()
	if policy == nil || policy.SourceLanguage != "es-do" || policy.ReplyLanguage != "es-do" {
		t.Fatalf("response language policy = %#v, want existing non-English policy preserved", policy)
	}
}

func TestResponseLanguagePolicyClearsShortConfirmationWithoutPendingAction(t *testing.T) {
	board := newKanbanBoard()
	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "es-DO",
		OriginalText:      "Necesito crear una tarea nueva.",
		EnglishText:       "I need to create a new task.",
		TranslationStatus: "translated",
	})

	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "pt",
		OriginalText:      "sim",
		EnglishText:       "yes",
		TranslationStatus: "translated",
	})

	if policy := board.activeResponseLanguagePolicy(); policy != nil {
		t.Fatalf("response language policy = %#v, want cleared for confirmation-only input without pending action", policy)
	}
}

func TestMeetingResponseLanguagePromptResetsEnglishAudioTurn(t *testing.T) {
	prompt := meetingResponseLanguagePrompt("Scott", normalizedMeetingText{
		OriginalText:      "hello",
		EnglishText:       "hello",
		Language:          "en",
		InputMode:         "audio",
		TranslationStatus: "not_needed",
	})

	for _, want := range []string{"reply in English only", "Do not continue any previous non-English language", "Use the bilingual native-language plus 'For the room:' English pattern only when the latest live participant input is non-English"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("English policy prompt missing %q: %s", want, prompt)
		}
	}
}

func TestMeetingResponseLanguagePromptTreatsShortConfirmationAsLanguageAmbiguous(t *testing.T) {
	prompt := meetingResponseLanguagePrompt("Scott", normalizedMeetingText{
		OriginalText:      "sim",
		EnglishText:       "yes",
		Language:          "pt",
		InputMode:         "audio",
		Translated:        true,
		TranslationStatus: "translated",
	})

	for _, want := range []string{"Short confirmation tokens are language-ambiguous", "Do not infer, start, or switch response languages", "stay silent or call do_nothing", "default to concise English", "Do not use markdown headings"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("confirmation-only policy prompt missing %q: %s", want, prompt)
		}
	}
}

func TestMeetingResponseLanguagePromptRequiresNativeAndRoomEnglishForNonEnglishAudio(t *testing.T) {
	prompt := meetingResponseLanguagePrompt("Aylin", normalizedMeetingText{
		OriginalText:      "Merhaba, yeni bir gorev olusturmam gerekiyor.",
		EnglishText:       "Hello, I need to create a new task.",
		Language:          "tr",
		InputMode:         "audio",
		Translated:        true,
		TranslationStatus: "translated",
	})

	for _, want := range []string{"first answer Aylin in tr", "For the room:", "provide the English meaning or outcome", "speak both the native-language reply", "Do not say or imply that you can only respond in English", "english_text as the canonical board/Jira command"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("non-English policy prompt missing %q: %s", want, prompt)
		}
	}
}

func TestShouldClearResponseLanguagePolicyAfterConfirmationToolResult(t *testing.T) {
	if !shouldClearResponseLanguagePolicyAfterToolResult("confirm_action", map[string]any{"ok": true}) {
		t.Fatal("confirm_action should clear response language policy after its tool result is sent")
	}
	if !shouldClearResponseLanguagePolicyAfterToolResult("assign_ticket", map[string]any{"confirmed": true}) {
		t.Fatal("confirmed tool result should clear response language policy after its tool result is sent")
	}
	if shouldClearResponseLanguagePolicyAfterToolResult("create_ticket", map[string]any{"ok": true}) {
		t.Fatal("ordinary tool result should not clear response language policy")
	}
}

func TestCurrentResponseLanguageInstructionDefaultsToEnglishAfterClear(t *testing.T) {
	board := newKanbanBoard()
	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "es-DO",
		OriginalText:      "Necesito crear una tarea nueva.",
		EnglishText:       "I need to create a new task.",
		TranslationStatus: "translated",
	})
	board.UpdateResponseLanguagePolicy("Scott", normalizedMeetingText{
		Language:          "en",
		OriginalText:      "why are you speaking Spanish?",
		EnglishText:       "why are you speaking Spanish?",
		TranslationStatus: "not_needed",
	})

	instruction := board.currentResponseLanguageInstruction()
	for _, want := range []string{"Current response-language policy: English", "Do not carry over a non-English language"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("English current policy missing %q: %s", want, instruction)
		}
	}
}
