package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// meetingIntelligenceReport is the JSON report returned by
// /meeting/intelligence and archived when durable storage is configured.
type meetingIntelligenceReport struct {
	OK                   bool                       `json:"ok"`
	TenantID             string                     `json:"tenant_id,omitempty"`
	BoardID              string                     `json:"board_id"`
	MeetingID            string                     `json:"meeting_id"`
	MeetingType          string                     `json:"meeting_type,omitempty"`
	Mode                 scrumMeetingMode           `json:"mode,omitempty"`
	Active               bool                       `json:"active"`
	GeneratedAt          string                     `json:"generated_at"`
	StartedAt            string                     `json:"started_at,omitempty"`
	EndedAt              string                     `json:"ended_at,omitempty"`
	HostIdentity         string                     `json:"host_identity,omitempty"`
	Summary              string                     `json:"summary"`
	SlackSummary         string                     `json:"slack_summary"`
	Participants         []meetingParticipantReport `json:"participants,omitempty"`
	Agenda               []string                   `json:"agenda,omitempty"`
	Decisions            []string                   `json:"decisions,omitempty"`
	Risks                []string                   `json:"risks,omitempty"`
	ActionItems          []string                   `json:"action_items,omitempty"`
	ParkingLot           []string                   `json:"parking_lot,omitempty"`
	FollowUps            []scrumFollowUp            `json:"follow_ups,omitempty"`
	UnresolvedBlockers   []scrumBlocker             `json:"unresolved_blockers,omitempty"`
	Ownership            []scrumOwnership           `json:"ownership,omitempty"`
	Briefing             *scrumBriefing             `json:"briefing,omitempty"`
	JiraChanges          []boardMutationView        `json:"jira_changes,omitempty"`
	AgentRuns            []agentRunView             `json:"agent_runs,omitempty"`
	Transcript           []transcriptEntry          `json:"transcript,omitempty"`
	TranscriptEvidence   []transcriptEvidence       `json:"transcript_evidence,omitempty"`
	SprintIntelligence   sprintIntelligence         `json:"sprint_intelligence"`
	GitHubContext        githubMeetingContext       `json:"github_context"`
	ProductProof         productProofMetrics        `json:"product_proof"`
	Observability        meetingObservability       `json:"observability"`
	Setup                setupReadinessReport       `json:"setup"`
	OpenQuestions        []string                   `json:"open_questions,omitempty"`
	ChangedSinceStart    []string                   `json:"changed_since_start,omitempty"`
	PendingConfirmations []pendingConfirmationView  `json:"pending_confirmations,omitempty"`
	Conflicts            []jiraConflict             `json:"conflicts,omitempty"`
	BoardSequenceNumber  int64                      `json:"board_sequence_number"`
	BoardUpdatedAt       string                     `json:"board_updated_at,omitempty"`
	Source               string                     `json:"source,omitempty"`
}

type meetingReportSummary struct {
	BoardID        string           `json:"board_id"`
	MeetingID      string           `json:"meeting_id"`
	MeetingType    string           `json:"meeting_type,omitempty"`
	Mode           scrumMeetingMode `json:"mode,omitempty"`
	StartedAt      string           `json:"started_at,omitempty"`
	EndedAt        string           `json:"ended_at,omitempty"`
	GeneratedAt    string           `json:"generated_at"`
	Summary        string           `json:"summary"`
	ParticipantCnt int              `json:"participant_count"`
	JiraChangeCnt  int              `json:"jira_change_count"`
	AgentRunCnt    int              `json:"agent_run_count"`
	BlockerCnt     int              `json:"blocker_count"`
	ActionItemCnt  int              `json:"action_item_count"`
	MinutesSaved   int              `json:"estimated_minutes_saved"`
}

type meetingParticipantReport struct {
	Identity   string `json:"identity"`
	Role       string `json:"role,omitempty"`
	JoinedAt   string `json:"joined_at,omitempty"`
	Present    bool   `json:"present"`
	HasSpoken  bool   `json:"has_spoken"`
	LastUpdate string `json:"last_update,omitempty"`
}

type sprintIntelligence struct {
	BlockedCards         []string `json:"blocked_cards,omitempty"`
	UnassignedCards      []string `json:"unassigned_cards,omitempty"`
	MissingETACards      []string `json:"missing_eta_cards,omitempty"`
	StaleCards           []string `json:"stale_cards,omitempty"`
	OverloadedOwners     []string `json:"overloaded_owners,omitempty"`
	ScopeChanges         []string `json:"scope_changes,omitempty"`
	MovedBackward        []string `json:"moved_backward,omitempty"`
	PRReadyCards         []string `json:"pr_ready_cards,omitempty"`
	RecommendedQuestions []string `json:"recommended_questions,omitempty"`
	RiskScore            int      `json:"risk_score"`
}

type githubMeetingContext struct {
	Configured bool                      `json:"configured"`
	Signals    []githubPullRequestSignal `json:"signals,omitempty"`
	Message    string                    `json:"message"`
}

