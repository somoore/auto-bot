package evaluation_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type fixtureHeader struct {
	SchemaVersion int    `json:"schema_version"`
	FixtureKind   string `json:"fixture_kind"`
	ID            string `json:"id"`
}

type meetingFixture struct {
	SchemaVersion int    `json:"schema_version"`
	FixtureKind   string `json:"fixture_kind"`
	ID            string `json:"id"`
	Description   string `json:"description"`
	Meeting       struct {
		InitialType  string        `json:"initial_type"`
		Host         string        `json:"host"`
		Participants []participant `json:"participants"`
	} `json:"meeting"`
	Events   []meetingEvent `json:"events"`
	Expected expectation    `json:"expected"`
}

type participant struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	ExpectedToSpeak bool   `json:"expected_to_speak"`
}

type meetingEvent struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Speaker         string         `json:"speaker,omitempty"`
	Speakers        []string       `json:"speakers,omitempty"`
	Text            string         `json:"text,omitempty"`
	Tags            []string       `json:"tags,omitempty"`
	DurationSeconds int            `json:"duration_seconds,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type expectation struct {
	AccessControl           accessControl           `json:"access_control"`
	MeetingTypeTransitions  []meetingTypeTransition `json:"meeting_type_transitions"`
	MeetingControlCenter    meetingControlCenter    `json:"meeting_control_center"`
	VoiceReliabilitySignals []string                `json:"voice_reliability_signals"`
	FailureStates           []failureState          `json:"failure_states"`
	JiraActions             []jiraAction            `json:"jira_actions"`
	ForbiddenActions        []forbiddenAction       `json:"forbidden_actions"`
	Confirmations           []confirmation          `json:"confirmations"`
	Extractions             []extraction            `json:"extractions"`
	Recap                   recapExpectation        `json:"recap"`
	ConfidenceExplanations  []confidenceExplanation `json:"confidence_explanations"`
	PostMeetingIntelligence *postMeetingExpectation `json:"post_meeting_intelligence,omitempty"`
}

type accessControl struct {
	RequiresRandomCode  bool   `json:"requires_random_code"`
	CodeEntropyBitsMin  int    `json:"code_entropy_bits_min"`
	WrongCodeRejected   bool   `json:"wrong_code_rejected"`
	CodeScopedToMeeting bool   `json:"code_scoped_to_meeting"`
	HostCanRegenerate   bool   `json:"host_can_regenerate"`
	ParticipantJoinPath string `json:"participant_join_path"`
}

type meetingTypeTransition struct {
	From           string `json:"from"`
	To             string `json:"to"`
	TriggerEventID string `json:"trigger_event_id"`
}

type meetingControlCenter struct {
	Spoken               []string     `json:"spoken"`
	NotSpoken            []string     `json:"not_spoken"`
	Blockers             []string     `json:"blockers"`
	Decisions            []string     `json:"decisions"`
	ActionItems          []actionItem `json:"action_items"`
	PendingConfirmations []string     `json:"pending_confirmations"`
	JiraMutations        []jiraAction `json:"jira_mutations"`
}

type actionItem struct {
	Owner string `json:"owner"`
	Text  string `json:"text"`
	Due   string `json:"due,omitempty"`
}

type failureState struct {
	Key     string `json:"key"`
	Message string `json:"message"`
	Signal  string `json:"signal"`
}

type jiraAction struct {
	Tool            string `json:"tool"`
	IssueKey        string `json:"issue_key,omitempty"`
	Risk            string `json:"risk"`
	MustExecute     bool   `json:"must_execute"`
	RequiresConfirm bool   `json:"requires_confirmation"`
}

type forbiddenAction struct {
	Tool          string `json:"tool"`
	Reason        string `json:"reason"`
	SourceEventID string `json:"source_event_id"`
	Expected      string `json:"expected"`
}

type confirmation struct {
	Tool           string `json:"tool"`
	IssueKey       string `json:"issue_key,omitempty"`
	Risk           string `json:"risk"`
	Required       bool   `json:"required"`
	PromptContains string `json:"prompt_contains"`
}

type extraction struct {
	Type     string `json:"type"`
	IssueKey string `json:"issue_key,omitempty"`
	Owner    string `json:"owner,omitempty"`
	Value    string `json:"value"`
	SourceID string `json:"source_event_id"`
}

type recapExpectation struct {
	SlackReadySummary   bool     `json:"slack_ready_summary"`
	JiraChangesSummary  bool     `json:"jira_changes_summary"`
	Blockers            []string `json:"blockers"`
	ActionItemsByOwner  []string `json:"action_items_by_owner"`
	UnresolvedQuestions []string `json:"unresolved_questions"`
	ChangedSinceStart   []string `json:"changed_since_start"`
}

type confidenceExplanation struct {
	Type     string `json:"type"`
	Example  string `json:"example"`
	Required bool   `json:"required"`
}

type postMeetingExpectation struct {
	PageRequired        bool     `json:"page_required"`
	ArchivesReport      bool     `json:"archives_report"`
	SlackCopy           bool     `json:"slack_copy"`
	TranscriptEvidence  bool     `json:"transcript_evidence"`
	GitHubContext       bool     `json:"github_context"`
	SetupReadiness      []string `json:"setup_readiness"`
	Observability       []string `json:"observability"`
	SprintSignals       []string `json:"sprint_signals"`
	ReportEndpoint      string   `json:"report_endpoint"`
	ArchiveListEndpoint string   `json:"archive_list_endpoint"`
}

type audioManifest struct {
	SchemaVersion     int          `json:"schema_version"`
	FixtureKind       string       `json:"fixture_kind"`
	ID                string       `json:"id"`
	FixtureID         string       `json:"fixture_id"`
	RequiredBehaviors []string     `json:"required_behaviors"`
	RequiredSilenceMS int          `json:"required_silence_ms"`
	Tracks            []audioTrack `json:"tracks"`
	Events            []audioEvent `json:"events"`
}

type audioTrack struct {
	ParticipantID string         `json:"participant_id"`
	Segments      []audioSegment `json:"segments"`
}

type audioSegment struct {
	ID                  string `json:"id"`
	StartMS             int    `json:"start_ms"`
	EndMS               int    `json:"end_ms"`
	Text                string `json:"text"`
	InterruptsSegmentID string `json:"interrupts_segment_id,omitempty"`
}

type audioEvent struct {
	Type          string `json:"type"`
	ParticipantID string `json:"participant_id"`
	AtMS          int    `json:"at_ms"`
}

type awsChecklist struct {
	SchemaVersion int                `json:"schema_version"`
	FixtureKind   string             `json:"fixture_kind"`
	ID            string             `json:"id"`
	Areas         []awsChecklistArea `json:"areas"`
}

type awsChecklistArea struct {
	ID    string             `json:"id"`
	Name  string             `json:"name"`
	Items []awsChecklistItem `json:"items"`
}

type awsChecklistItem struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Command          string `json:"command"`
	EvidenceRequired string `json:"evidence_required"`
	PassCriteria     string `json:"pass_criteria"`
}

type extensionContractFixture struct {
	SchemaVersion          int      `json:"schema_version"`
	FixtureKind            string   `json:"fixture_kind"`
	ID                     string   `json:"id"`
	Description            string   `json:"description"`
	RequiredVoiceProviders []string `json:"required_voice_providers"`
	RequiredConnectors     []string `json:"required_connectors"`
	RequiredModelProviders []string `json:"required_model_providers"`
	RequiredContracts      []string `json:"required_contracts"`
	QualityGates           []string `json:"quality_gates"`
}

type evaluationResult struct {
	FixtureID               string                  `json:"fixture_id"`
	Behaviors               []string                `json:"behaviors"`
	JiraActions             []jiraAction            `json:"jira_actions"`
	ForbiddenActions        []forbiddenAction       `json:"forbidden_actions"`
	Confirmations           []confirmation          `json:"confirmations"`
	Extractions             []extraction            `json:"extractions"`
	Recap                   recapExpectation        `json:"recap"`
	VoiceReliabilitySignals []string                `json:"voice_reliability_signals"`
	MeetingControlCenter    meetingControlCenter    `json:"meeting_control_center"`
	FailureStates           []failureState          `json:"failure_states"`
	ConfidenceExplanations  []confidenceExplanation `json:"confidence_explanations"`
	PostMeetingIntelligence *postMeetingExpectation `json:"post_meeting_intelligence,omitempty"`
}

func TestMeetingEvaluationFixturesCoverRequiredScenarios(t *testing.T) {
	fixtures := loadMeetingFixtures(t)

	requiredTags := []string{
		"host_code_required",
		"meeting_type_switch",
		"interruption",
		"overlap",
		"silence",
		"reconnect",
		"late_join",
		"risky_jira_confirmation",
		"prompt_injection",
	}
	coveredTags := map[string]bool{}
	coveredMeetingTypes := map[string]bool{}
	allowedMeetingTypes := map[string]bool{
		"general":       true,
		"standup":       true,
		"one_on_one":    true,
		"sprint_review": true,
		"open_ended":    true,
	}

	for _, fixture := range fixtures {
		if fixture.SchemaVersion != 1 {
			t.Fatalf("%s schema_version = %d, want 1", fixture.ID, fixture.SchemaVersion)
		}
		if fixture.ID == "" || fixture.Description == "" {
			t.Fatalf("fixture has missing id/description: %#v", fixture)
		}
		if !allowedMeetingTypes[fixture.Meeting.InitialType] {
			t.Fatalf("%s initial meeting type = %q", fixture.ID, fixture.Meeting.InitialType)
		}
		if count := len(fixture.Meeting.Participants); count < 2 || count > 4 {
			t.Fatalf("%s participants = %d, want 2-4", fixture.ID, count)
		}
		coveredMeetingTypes[fixture.Meeting.InitialType] = true
		for _, transition := range fixture.Expected.MeetingTypeTransitions {
			if !allowedMeetingTypes[transition.From] || !allowedMeetingTypes[transition.To] {
				t.Fatalf("%s transition has invalid meeting type: %#v", fixture.ID, transition)
			}
			coveredMeetingTypes[transition.From] = true
			coveredMeetingTypes[transition.To] = true
		}
		for _, event := range fixture.Events {
			if event.ID == "" || event.Type == "" {
				t.Fatalf("%s event missing id/type: %#v", fixture.ID, event)
			}
			for _, tag := range event.Tags {
				coveredTags[tag] = true
			}
		}
	}

	for _, tag := range requiredTags {
		if !coveredTags[tag] {
			t.Fatalf("meeting fixture corpus does not cover required behavior tag %q", tag)
		}
	}
	for meetingType := range allowedMeetingTypes {
		if !coveredMeetingTypes[meetingType] {
			t.Fatalf("meeting fixture corpus does not cover meeting type %q", meetingType)
		}
	}
}

func TestEvaluationFixturesDefineGradeableOutputs(t *testing.T) {
	fixtures := loadMeetingFixtures(t)

	requiredSignals := []string{
		"user_mic_detected",
		"livekit_connected",
		"nova_sonic_connected",
		"bedrock_stream_active",
		"transcription_flowing",
		"jira_reachable",
		"agent_participant_present",
	}
	requiredFailureMessages := map[string]string{
		"aws_credentials_expired":          "AWS credentials expired",
		"nova_agent_not_in_room":           "Nova agent not in room",
		"mic_permission_blocked":           "Mic permission blocked",
		"jira_write_rejected_by_scope":     "Jira write rejected by scope",
		"livekit_connected_no_audio_track": "LiveKit connected but no audio track published",
	}
	requiredConfidenceTypes := []string{"issue_match", "risky_confirmation", "prompt_injection_noop"}
	requiredRecapFields := []string{"blockers", "action_items_by_owner", "unresolved_questions", "changed_since_start"}

	foundCorrectJiraAction := false
	foundPromptInjectionBlock := false
	foundRiskyConfirmation := false
	foundExtraction := map[string]bool{}
	foundSignal := map[string]bool{}
	foundFailure := map[string]string{}
	foundConfidence := map[string]bool{}
	recapFields := map[string]bool{}
	foundAccessControl := false
	foundPostMeetingIntelligence := false

	for _, fixture := range fixtures {
		ac := fixture.Expected.AccessControl
		if ac.RequiresRandomCode && ac.CodeEntropyBitsMin >= 72 && ac.WrongCodeRejected && ac.CodeScopedToMeeting && ac.ParticipantJoinPath != "" {
			foundAccessControl = true
		}
		for _, signal := range fixture.Expected.VoiceReliabilitySignals {
			foundSignal[signal] = true
		}
		for _, state := range fixture.Expected.FailureStates {
			foundFailure[state.Key] = state.Message
		}
		for _, action := range fixture.Expected.JiraActions {
			if action.MustExecute && action.Tool != "" && action.Risk != "" {
				foundCorrectJiraAction = true
			}
		}
		for _, action := range fixture.Expected.ForbiddenActions {
			if action.Reason == "prompt_injection" && action.Expected == "no_tool_call" {
				foundPromptInjectionBlock = true
			}
		}
		for _, confirmation := range fixture.Expected.Confirmations {
			if confirmation.Required && (confirmation.Risk == "medium" || confirmation.Risk == "high") && confirmation.PromptContains != "" {
				foundRiskyConfirmation = true
			}
		}
		for _, extraction := range fixture.Expected.Extractions {
			foundExtraction[extraction.Type] = true
		}
		for _, explanation := range fixture.Expected.ConfidenceExplanations {
			if explanation.Required && explanation.Example != "" {
				foundConfidence[explanation.Type] = true
			}
		}
		if pmi := fixture.Expected.PostMeetingIntelligence; pmi != nil {
			if pmi.PageRequired && pmi.ArchivesReport && pmi.SlackCopy && pmi.TranscriptEvidence &&
				pmi.ReportEndpoint != "" && pmi.ArchiveListEndpoint != "" &&
				len(pmi.SetupReadiness) > 0 && len(pmi.Observability) > 0 && len(pmi.SprintSignals) > 0 {
				foundPostMeetingIntelligence = true
			}
		}
		recap := fixture.Expected.Recap
		if recap.SlackReadySummary {
			recapFields["slack_ready_summary"] = true
		}
		if recap.JiraChangesSummary {
			recapFields["jira_changes_summary"] = true
		}
		if len(recap.Blockers) > 0 {
			recapFields["blockers"] = true
		}
		if len(recap.ActionItemsByOwner) > 0 {
			recapFields["action_items_by_owner"] = true
		}
		if len(recap.UnresolvedQuestions) > 0 {
			recapFields["unresolved_questions"] = true
		}
		if len(recap.ChangedSinceStart) > 0 {
			recapFields["changed_since_start"] = true
		}
		requireControlCenterExpectation(t, fixture.ID, fixture.Expected.MeetingControlCenter)
	}

	if !foundAccessControl {
		t.Fatal("fixtures do not define a gradeable random-code host/participant access-control expectation")
	}
	for _, signal := range requiredSignals {
		if !foundSignal[signal] {
			t.Fatalf("fixtures do not define voice reliability signal %q", signal)
		}
	}
	for key, want := range requiredFailureMessages {
		if got := foundFailure[key]; got != want {
			t.Fatalf("failure state %q = %q, want %q", key, got, want)
		}
	}
	if !foundCorrectJiraAction {
		t.Fatal("fixtures do not define a correct Jira action grading target")
	}
	if !foundPromptInjectionBlock {
		t.Fatal("fixtures do not define a prompt-injection no-action grading target")
	}
	if !foundRiskyConfirmation {
		t.Fatal("fixtures do not define a medium/high-risk confirmation grading target")
	}
	for _, extractionType := range []string{"owner", "eta", "blocker"} {
		if !foundExtraction[extractionType] {
			t.Fatalf("fixtures do not define extraction grading target %q", extractionType)
		}
	}
	for _, field := range append([]string{"slack_ready_summary", "jira_changes_summary"}, requiredRecapFields...) {
		if !recapFields[field] {
			t.Fatalf("fixtures do not define recap grading field %q", field)
		}
	}
	for _, explanationType := range requiredConfidenceTypes {
		if !foundConfidence[explanationType] {
			t.Fatalf("fixtures do not define confidence explanation %q", explanationType)
		}
	}
	if !foundPostMeetingIntelligence {
		t.Fatal("fixtures do not define gradeable post-meeting intelligence expectations")
	}
}

func TestAudioFixtureManifestsCoverMultiParticipantTiming(t *testing.T) {
	manifests := loadAudioManifests(t)
	if len(manifests) == 0 {
		t.Fatal("no audio fixture manifests found")
	}

	found := map[string]bool{}
	for _, manifest := range manifests {
		if manifest.SchemaVersion != 1 || manifest.ID == "" || manifest.FixtureID == "" {
			t.Fatalf("invalid audio manifest header: %#v", manifest)
		}
		if count := len(manifest.Tracks); count < 2 || count > 4 {
			t.Fatalf("%s tracks = %d, want 2-4", manifest.ID, count)
		}
		for _, behavior := range manifest.RequiredBehaviors {
			found[behavior] = true
		}
		if hasOverlappingSegments(manifest) {
			found["overlap"] = true
		}
		if hasInterruptedSegment(manifest) {
			found["interruption"] = true
		}
		if longestSilenceMS(manifest) >= manifest.RequiredSilenceMS && manifest.RequiredSilenceMS > 0 {
			found["silence"] = true
		}
		for _, event := range manifest.Events {
			found[event.Type] = true
		}
	}

	for _, behavior := range []string{"interruption", "overlap", "silence", "reconnect", "late_join"} {
		if !found[behavior] {
			t.Fatalf("audio manifests do not cover %q", behavior)
		}
	}
}

func TestAWSLiveKitHardeningChecklistIsActionable(t *testing.T) {
	checklists := loadAWSChecklists(t)
	if len(checklists) == 0 {
		t.Fatal("no AWS LiveKit hardening checklist found")
	}

	requiredAreas := map[string]bool{
		"udp_turn":             false,
		"reconnect":            false,
		"cloudwatch_alarms":    false,
		"livekit_cloud_switch": false,
	}
	validStatuses := map[string]bool{"not_run": true, "pass": true, "fail": true, "blocked": true}

	for _, checklist := range checklists {
		if checklist.SchemaVersion != 1 || checklist.ID == "" {
			t.Fatalf("invalid AWS checklist header: %#v", checklist)
		}
		for _, area := range checklist.Areas {
			if _, ok := requiredAreas[area.ID]; ok {
				requiredAreas[area.ID] = true
			}
			if area.ID == "" || area.Name == "" || len(area.Items) == 0 {
				t.Fatalf("%s has invalid area: %#v", checklist.ID, area)
			}
			for _, item := range area.Items {
				if item.ID == "" || item.Command == "" || item.EvidenceRequired == "" || item.PassCriteria == "" {
					t.Fatalf("%s/%s has incomplete item: %#v", checklist.ID, area.ID, item)
				}
				if !validStatuses[item.Status] {
					t.Fatalf("%s/%s/%s status = %q", checklist.ID, area.ID, item.ID, item.Status)
				}
			}
		}
	}

	for area, covered := range requiredAreas {
		if !covered {
			t.Fatalf("AWS LiveKit checklist does not cover area %q", area)
		}
	}
}

func TestExtensionContractFixtureIsDefined(t *testing.T) {
	fixtures := loadExtensionContractFixtures(t)
	if len(fixtures) == 0 {
		t.Fatal("no extension contract fixture found")
	}
	requiredContracts := map[string]bool{
		"voice_provider_contract": false,
		"connector_contract":      false,
		"model_provider_contract": false,
		"action_ledger_contract":  false,
		"import_boundary_check":   false,
	}
	for _, fixture := range fixtures {
		if fixture.SchemaVersion != 1 || fixture.ID == "" || fixture.Description == "" {
			t.Fatalf("invalid extension fixture header: %#v", fixture)
		}
		for _, name := range []string{"nova-sonic", "openai-realtime", "openai-realtime-translate", "openai-realtime-whisper", "livekit-cloud"} {
			if !containsString(fixture.RequiredVoiceProviders, name) {
				t.Fatalf("%s missing voice provider %q", fixture.ID, name)
			}
		}
		for _, name := range []string{"local-board", "jira", "github"} {
			if !containsString(fixture.RequiredConnectors, name) {
				t.Fatalf("%s missing connector %q", fixture.ID, name)
			}
		}
		if !containsString(fixture.RequiredModelProviders, "bedrock") {
			t.Fatalf("%s missing bedrock model provider", fixture.ID)
		}
		for _, contract := range fixture.RequiredContracts {
			if _, ok := requiredContracts[contract]; ok {
				requiredContracts[contract] = true
			}
		}
		if len(fixture.QualityGates) == 0 {
			t.Fatalf("%s must list quality gates", fixture.ID)
		}
	}
	for contract, covered := range requiredContracts {
		if !covered {
			t.Fatalf("extension fixture does not cover contract %q", contract)
		}
	}
}

func TestOptionalEvaluationResultsMatchFixtures(t *testing.T) {
	resultsDir := strings.TrimSpace(os.Getenv("AUTO_BOT_EVAL_RESULTS_DIR"))
	if resultsDir == "" {
		t.Skip("set AUTO_BOT_EVAL_RESULTS_DIR to grade captured agent outputs against the fixtures")
	}

	fixtures := loadMeetingFixtures(t)
	results := loadEvaluationResults(t, resultsDir)
	for _, fixture := range fixtures {
		result, ok := results[fixture.ID]
		if !ok {
			t.Fatalf("missing evaluation result for fixture %s", fixture.ID)
		}
		assertExpectedSubset(t, fixture, result)
	}
}

func requireControlCenterExpectation(t *testing.T, fixtureID string, mcc meetingControlCenter) {
	t.Helper()
	if len(mcc.Spoken) == 0 {
		t.Fatalf("%s missing meeting-control spoken list", fixtureID)
	}
	if mcc.NotSpoken == nil {
		t.Fatalf("%s missing meeting-control not-spoken list", fixtureID)
	}
	if mcc.Blockers == nil || mcc.Decisions == nil || mcc.ActionItems == nil || mcc.PendingConfirmations == nil || mcc.JiraMutations == nil {
		t.Fatalf("%s missing one or more meeting-control center collections", fixtureID)
	}
}

func assertExpectedSubset(t *testing.T, fixture meetingFixture, result evaluationResult) {
	t.Helper()
	for _, action := range fixture.Expected.JiraActions {
		if action.MustExecute && !containsJiraAction(result.JiraActions, action) {
			t.Fatalf("%s result missing expected Jira action %#v", fixture.ID, action)
		}
	}
	for _, action := range fixture.Expected.ForbiddenActions {
		if !containsForbiddenAction(result.ForbiddenActions, action) {
			t.Fatalf("%s result missing forbidden action assertion %#v", fixture.ID, action)
		}
	}
	for _, confirmation := range fixture.Expected.Confirmations {
		if confirmation.Required && !containsConfirmation(result.Confirmations, confirmation) {
			t.Fatalf("%s result missing confirmation %#v", fixture.ID, confirmation)
		}
	}
	for _, extraction := range fixture.Expected.Extractions {
		if !containsExtraction(result.Extractions, extraction) {
			t.Fatalf("%s result missing extraction %#v", fixture.ID, extraction)
		}
	}
	for _, signal := range fixture.Expected.VoiceReliabilitySignals {
		if !containsString(result.VoiceReliabilitySignals, signal) {
			t.Fatalf("%s result missing voice reliability signal %q", fixture.ID, signal)
		}
	}
	for _, explanation := range fixture.Expected.ConfidenceExplanations {
		if explanation.Required && !containsConfidenceExplanation(result.ConfidenceExplanations, explanation.Type) {
			t.Fatalf("%s result missing confidence explanation %q", fixture.ID, explanation.Type)
		}
	}
	if fixture.Expected.Recap.SlackReadySummary && !result.Recap.SlackReadySummary {
		t.Fatalf("%s result missing Slack-ready recap", fixture.ID)
	}
	if fixture.Expected.Recap.JiraChangesSummary && !result.Recap.JiraChangesSummary {
		t.Fatalf("%s result missing Jira changes recap", fixture.ID)
	}
	if expected := fixture.Expected.PostMeetingIntelligence; expected != nil {
		if result.PostMeetingIntelligence == nil {
			t.Fatalf("%s result missing post-meeting intelligence output", fixture.ID)
		}
		if expected.PageRequired && !result.PostMeetingIntelligence.PageRequired {
			t.Fatalf("%s result did not prove post-meeting page", fixture.ID)
		}
		if expected.ArchivesReport && !result.PostMeetingIntelligence.ArchivesReport {
			t.Fatalf("%s result did not prove report archiving", fixture.ID)
		}
		if expected.TranscriptEvidence && !result.PostMeetingIntelligence.TranscriptEvidence {
			t.Fatalf("%s result missing transcript evidence in post-meeting report", fixture.ID)
		}
	}
}

func containsJiraAction(actions []jiraAction, want jiraAction) bool {
	for _, action := range actions {
		if action.Tool == want.Tool && action.IssueKey == want.IssueKey && action.Risk == want.Risk && action.RequiresConfirm == want.RequiresConfirm {
			return true
		}
	}
	return false
}

func containsForbiddenAction(actions []forbiddenAction, want forbiddenAction) bool {
	for _, action := range actions {
		if action.Tool == want.Tool && action.Reason == want.Reason && action.SourceEventID == want.SourceEventID && action.Expected == want.Expected {
			return true
		}
	}
	return false
}

func containsConfirmation(confirmations []confirmation, want confirmation) bool {
	for _, confirmation := range confirmations {
		if confirmation.Tool == want.Tool && confirmation.IssueKey == want.IssueKey && confirmation.Risk == want.Risk && confirmation.Required == want.Required && strings.Contains(confirmation.PromptContains, want.PromptContains) {
			return true
		}
	}
	return false
}

func containsExtraction(extractions []extraction, want extraction) bool {
	for _, extraction := range extractions {
		if extraction.Type == want.Type && extraction.IssueKey == want.IssueKey && extraction.Owner == want.Owner && extraction.Value == want.Value {
			return true
		}
	}
	return false
}

func containsConfidenceExplanation(explanations []confidenceExplanation, wantType string) bool {
	for _, explanation := range explanations {
		if explanation.Type == wantType && explanation.Example != "" {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasOverlappingSegments(manifest audioManifest) bool {
	type timedSegment struct {
		trackID string
		segment audioSegment
	}
	var segments []timedSegment
	for _, track := range manifest.Tracks {
		for _, segment := range track.Segments {
			segments = append(segments, timedSegment{trackID: track.ParticipantID, segment: segment})
		}
	}
	for i := range segments {
		for j := i + 1; j < len(segments); j++ {
			a := segments[i]
			b := segments[j]
			if a.trackID != b.trackID && a.segment.StartMS < b.segment.EndMS && b.segment.StartMS < a.segment.EndMS {
				return true
			}
		}
	}
	return false
}

func hasInterruptedSegment(manifest audioManifest) bool {
	for _, track := range manifest.Tracks {
		for _, segment := range track.Segments {
			if segment.InterruptsSegmentID != "" {
				return true
			}
		}
	}
	return false
}

func longestSilenceMS(manifest audioManifest) int {
	var segments []audioSegment
	for _, track := range manifest.Tracks {
		segments = append(segments, track.Segments...)
	}
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].StartMS < segments[j].StartMS
	})
	longest := 0
	lastEnd := 0
	for _, segment := range segments {
		if segment.StartMS > lastEnd && segment.StartMS-lastEnd > longest {
			longest = segment.StartMS - lastEnd
		}
		if segment.EndMS > lastEnd {
			lastEnd = segment.EndMS
		}
	}
	return longest
}

func loadMeetingFixtures(t *testing.T) []meetingFixture {
	t.Helper()
	var fixtures []meetingFixture
	for _, path := range fixturePaths(t) {
		header := loadHeader(t, path)
		if header.FixtureKind != "meeting_evaluation" {
			continue
		}
		var fixture meetingFixture
		loadJSON(t, path, &fixture)
		fixtures = append(fixtures, fixture)
	}
	if len(fixtures) == 0 {
		t.Fatal("no meeting evaluation fixtures found")
	}
	return fixtures
}

func loadAudioManifests(t *testing.T) []audioManifest {
	t.Helper()
	var manifests []audioManifest
	for _, path := range fixturePaths(t) {
		header := loadHeader(t, path)
		if header.FixtureKind != "audio_manifest" {
			continue
		}
		var manifest audioManifest
		loadJSON(t, path, &manifest)
		manifests = append(manifests, manifest)
	}
	return manifests
}

func loadAWSChecklists(t *testing.T) []awsChecklist {
	t.Helper()
	var checklists []awsChecklist
	for _, path := range fixturePaths(t) {
		header := loadHeader(t, path)
		if header.FixtureKind != "aws_livekit_hardening_checklist" {
			continue
		}
		var checklist awsChecklist
		loadJSON(t, path, &checklist)
		checklists = append(checklists, checklist)
	}
	return checklists
}

func loadExtensionContractFixtures(t *testing.T) []extensionContractFixture {
	t.Helper()
	var fixtures []extensionContractFixture
	for _, path := range fixturePaths(t) {
		header := loadHeader(t, path)
		if header.FixtureKind != "extension_contracts" {
			continue
		}
		var fixture extensionContractFixture
		loadJSON(t, path, &fixture)
		fixtures = append(fixtures, fixture)
	}
	return fixtures
}

func loadEvaluationResults(t *testing.T, dir string) map[string]evaluationResult {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob evaluation results: %v", err)
	}
	results := map[string]evaluationResult{}
	for _, path := range paths {
		var result evaluationResult
		loadJSON(t, path, &result)
		if result.FixtureID == "" {
			t.Fatalf("%s missing fixture_id", path)
		}
		results[result.FixtureID] = result
	}
	return results
}

func fixturePaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("fixtures", "*.json"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	sort.Strings(paths)
	return paths
}

func loadHeader(t *testing.T, path string) fixtureHeader {
	t.Helper()
	var header fixtureHeader
	loadJSON(t, path, &header)
	return header
}

func loadJSON(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestFixtureIDsMatchFileNames(t *testing.T) {
	for _, path := range fixturePaths(t) {
		header := loadHeader(t, path)
		if header.ID == "" {
			t.Fatalf("%s missing id", path)
		}
		want := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if header.ID != want {
			t.Fatalf("%s id = %q, want %q", path, header.ID, want)
		}
		if header.SchemaVersion != 1 {
			t.Fatalf("%s schema_version = %d, want 1", path, header.SchemaVersion)
		}
		if header.FixtureKind == "" {
			t.Fatalf("%s missing fixture_kind", path)
		}
	}
}

func Example() {
	result := evaluationResult{
		FixtureID: "daily_standup_multi_participant_v1",
		Behaviors: []string{"interruption", "overlap", "silence", "reconnect", "late_join"},
		JiraActions: []jiraAction{{
			Tool:        "set_blocked",
			IssueKey:    "EMAL-14",
			Risk:        "low",
			MustExecute: true,
		}},
		Recap: recapExpectation{
			SlackReadySummary:  true,
			JiraChangesSummary: true,
		},
	}
	raw, _ := json.Marshal(result)
	fmt.Println(string(raw))
	// Output: {"fixture_id":"daily_standup_multi_participant_v1","behaviors":["interruption","overlap","silence","reconnect","late_join"],"jira_actions":[{"tool":"set_blocked","issue_key":"EMAL-14","risk":"low","must_execute":true,"requires_confirmation":false}],"forbidden_actions":null,"confirmations":null,"extractions":null,"recap":{"slack_ready_summary":true,"jira_changes_summary":true,"blockers":null,"action_items_by_owner":null,"unresolved_questions":null,"changed_since_start":null},"voice_reliability_signals":null,"meeting_control_center":{"spoken":null,"not_spoken":null,"blockers":null,"decisions":null,"action_items":null,"pending_confirmations":null,"jira_mutations":null},"failure_states":null,"confidence_explanations":null}
}