type githubPullRequestSignal struct {
	CardID string `json:"card_id,omitempty"`
	Title  string `json:"title"`
	URL    string `json:"url,omitempty"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type productProofMetrics struct {
	BaselineMeetingMinutes       int      `json:"baseline_meeting_minutes"`
	ActualMeetingMinutes         int      `json:"actual_meeting_minutes"`
	EstimatedAdminMinutesAvoided int      `json:"estimated_admin_minutes_avoided"`
	EstimatedNetMinutesSaved     int      `json:"estimated_net_minutes_saved"`
	JiraChangesAutomated         int      `json:"jira_changes_automated"`
	AgentRunsStarted             int      `json:"agent_runs_started"`
	AgentRunsCompleted           int      `json:"agent_runs_completed"`
	AgentRunsNeedingHuman        int      `json:"agent_runs_needing_human"`
	NeedsToolingEscalations      int      `json:"needs_tooling_escalations"`
	HumanFallbackSignals         int      `json:"human_fallback_signals"`
	AutomationRate               float64  `json:"automation_rate"`
	MeasurementQuality           string   `json:"measurement_quality"`
	Evidence                     []string `json:"evidence,omitempty"`
}

type meetingObservability struct {
	VoiceProvider             string `json:"voice_provider"`
	VoiceProviderReady        bool   `json:"voice_provider_ready"`
	LiveKitBrowserURL         string `json:"livekit_browser_url,omitempty"`
	LiveKitDeploymentMode     string `json:"livekit_deployment_mode,omitempty"`
	JiraConfigured            bool   `json:"jira_configured"`
	StoragePersistent         bool   `json:"storage_persistent"`
	BoardSequenceNumber       int64  `json:"board_sequence_number"`
	LastTranscriptionAt       string `json:"last_transcription_at,omitempty"`
	AgentParticipantPresent   bool   `json:"agent_participant_present"`
	BedrockStreamActive       bool   `json:"bedrock_stream_active"`
	MeetingAccessActive       bool   `json:"meeting_access_active"`
	ActiveMeetingParticipants int    `json:"active_meeting_participants"`
}

type setupReadinessReport struct {
	AuthMode         string                `json:"auth_mode"`
	IdentityProvider string                `json:"identity_provider"`
	Region           string                `json:"region"`
	Checks           []setupReadinessCheck `json:"checks"`
	ProviderOptions  []voiceProviderOption `json:"provider_options"`
	AdminActions     []string              `json:"admin_actions,omitempty"`
	Metadata         map[string]string     `json:"metadata,omitempty"`
}

type setupReadinessCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Status   string `json:"status"`
	Remedy   string `json:"remedy,omitempty"`
	Required bool   `json:"required"`
}

type voiceProviderOption struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	FullDuplex bool   `json:"full_duplex"`
	Transport  string `json:"transport"`
	Notes      string `json:"notes,omitempty"`
}

// BuildMeetingIntelligenceReport takes a point-in-time board snapshot and
// derives client-safe recap, evidence, setup, observability, sprint, GitHub,
// and agent-run views without mutating the board.
func (board *kanbanBoard) BuildMeetingIntelligenceReport(source string) meetingIntelligenceReport {
	now := time.Now().UTC()
	board.mu.Lock()
	state := board.snapshotStateLocked()
	meeting := cloneScrumMeetingStatePointerValue(state.Meeting)
	cards := cloneKanbanCards(board.cards)
	mutations := append([]boardMutationRecord(nil), board.mutationHistory...)
	agentRuns := board.agentRunViewsLocked(50)
	transcripts := append([]transcriptEntry(nil), board.lastTranscripts...)
	pending := board.pendingConfirmationViewsLocked()
	conflicts := append([]jiraConflict(nil), board.conflicts...)
	board.mu.Unlock()

	access := meetingAccessSnapshot{}
	if meetingAccess != nil {
		access = meetingAccess.snapshot(false)
	}
	if meeting == nil {
		meeting = &scrumMeetingState{
			MeetingID: access.MeetingID,
			Active:    access.Active,
			StartedAt: access.CreatedAt,
			Mode:      meetingTypeToScrumMode(access.MeetingType),
		}
	}
	if meeting.MeetingID == "" {
		meeting.MeetingID = firstNonEmpty(access.MeetingID, "current-"+now.Format("20060102T150405Z"))
	}

	startedAt := firstNonEmpty(meeting.StartedAt, access.CreatedAt)
	endedAt := meeting.EndedAt
	if !meeting.Active && endedAt == "" {
		endedAt = firstNonEmpty(access.UpdatedAt, now.Format(time.RFC3339))
	}
	since := parseOptionalTime(startedAt)
	jiraChanges := mutationViewsSince(mutations, since)
	transcript := transcriptsSince(transcripts, since)
	productProof := buildProductProofMetrics(firstNonEmpty(access.MeetingType, string(meeting.Mode)), startedAt, endedAt, now, jiraChanges, agentRuns, *meeting, pending)

	report := meetingIntelligenceReport{
		OK:                   true,
		TenantID:             board.tenantID,
		BoardID:              stateBoardID(state, board.boardID),
		MeetingID:            meeting.MeetingID,
		MeetingType:          firstNonEmpty(access.MeetingType, string(meeting.Mode)),
		Mode:                 meeting.Mode,
		Active:               meeting.Active,
		GeneratedAt:          now.Format(time.RFC3339Nano),
		StartedAt:            startedAt,
		EndedAt:              endedAt,
		HostIdentity:         access.HostIdentity,
		Participants:         mergeMeetingParticipants(*meeting, access),
		Agenda:               append([]string(nil), meeting.Agenda...),
		Decisions:            append([]string(nil), meeting.Decisions...),
		Risks:                append([]string(nil), meeting.Risks...),
		ActionItems:          append([]string(nil), meeting.ActionItems...),
		ParkingLot:           append([]string(nil), meeting.ParkingLot...),
		FollowUps:            append([]scrumFollowUp(nil), meeting.FollowUps...),
		UnresolvedBlockers:   append([]scrumBlocker(nil), meeting.UnresolvedBlockers...),
		Ownership:            append([]scrumOwnership(nil), meeting.Ownership...),
		Briefing:             cloneScrumBriefing(meeting.LastBriefing),
		JiraChanges:          jiraChanges,
		AgentRuns:            agentRuns,
		Transcript:           transcript,
		TranscriptEvidence:   transcriptEvidenceFromMutations(jiraChanges),
		SprintIntelligence:   buildSprintIntelligence(cards, mutations, since),
		GitHubContext:        buildGitHubMeetingContext(cards),
		ProductProof:         productProof,
		Observability:        buildMeetingObservability(state.SequenceNumber, access),
		Setup:                buildSetupReadinessReport(),
		OpenQuestions:        buildOpenQuestions(*meeting, cards),
		ChangedSinceStart:    mutationSummaries(jiraChanges),
		PendingConfirmations: pending,
		Conflicts:            conflicts,
		BoardSequenceNumber:  state.SequenceNumber,
		BoardUpdatedAt:       state.UpdatedAt,
		Source:               truncateString(source, 80),
	}
	report.Summary = report.buildSummary()
	report.SlackSummary = report.buildSlackSummary()
	return report
}

func stateBoardID(state kanbanBoardState, fallback string) string {
	if fallback != "" {
		return fallback
	}
	return defaultAppBoardID
}

// SummaryView returns the compact archive/list representation for a report.
func (report meetingIntelligenceReport) SummaryView() meetingReportSummary {
	return meetingReportSummary{
		BoardID:        report.BoardID,
		MeetingID:      report.MeetingID,
		MeetingType:    report.MeetingType,
		Mode:           report.Mode,
		StartedAt:      report.StartedAt,
		EndedAt:        report.EndedAt,
		GeneratedAt:    report.GeneratedAt,
		Summary:        report.Summary,
		ParticipantCnt: len(report.Participants),
		JiraChangeCnt:  len(report.JiraChanges),
		AgentRunCnt:    len(report.AgentRuns),
		BlockerCnt:     len(report.UnresolvedBlockers) + len(report.SprintIntelligence.BlockedCards),
		ActionItemCnt:  len(report.ActionItems) + len(report.FollowUps),
		MinutesSaved:   report.ProductProof.EstimatedNetMinutesSaved,
	}
}

func (report meetingIntelligenceReport) buildSummary() string {
	meetingType := string(report.Mode)
	if meetingType == "" {
		meetingType = firstNonEmpty(report.MeetingType, "meeting")
	}
	return fmt.Sprintf("%s captured %d participant%s, %d Jira change%s, %d blocker%s, and %d action item%s.",
		meetingType,
		len(report.Participants), plural(len(report.Participants)),
		len(report.JiraChanges), plural(len(report.JiraChanges)),
		len(report.UnresolvedBlockers)+len(report.SprintIntelligence.BlockedCards), plural(len(report.UnresolvedBlockers)+len(report.SprintIntelligence.BlockedCards)),
		len(report.ActionItems)+len(report.FollowUps), plural(len(report.ActionItems)+len(report.FollowUps)))
}

func (report meetingIntelligenceReport) buildSlackSummary() string {
	lines := []string{
		fmt.Sprintf("*%s recap*", firstNonEmpty(report.MeetingType, string(report.Mode), "Meeting")),
		report.Summary,
		"",
		"*Decisions:*",
	}
	lines = append(lines, markdownBullets(report.Decisions, "No decisions captured.")...)
	lines = append(lines, "", "*Jira changes:*")
	lines = append(lines, markdownBullets(report.ChangedSinceStart, "No Jira changes captured.")...)
	lines = append(lines, "", "*Blockers and risks:*")
	lines = append(lines, markdownBullets(uniqueStrings(append(report.Risks, report.SprintIntelligence.BlockedCards...)), "No blockers detected.")...)
	lines = append(lines, "", "*Action items:*")
	actionItems := append([]string(nil), report.ActionItems...)
	for _, followUp := range report.FollowUps {
		actionItems = append(actionItems, formatFollowUpSummary(followUp))
	}
	lines = append(lines, markdownBullets(actionItems, "No action items captured.")...)
	lines = append(lines, "", "*Product proof:*")
	lines = append(lines, productProofBullets(report.ProductProof)...)
	lines = append(lines, "", "*Open questions:*")
	lines = append(lines, markdownBullets(report.OpenQuestions, "No unresolved questions.")...)
	return strings.Join(lines, "\n")
}

func productProofBullets(metrics productProofMetrics) []string {
	if metrics.MeasurementQuality == "" {
		return []string{"- Product proof metrics were not generated."}
	}
	return []string{
		fmt.Sprintf("- Estimated net minutes saved: %d (%s).", metrics.EstimatedNetMinutesSaved, metrics.MeasurementQuality),
		fmt.Sprintf("- Automated Jira changes: %d; completed agent runs: %d.", metrics.JiraChangesAutomated, metrics.AgentRunsCompleted),
		fmt.Sprintf("- Human fallback signals: %d; needs-tooling escalations: %d.", metrics.HumanFallbackSignals, metrics.NeedsToolingEscalations),
	}
}

func markdownBullets(items []string, empty string) []string {
	items = uniqueStrings(items)
	if len(items) == 0 {
		return []string{"- " + empty}
	}
	if len(items) > 12 {
		items = items[:12]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, "- "+item)
	}
	return out
}

func mergeMeetingParticipants(meeting scrumMeetingState, access meetingAccessSnapshot) []meetingParticipantReport {
	byIdentity := map[string]meetingParticipantReport{}
	for _, participant := range meeting.Participants {
		name := strings.TrimSpace(participant.Name)
		if name == "" {
			name = strings.TrimSpace(participant.ParticipantID)
		}
		if name == "" {
			continue
		}
		byIdentity[strings.ToLower(name)] = meetingParticipantReport{
			Identity:   name,
			Role:       participant.Role,
			HasSpoken:  participant.HasSpoken,
			LastUpdate: participant.LastUpdate,
		}
	}
	for _, session := range access.Participants {
		key := strings.ToLower(session.Identity)
		existing := byIdentity[key]
		existing.Identity = session.Identity
		existing.Role = firstNonEmpty(existing.Role, session.Role)
		existing.JoinedAt = session.JoinedAt
		existing.Present = access.Active
		byIdentity[key] = existing
	}
	out := make([]meetingParticipantReport, 0, len(byIdentity))
	for _, participant := range byIdentity {
		out = append(out, participant)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role == meetingRoleHost
		}
		return out[i].Identity < out[j].Identity
	})
	return out
}

func mutationViewsSince(records []boardMutationRecord, since time.Time) []boardMutationView {
	views := make([]boardMutationView, 0)
	for _, record := range records {
		if !since.IsZero() {
			occurred, err := time.Parse(time.RFC3339Nano, record.OccurredAt)
			if err == nil && occurred.Before(since) {
				continue
			}
		}
		views = append(views, boardMutationToView(record))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].OccurredAt > views[j].OccurredAt })
	if len(views) > 100 {
		views = views[:100]
	}
	return views
}

func transcriptsSince(entries []transcriptEntry, since time.Time) []transcriptEntry {
	out := make([]transcriptEntry, 0, len(entries))
	for _, entry := range entries {
		if !since.IsZero() {
			created, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
			if err == nil && created.Before(since) {
				continue
			}
		}
		out = append(out, entry)
	}
	return out
}

func transcriptEvidenceFromMutations(mutations []boardMutationView) []transcriptEvidence {
	out := make([]transcriptEvidence, 0)
	for _, mutation := range mutations {
		if mutation.Transcript.Summary != "" || len(mutation.Transcript.Entries) > 0 {
			out = append(out, mutation.Transcript)
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func mutationSummaries(mutations []boardMutationView) []string {
	out := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.Summary != "" {
			out = append(out, mutation.Summary)
		}
	}
	return uniqueStrings(out)
}

func buildSprintIntelligence(cards []kanbanCard, records []boardMutationRecord, since time.Time) sprintIntelligence {
	ownerCounts := map[string]int{}
	intel := sprintIntelligence{}
	for _, card := range cards {
		label := card.ID + ": " + card.Title
		if card.Status == kanbanStatusBlocked {
			intel.BlockedCards = append(intel.BlockedCards, label)
		}
		if card.Assignee == nil && card.Status != kanbanStatusDone {
			intel.UnassignedCards = append(intel.UnassignedCards, label)
		}
		if card.DueDate == "" && card.Status != kanbanStatusDone {
			intel.MissingETACards = append(intel.MissingETACards, label)
		}
		if cardLooksPRReady(card) {
			intel.PRReadyCards = append(intel.PRReadyCards, label)
		}
		if cardLooksStale(card, time.Now().UTC().Add(-24*time.Hour)) {
			intel.StaleCards = append(intel.StaleCards, label)
		}
		if card.Assignee != nil && card.Assignee.DisplayName != "" && card.Status != kanbanStatusDone {
			ownerCounts[card.Assignee.DisplayName]++
		}
	}
	for owner, count := range ownerCounts {
		if count >= 4 {
			intel.OverloadedOwners = append(intel.OverloadedOwners, fmt.Sprintf("%s owns %d open items", owner, count))
		}
	}
	for _, record := range records {
		if !since.IsZero() {
			occurred, err := time.Parse(time.RFC3339Nano, record.OccurredAt)
			if err == nil && occurred.Before(since) {
				continue
			}
		}
		switch record.ToolName {
		case "create_ticket", "create_subtask":
			intel.ScopeChanges = append(intel.ScopeChanges, record.Summary)
		case "move_ticket":
			if status := strings.ToLower(asString(record.Arguments["status"])); status == strings.ToLower(string(kanbanStatusBacklog)) {
				intel.MovedBackward = append(intel.MovedBackward, record.Summary)
			}
		}
	}
	if len(intel.BlockedCards) > 0 {
		intel.RecommendedQuestions = append(intel.RecommendedQuestions, "Who owns each blocker, and what is the next unblock step?")
	}
	if len(intel.UnassignedCards) > 0 {
		intel.RecommendedQuestions = append(intel.RecommendedQuestions, "Which unassigned work should get an owner before the next meeting?")
	}
	if len(intel.MissingETACards) > 0 {
		intel.RecommendedQuestions = append(intel.RecommendedQuestions, "Which open items need ETA or due-date commitments?")
	}
	if len(intel.ScopeChanges) > 0 {
		intel.RecommendedQuestions = append(intel.RecommendedQuestions, "Did the meeting add scope that should be accepted into the sprint?")
	}
	intel.RiskScore = minInt(100, len(intel.BlockedCards)*18+len(intel.UnassignedCards)*8+len(intel.MissingETACards)*5+len(intel.StaleCards)*10+len(intel.OverloadedOwners)*12)
	return intel
}

func buildGitHubMeetingContext(cards []kanbanCard) githubMeetingContext {
	signals := make([]githubPullRequestSignal, 0)
	for _, card := range cards {
		for _, link := range card.RemoteLinks {
			normalized := strings.ToLower(link.URL + " " + link.Title + " " + link.Summary)
			if strings.Contains(normalized, "github.com") || strings.Contains(normalized, "/pull/") || strings.Contains(normalized, "pull request") {
				signals = append(signals, githubPullRequestSignal{
					CardID: card.ID,
					Title:  firstNonEmpty(link.Title, card.Title),
					URL:    link.URL,
					State:  inferPullRequestState(normalized),
					Reason: "Card remote link mentions GitHub or a pull request.",
				})
			}
		}
		if cardLooksPRReady(card) {
			signals = append(signals, githubPullRequestSignal{
				CardID: card.ID,
				Title:  card.Title,
				State:  "ready_for_review",
				Reason: "Card tags, comments, or links indicate a ready PR.",
			})
		}
	}
	if raw := strings.TrimSpace(os.Getenv("GITHUB_CONTEXT_JSON")); raw != "" {
		var configured []githubPullRequestSignal
		if err := json.Unmarshal([]byte(raw), &configured); err == nil {
			signals = append(signals, configured...)
		}
	}
	configured := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) != "" || strings.TrimSpace(os.Getenv("GITHUB_CONTEXT_JSON")) != ""
	message := "GitHub connector is not configured; using Jira card tags and remote links for PR context."
	if configured {
		message = "GitHub context is configured for enrichment."
	}
	return githubMeetingContext{
		Configured: configured,
		Signals:    dedupeGitHubSignals(signals),
		Message:    message,
	}
}

func inferPullRequestState(text string) string {
	switch {
	case strings.Contains(text, "merged"):
		return "merged"
	case strings.Contains(text, "blocked") || strings.Contains(text, "changes requested"):
		return "blocked"
	case strings.Contains(text, "ready") || strings.Contains(text, "review"):
		return "ready_for_review"
	default:
		return "mentioned"
	}
}

func dedupeGitHubSignals(signals []githubPullRequestSignal) []githubPullRequestSignal {
	seen := map[string]struct{}{}
	out := make([]githubPullRequestSignal, 0, len(signals))
	for _, signal := range signals {
		key := signal.CardID + "|" + signal.URL + "|" + signal.Title + "|" + signal.State
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, signal)
	}
	if len(out) > 25 {
		return out[:25]
	}
	return out
}

func buildProductProofMetrics(meetingType string, startedAt string, endedAt string, now time.Time, jiraChanges []boardMutationView, agentRuns []agentRunView, meeting scrumMeetingState, pending []pendingConfirmationView) productProofMetrics {
	baseline := baselineMeetingMinutes(meetingType)
	actual, quality := actualMeetingMinutes(startedAt, endedAt, now)
	jiraChangeCount := countExternallyConfirmedJiraChanges(jiraChanges)
	if jiraChangeCount == 0 {
		jiraChangeCount = len(jiraChanges)
	}
	completedAgentRuns := 0
	agentNeedsHuman := 0
	needsTooling := 0
	for _, run := range agentRuns {
		switch run.Status {
		case agentRunCompleted:
			completedAgentRuns++
		case agentRunNeedsInput, agentRunWaitingOnHuman, agentRunFailed, agentRunUnsupported, agentRunCancelled, agentRunTakenOver, agentRunRetrying:
			agentNeedsHuman++
		}
		if agentRunMentionsNeedsTooling(run) {
			needsTooling++
		}
	}

	humanFallback := len(meeting.UnresolvedBlockers) + len(pending) + agentNeedsHuman
	adminAvoided := jiraChangeCount*2 + completedAgentRuns*15
	netSaved := adminAvoided
	if actual > 0 {
		netSaved += maxInt(0, baseline-actual)
	}
	automatedUnits := jiraChangeCount + completedAgentRuns
	totalDecisionUnits := automatedUnits + humanFallback
	automationRate := 0.0
	if totalDecisionUnits > 0 {
		automationRate = float64(automatedUnits) / float64(totalDecisionUnits)
	}
	evidence := []string{
		fmt.Sprintf("%d Jira change%s handled during the meeting.", jiraChangeCount, plural(jiraChangeCount)),
		fmt.Sprintf("%d completed agent run%s.", completedAgentRuns, plural(completedAgentRuns)),
	}
	if actual > 0 {
		evidence = append(evidence, fmt.Sprintf("Meeting ran %d minute%s against a %d minute baseline.", actual, plural(actual), baseline))
	} else {
		evidence = append(evidence, fmt.Sprintf("No completed meeting duration yet; using a %d minute %s baseline.", baseline, firstNonEmpty(meetingType, "meeting")))
	}
	if humanFallback > 0 {
		evidence = append(evidence, fmt.Sprintf("%d human fallback signal%s remain.", humanFallback, plural(humanFallback)))
	}

	return productProofMetrics{
		BaselineMeetingMinutes:       baseline,
		ActualMeetingMinutes:         actual,
		EstimatedAdminMinutesAvoided: adminAvoided,
		EstimatedNetMinutesSaved:     maxInt(0, netSaved),
		JiraChangesAutomated:         jiraChangeCount,
		AgentRunsStarted:             len(agentRuns),
		AgentRunsCompleted:           completedAgentRuns,
		AgentRunsNeedingHuman:        agentNeedsHuman,
		NeedsToolingEscalations:      needsTooling,
		HumanFallbackSignals:         humanFallback,
		AutomationRate:               roundFloat(automationRate, 2),
		MeasurementQuality:           quality,
		Evidence:                     evidence,
	}
}

func baselineMeetingMinutes(meetingType string) int {
	if override := strings.TrimSpace(os.Getenv("AUTO_BOT_BASELINE_MEETING_MINUTES")); override != "" {
		if parsed, err := strconv.Atoi(override); err == nil && parsed > 0 && parsed <= 480 {
			return parsed
		}
	}
	switch strings.ToLower(strings.TrimSpace(meetingType)) {
	case meetingTypeStandup, string(scrumMeetingModeStandup):
		return 15
	case meetingTypeOneOnOne:
		return 30
	case meetingTypeSprintReview:
		return 60
	case meetingTypeOpenEnded:
		return 30
	default:
		return 30
	}
}

func actualMeetingMinutes(startedAt string, endedAt string, now time.Time) (int, string) {
	start := parseOptionalTime(startedAt)
	if start.IsZero() {
		return 0, "estimated_no_start_time"
	}
	end := parseOptionalTime(endedAt)
	quality := "measured"
	if end.IsZero() {
		end = now
		quality = "estimated_active_meeting"
	}
	if end.Before(start) {
		return 0, "estimated_invalid_duration"
	}
	duration := end.Sub(start)
	if duration <= 0 {
		return 0, quality
	}
	minutes := int(duration / time.Minute)
	if duration%time.Minute != 0 {
		minutes++
	}
	return minutes, quality
}

func countExternallyConfirmedJiraChanges(changes []boardMutationView) int {
	count := 0
	for _, change := range changes {
		for _, confirmation := range change.ExternalConfirmations {
			if strings.EqualFold(confirmation.System, "jira") && confirmation.OK {
				count++
				break
			}
		}
	}
	return count
}

func agentRunMentionsNeedsTooling(run agentRunView) bool {
	text := strings.ToLower(strings.Join([]string{run.Summary, run.Error, run.CurrentStep}, " "))
	if strings.Contains(text, "needs-tooling") || strings.Contains(text, "needs tooling") {
		return true
	}
	for _, checkpoint := range run.Checkpoints {
		text := strings.ToLower(checkpoint.Message + " " + checkpoint.Step)
		if strings.Contains(text, "needs-tooling") || strings.Contains(text, "needs tooling") {
			return true
		}
	}
	return false
}

func roundFloat(value float64, places int) float64 {
	if places <= 0 {
		return float64(int(value + 0.5))
	}
	scale := 1.0
	for i := 0; i < places; i++ {
		scale *= 10
	}
	if value >= 0 {
		return float64(int(value*scale+0.5)) / scale
	}
	return float64(int(value*scale-0.5)) / scale
}

func buildOpenQuestions(meeting scrumMeetingState, cards []kanbanCard) []string {
	questions := make([]string, 0)
	for _, blocker := range meeting.UnresolvedBlockers {
		if blocker.Status == "" || blocker.Status == "open" {
			owner := firstNonEmpty(blocker.Owner, "someone")
			questions = append(questions, fmt.Sprintf("What is %s's next step to unblock %s?", owner, firstNonEmpty(blocker.CardID, blocker.Text)))
		}
	}
	for _, followUp := range meeting.FollowUps {
		if followUp.Status == "" || followUp.Status == "open" {
			questions = append(questions, fmt.Sprintf("Is %s still owning %s?", firstNonEmpty(followUp.Owner, "someone"), followUp.Text))
		}
	}
	for _, card := range cards {
		if card.Status != kanbanStatusDone && card.Assignee == nil {
			questions = append(questions, "Who should own "+card.ID+"?")
		}
	}
	return uniqueStrings(limitStrings(questions, 12))
}

func buildMeetingObservability(sequence int64, access meetingAccessSnapshot) meetingObservability {
	status := voiceReadinessResponse{}
	if voiceProvider != "" {
		status = currentVoiceReadiness(context.Background(), false)
	}
	activeParticipants := 0
	if access.Active {
		activeParticipants = len(access.Participants)
	}
	return meetingObservability{
		VoiceProvider:             firstNonEmpty(voiceProvider, "openai"),
		VoiceProviderReady:        status.Ready || voiceProvider == "openai",
		LiveKitBrowserURL:         safeLiveKitBrowserURL(),
		LiveKitDeploymentMode:     firstNonEmpty(os.Getenv("LIVEKIT_DEPLOYMENT_MODE"), "self-hosted"),
		JiraConfigured:            jiraSync != nil,
		StoragePersistent:         sharedBoard != nil && sharedBoard.store != nil,
		BoardSequenceNumber:       sequence,
		LastTranscriptionAt:       status.LastTranscriptionAt,
		AgentParticipantPresent:   status.AgentParticipantPresent,
		BedrockStreamActive:       status.BedrockStreamActive,
		MeetingAccessActive:       access.Active,
		ActiveMeetingParticipants: activeParticipants,
	}
}

func buildSetupReadinessReport() setupReadinessReport {
	checks := []setupReadinessCheck{
		{
			Name:     "Auth",
			OK:       appAuthMode != "disabled" || appEnvironment == "local",
			Status:   "mode=" + firstNonEmpty(appAuthMode, "token"),
			Remedy:   "Use OIDC/Cognito before public access.",
			Required: true,
		},
		{
			Name:     "Identity",
			OK:       identityProviderMode() != "local",
			Status:   identityProviderMode(),
			Remedy:   "Configure Cognito/OIDC or trusted ALB identity headers for production.",
			Required: false,
		},
		{
			Name:     "Jira",
			OK:       jiraSync != nil,
			Status:   boolStatus(jiraSync != nil, "configured", "not configured"),
			Remedy:   "Run the Jira setup validator and provide config/token through Keychain or Secrets Manager.",
			Required: true,
		},
		{
			Name:     "GitHub App agent access",
			OK:       githubSetupConfigured(),
			Status:   boolStatus(githubSetupConfigured(), "configured", "not configured"),
			Remedy:   "Create a least-privilege GitHub App, install it only on the target repo, and inject app id, installation id, and private key through Keychain or Secrets Manager.",
			Required: false,
		},
		{
			Name:     "Bedrock agent models",
			OK:       agentPMModel() != "" && agentReviewModel() != "",
			Status:   fmt.Sprintf("pm=%s review=%s", agentPMModel(), agentReviewModel()),
			Remedy:   "Keep agent models on AWS Bedrock; defaults are Claude Haiku 4.5 for PM classification and Claude Opus 4.5 for code review.",
			Required: false,
		},
		{
			Name:     "Persistent meeting memory",
			OK:       sharedBoard != nil && sharedBoard.store != nil,
			Status:   boolStatus(sharedBoard != nil && sharedBoard.store != nil, "sqlite enabled", "in-memory only"),
			Remedy:   "Set BOARD_SQLITE_PATH locally and use Postgres/RDS in AWS when multi-room history becomes required.",
			Required: true,
		},
		{
			Name:     "LiveKit",
			OK:       os.Getenv("LIVEKIT_API_KEY") != "" && os.Getenv("LIVEKIT_API_SECRET") != "",
			Status:   firstNonEmpty(os.Getenv("LIVEKIT_DEPLOYMENT_MODE"), "self-hosted"),
			Remedy:   "Keep self-hosted locally; switch Terraform to LiveKit Cloud with LIVEKIT_DEPLOYMENT_MODE=cloud when needed.",
			Required: true,
		},
	}
	return setupReadinessReport{
		AuthMode:         firstNonEmpty(appAuthMode, "token"),
		IdentityProvider: identityProviderMode(),
		Region:           getEnvDefault("AWS_REGION", "us-east-1"),
		Checks:           checks,
		ProviderOptions:  voiceProviderOptions(),
		AdminActions: []string{
			"Validate Jira scopes and workflow mappings.",
			"Run the multi-participant LiveKit proof before demos.",
			"Review post-meeting intelligence after each dry run.",
		},
		Metadata: map[string]string{
			"board_id": appBoardID,
			"room_id":  appRoomID,
		},
	}
}

func voiceProviderOptions() []voiceProviderOption {
	if options := registeredVoiceProviderOptions(); len(options) > 0 {
		return options
	}
	return []voiceProviderOption{
		{Name: "nova-sonic", Enabled: voiceProvider == "nova-sonic", FullDuplex: true, Transport: "LiveKit", Notes: "Current AWS Bedrock Nova Sonic path."},
		{Name: "openai-realtime", Enabled: voiceProvider == "openai", FullDuplex: true, Transport: "WebRTC", Notes: "OpenAI voice-to-action path using " + defaultRealtimeModel + " with " + defaultRealtimeTranscriptionModel + " transcripts."},
		{Name: "openai-realtime-translate", Enabled: false, FullDuplex: true, Transport: "WebRTC", Notes: "Dedicated translation endpoint profile; no Jira/GitHub tool calling."},
		{Name: "openai-realtime-whisper", Enabled: false, FullDuplex: false, Transport: "WebRTC/WebSocket", Notes: "Dedicated streaming transcription endpoint profile; no Jira/GitHub tool calling."},
		{Name: "livekit-cloud", Enabled: strings.EqualFold(os.Getenv("LIVEKIT_DEPLOYMENT_MODE"), "cloud"), FullDuplex: true, Transport: "LiveKit Cloud", Notes: "Terraform switch via LIVEKIT_DEPLOYMENT_MODE=cloud."},
	}
}

func identityProviderMode() string {
	if value := strings.TrimSpace(os.Getenv("APP_IDENTITY_PROVIDER")); value != "" {
		return value
	}
	if os.Getenv("COGNITO_USER_POOL_ID") != "" {
		return "cognito"
	}
	if os.Getenv("TRUSTED_IDENTITY_HEADER") != "" || os.Getenv("TRUST_PROXY_HEADERS") != "" {
		return "trusted_headers"
	}
	if appEnvironment == "local" {
		return "local"
	}
	return "shared_token"
}

func safeLiveKitBrowserURL() string {
	if value := strings.TrimSpace(os.Getenv("LIVEKIT_BROWSER_URL")); value != "" {
		return value
	}
	if strings.EqualFold(os.Getenv("LIVEKIT_DEPLOYMENT_MODE"), "cloud") {
		return strings.TrimSpace(os.Getenv("LIVEKIT_CLOUD_URL"))
	}
	return ""
}

func boolStatus(ok bool, yes string, no string) string {
	if ok {
		return yes
	}
	return no
}

func parseOptionalTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func cloneScrumBriefing(briefing *scrumBriefing) *scrumBriefing {
	if briefing == nil {
		return nil
	}
	cloned := *briefing
	cloned.StaleCards = append([]string(nil), briefing.StaleCards...)
	cloned.UnresolvedBlockers = append([]string(nil), briefing.UnresolvedBlockers...)
	cloned.RecommendedQuestions = append([]string(nil), briefing.RecommendedQuestions...)
	return &cloned
}

func formatFollowUpSummary(followUp scrumFollowUp) string {
	owner := firstNonEmpty(followUp.Owner, "Unassigned")
	text := followUp.Text
	if followUp.CardID != "" {
		text = followUp.CardID + ": " + text
	}
	if followUp.DueDate != "" {
		text += " due " + followUp.DueDate
	}
	return owner + " - " + text
}

func limitStrings(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func (board *kanbanBoard) persistMeetingReport(report meetingIntelligenceReport) {
	store, ok := board.store.(meetingReportStore)
	if !ok || report.MeetingID == "" {
		return
	}
	if err := store.SaveMeetingReport(context.Background(), report); err != nil {
		log.Errorf("Failed to persist meeting report: %v", err)
	}
}

func (board *kanbanBoard) archiveMeetingReport(source string) meetingIntelligenceReport {
	report := board.BuildMeetingIntelligenceReport(source)
	board.persistMeetingReport(report)
	broadcastKanbanEventForBoard(board.tenantID, board.boardID, "meeting_report", report.SummaryView())
	// Sprint 4.1: post-meeting closer materializes follow-ups + blockers
	// as cards and kicks Runs for agent-assigned items. Failures are
	// non-fatal — the report has already been archived above.
	runClosersOnReport(report)
	return report
}

func loadMeetingReportFromStore(boardID string, meetingID string) (meetingIntelligenceReport, bool, error) {
	if sharedBoard == nil || sharedBoard.store == nil {
		return meetingIntelligenceReport{}, false, nil
	}
	store, ok := sharedBoard.store.(meetingReportStore)
	if !ok {
		return meetingIntelligenceReport{}, false, nil
	}
	return store.LoadMeetingReport(context.Background(), sharedBoard.tenantID, boardID, meetingID)
}

func listMeetingReportsFromStore(boardID string, limit int) ([]meetingReportSummary, error) {
	if sharedBoard == nil || sharedBoard.store == nil {
		return nil, nil
	}
	store, ok := sharedBoard.store.(meetingReportStore)
	if !ok {
		return nil, nil
	}
	return store.ListMeetingReports(context.Background(), sharedBoard.tenantID, boardID, limit)
}
