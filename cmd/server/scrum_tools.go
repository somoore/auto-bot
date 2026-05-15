package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (board *kanbanBoard) createSubtask(args map[string]any) (map[string]any, bool, error) {
	parentID := firstNonEmptyString(args, "parent_id", "parent_card_id")
	if parentID == "" {
		return nil, false, fmt.Errorf("parent_id or parent_card_id is required")
	}

	nextArgs := cloneToolArgs(args)
	nextArgs["parent_id"] = parentID
	nextArgs["issue_type"] = "Sub-task"
	if asString(nextArgs["status"]) == "" {
		nextArgs["status"] = string(kanbanStatusBacklog)
	}

	result, changed, err := board.createTicket(nextArgs)
	if err != nil {
		return nil, false, err
	}
	card, _ := result["card"].(kanbanCard)
	result["subtask"] = true
	result["card_id"] = card.ID
	result["parent_id"] = parentID
	return result, changed, nil
}

func (board *kanbanBoard) setStoryPoints(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	points, ok := asFloat64(args["points"])
	if !ok {
		points, ok = asFloat64(args["story_points"])
	}
	if !ok {
		return nil, false, fmt.Errorf("points or story_points is required")
	}
	if points < 0 {
		return nil, false, fmt.Errorf("story points must be non-negative")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.StoryPoints = &points
	board.touchLocked()

	return map[string]any{
		"ok":           true,
		"card_id":      cardID,
		"story_points": points,
		"points":       points,
	}, true, nil
}

func (board *kanbanBoard) setEstimate(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	original := truncateString(asString(args["original_estimate"]), 80)
	remaining := truncateString(asString(args["remaining_estimate"]), 80)
	if original == "" && remaining == "" {
		return nil, false, fmt.Errorf("original_estimate or remaining_estimate is required")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	if original != "" {
		card.OriginalEstimate = original
	}
	if remaining != "" {
		card.RemainingEstimate = remaining
	}
	card.Estimate = &kanbanEstimate{
		Original:  card.OriginalEstimate,
		Remaining: card.RemainingEstimate,
	}
	board.touchLocked()

	return map[string]any{
		"ok":       true,
		"card_id":  cardID,
		"estimate": *card.Estimate,
	}, true, nil
}

func (board *kanbanBoard) addWorklog(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	timeSpent := truncateString(asString(args["time_spent"]), 80)
	if timeSpent == "" {
		return nil, false, fmt.Errorf("time_spent is required")
	}
	started := firstNonEmptyString(args, "started", "started_at")
	comment := truncateString(asString(args["comment"]), 2000)
	seconds, _ := asInt(args["time_spent_seconds"])

	worklog := kanbanWorklog{
		TimeSpent:        timeSpent,
		TimeSpentSeconds: int64(seconds),
		Started:          truncateString(started, 80),
		Comment:          comment,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.Worklogs = append(card.Worklogs, worklog)
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"card_id": cardID,
		"worklog": worklog,
	}, true, nil
}

func (board *kanbanBoard) linkIssues(args map[string]any) (map[string]any, bool, error) {
	cardID := firstNonEmptyString(args, "card_id", "source_card_id")
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id or source_card_id is required")
	}
	targetID := asString(args["target_card_id"])
	if targetID == "" {
		return nil, false, fmt.Errorf("target_card_id is required")
	}
	if strings.EqualFold(cardID, targetID) {
		return nil, false, fmt.Errorf("cannot link a card to itself")
	}
	linkType := truncateString(asString(args["link_type"]), 80)
	if linkType == "" {
		linkType = "Relates"
	}
	direction := strings.ToLower(asString(args["direction"]))
	if direction == "" {
		direction = "outward"
	}
	if direction != "outward" && direction != "inward" {
		return nil, false, fmt.Errorf("direction must be outward or inward")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	link := kanbanIssueLink{
		Type:           linkType,
		Direction:      direction,
		SourceCardID:   cardID,
		TargetCardID:   targetID,
		Relationship:   truncateString(firstNonEmptyString(args, "relationship", "comment"), 500),
		CreatedByVoice: true,
	}
	if target, targetOK := board.findCardLocked(targetID); targetOK {
		link.TargetSummary = target.Title
		link.TargetStatus = string(target.Status)
	}
	card.IssueLinks = append(card.IssueLinks, link)
	board.touchLocked()

	return map[string]any{
		"ok":         true,
		"card_id":    cardID,
		"issue_link": link,
	}, true, nil
}

func (board *kanbanBoard) setSprint(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	sprintID, ok := asInt(args["sprint_id"])
	if !ok || sprintID <= 0 {
		return nil, false, fmt.Errorf("sprint_id is required")
	}
	sprint := kanbanSprint{
		ID:    sprintID,
		Name:  truncateString(asString(args["sprint_name"]), 120),
		State: truncateString(asString(args["state"]), 40),
		Goal:  truncateString(asString(args["goal"]), 500),
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.Sprint = &sprint
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"card_id": cardID,
		"sprint":  sprint,
	}, true, nil
}

func (board *kanbanBoard) rankIssue(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	beforeID := asString(args["before_card_id"])
	afterID := asString(args["after_card_id"])
	if beforeID != "" && afterID != "" {
		return nil, false, fmt.Errorf("only one of before_card_id or after_card_id may be set")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	index := board.cardIndexLocked(cardID)
	if index < 0 {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card := board.cards[index]
	rank := "manual"
	targetID := ""
	insertIndex := index
	if beforeID != "" || afterID != "" {
		targetID = beforeID
		if targetID == "" {
			targetID = afterID
		}
		targetIndex := board.cardIndexLocked(targetID)
		if targetIndex < 0 {
			return nil, false, fmt.Errorf("unknown target card_id: %s", targetID)
		}
		board.cards = append(board.cards[:index], board.cards[index+1:]...)
		if targetIndex > index {
			targetIndex--
		}
		insertIndex = targetIndex
		if afterID != "" {
			insertIndex = targetIndex + 1
			rank = "after " + afterID
		} else {
			rank = "before " + beforeID
		}
		if insertIndex < 0 {
			insertIndex = 0
		}
		if insertIndex > len(board.cards) {
			insertIndex = len(board.cards)
		}
		board.cards = append(board.cards[:insertIndex], append([]kanbanCard{card}, board.cards[insertIndex:]...)...)
	}
	rankedCard, _ := board.findCardLocked(cardID)
	rankedCard.Rank = rank
	rankedCard.RankHint = rank
	board.touchLocked()

	return map[string]any{
		"ok":             true,
		"card_id":        cardID,
		"rank":           rank,
		"target_card_id": targetID,
	}, true, nil
}

func (board *kanbanBoard) setComponents(args map[string]any) (map[string]any, bool, error) {
	return board.setStringListField(args, "components")
}

func (board *kanbanBoard) setFixVersions(args map[string]any) (map[string]any, bool, error) {
	return board.setStringListField(args, "fix_versions")
}

func (board *kanbanBoard) setStringListField(args map[string]any, field string) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	values := uniqueStrings(asStringSlice(args[field]))
	for index := range values {
		values[index] = truncateString(values[index], 120)
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	if field == "components" {
		card.Components = values
	} else {
		card.FixVersions = values
	}
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"card_id": cardID,
		field:     append([]string(nil), values...),
	}, true, nil
}

func (board *kanbanBoard) setCustomField(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	fieldID := asString(args["field_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	if fieldID == "" {
		return nil, false, fmt.Errorf("field_id is required")
	}
	value, ok := args["value"]
	if !ok {
		return nil, false, fmt.Errorf("value is required")
	}
	field := kanbanField{
		Name:  truncateString(firstNonEmptyString(args, "display_name", "field_name"), 120),
		Value: normalizeCustomFieldValue(value, asString(args["value_type"])),
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	if card.CustomFields == nil {
		card.CustomFields = map[string]kanbanField{}
	}
	card.CustomFields[fieldID] = field
	board.touchLocked()

	return map[string]any{
		"ok":       true,
		"card_id":  cardID,
		"field_id": fieldID,
		"field":    field,
	}, true, nil
}

func (board *kanbanBoard) addRemoteLink(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	rawURL := asString(args["url"])
	title := truncateString(asString(args["title"]), 200)
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	if rawURL == "" || title == "" {
		return nil, false, fmt.Errorf("url and title are required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, false, fmt.Errorf("url must be an absolute URL")
	}
	link := kanbanRemoteLink{
		URL:     rawURL,
		Title:   title,
		Summary: truncateString(asString(args["summary"]), 500),
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.RemoteLinks = append(card.RemoteLinks, link)
	board.touchLocked()

	return map[string]any{
		"ok":          true,
		"card_id":     cardID,
		"remote_link": link,
	}, true, nil
}

func (board *kanbanBoard) setReporter(args map[string]any) (map[string]any, bool, error) {
	user, result, err := userFromArgsOrSearch(args)
	if err != nil || user.AccountID == "" {
		return result, false, err
	}
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.Reporter = &user
	board.touchLocked()

	return map[string]any{
		"ok":       true,
		"card_id":  cardID,
		"reporter": user,
	}, true, nil
}

func (board *kanbanBoard) addWatcher(args map[string]any) (map[string]any, bool, error) {
	user, result, err := userFromArgsOrSearch(args)
	if err != nil || user.AccountID == "" {
		return result, false, err
	}
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	for _, watcher := range card.Watchers {
		if watcher.AccountID == user.AccountID {
			return map[string]any{
				"ok":      true,
				"card_id": cardID,
				"watcher": user,
				"already": true,
			}, false, nil
		}
	}
	card.Watchers = append(card.Watchers, user)
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"card_id": cardID,
		"watcher": user,
	}, true, nil
}

func (board *kanbanBoard) getJiraMetadata() (map[string]any, bool, error) {
	if jiraSync == nil {
		return fallbackJiraMetadata(), false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	metadata, err := jiraSync.client.GetMetadata(ctx)
	if err != nil {
		return map[string]any{
			"ok":       false,
			"error":    err.Error(),
			"fallback": fallbackJiraMetadata(),
		}, false, nil
	}
	metadata["ok"] = true
	return metadata, false, nil
}

func (board *kanbanBoard) getTransitionOptions(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}
	if jiraSync == nil {
		return map[string]any{
			"ok":       true,
			"card_id":  cardID,
			"statuses": kanbanStatuses,
			"transitions": []map[string]any{
				{"name": "Backlog", "to": kanbanStatusBacklog},
				{"name": "Start Progress", "to": kanbanStatusInProgress},
				{"name": "Block", "to": kanbanStatusBlocked},
				{"name": "Done", "to": kanbanStatusDone},
			},
		}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transitions, err := jiraSync.client.GetTransitions(ctx, cardID)
	if err != nil {
		return map[string]any{
			"ok":      false,
			"card_id": cardID,
			"error":   err.Error(),
		}, false, nil
	}
	return map[string]any{
		"ok":          true,
		"card_id":     cardID,
		"transitions": transitions,
		"statuses":    kanbanStatuses,
	}, false, nil
}

func (board *kanbanBoard) startMeeting(args map[string]any) (map[string]any, bool, error) {
	mode := normalizeScrumMeetingMode(firstNonEmptyString(args, "mode", "meeting_type"))
	meetingID := firstNonEmptyString(args, "meeting_id")
	if meetingID == "" {
		meetingID = fmt.Sprintf("%s-%s", mode, time.Now().UTC().Format("20060102T150405Z"))
	}
	participants := meetingParticipantsFromArg(args["participants"])
	goal := truncateString(asString(args["goal"]), 1000)
	if goal == "" && asString(args["sprint_name"]) != "" {
		goal = "Facilitate " + asString(args["sprint_name"])
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	board.meeting = scrumMeetingState{
		MeetingID:    meetingID,
		Active:       true,
		Mode:         mode,
		Goal:         goal,
		SprintID:     truncateString(asString(args["sprint_id"]), 80),
		SprintName:   truncateString(asString(args["sprint_name"]), 120),
		Agenda:       uniqueStrings(asStringSlice(args["agenda"])),
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		Participants: participants,
	}
	if len(participants) > 0 {
		board.meeting.CurrentSpeaker = participants[0].Name
	}
	briefing := board.scrumBriefingLocked(time.Now().UTC().Add(-24 * time.Hour))
	board.meeting.LastBriefing = &briefing
	board.touchLocked()

	return map[string]any{
		"ok":              true,
		"status":          "meeting_active",
		"meeting":         cloneScrumMeetingState(board.meeting),
		"meeting_id":      meetingID,
		"current_speaker": board.meeting.CurrentSpeaker,
		"briefing":        briefing,
		"briefing_text":   briefing.Summary,
	}, true, nil
}

func (board *kanbanBoard) registerParticipant(args map[string]any) (map[string]any, bool, error) {
	participant := participantFromArgs(args)
	if participant.Name == "" {
		return nil, false, fmt.Errorf("name or display_name is required")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	board.upsertParticipantLocked(participant)
	if board.meeting.CurrentSpeaker == "" {
		board.meeting.CurrentSpeaker = participant.Name
	}
	board.touchLocked()

	return map[string]any{
		"ok":          true,
		"participant": participant,
		"meeting":     cloneScrumMeetingState(board.meeting),
	}, true, nil
}

func (board *kanbanBoard) recordParticipantUpdate(args map[string]any) (map[string]any, bool, error) {
	update := scrumParticipantUpdate{
		ParticipantID: truncateString(asString(args["participant_id"]), 120),
		Participant:   truncateString(firstNonEmptyString(args, "participant", "display_name"), 120),
		CardID:        firstRelatedCardID(args),
		Summary:       truncateString(firstNonEmptyString(args, "summary", "spoken_text"), 2000),
		Completed:     uniqueStrings(asStringSlice(args["completed"])),
		Planned:       uniqueStrings(asStringSlice(args["planned"])),
		Risks:         uniqueStrings(asStringSlice(args["risks"])),
		ETA:           truncateString(asString(args["eta"]), 40),
		FollowUp:      truncateString(asString(args["follow_up"]), 1000),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	blockers := uniqueStrings(asStringSlice(args["blockers"]))
	if blocker := asString(args["blocker"]); blocker != "" {
		blockers = uniqueStrings(append(blockers, blocker))
	}
	update.Blocker = strings.Join(blockers, "; ")
	if update.Participant == "" {
		update.Participant = update.ParticipantID
	}
	if update.Participant == "" {
		return nil, false, fmt.Errorf("participant, display_name, or participant_id is required")
	}
	if update.Summary == "" {
		update.Summary = compactStatusSummary(update.Completed, update.Planned, blockers, update.Risks)
	}
	if update.Summary == "" {
		return nil, false, fmt.Errorf("summary or spoken_text is required")
	}
	if rawStatus, ok := args["status"]; ok && asString(rawStatus) != "" {
		status, err := parseKanbanStatus(rawStatus)
		if err != nil {
			return nil, false, err
		}
		update.Status = status
	} else if update.Blocker != "" {
		update.Status = kanbanStatusBlocked
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	board.upsertParticipantLocked(scrumParticipant{
		ParticipantID: update.ParticipantID,
		Name:          update.Participant,
		HasSpoken:     true,
		LastUpdate:    update.Summary,
	})
	board.markParticipantSpokenLocked(update.ParticipantID, update.Participant, update.Summary)
	board.meeting.Updates = append(board.meeting.Updates, update)
	board.meeting.CurrentSpeaker = update.Participant
	if update.Blocker != "" {
		board.meeting.Risks = uniqueStrings(append(board.meeting.Risks, update.Blocker))
	}
	board.meeting.Risks = uniqueStrings(append(board.meeting.Risks, update.Risks...))
	if update.FollowUp != "" {
		board.meeting.ActionItems = uniqueStrings(append(board.meeting.ActionItems, update.FollowUp))
	}
	board.syncMeetingMemoryFromUpdateLocked(update)

	var ticketChange string
	if update.CardID != "" {
		if card, ok := board.findCardLocked(update.CardID); ok {
			if update.Status != "" {
				card.Status = update.Status
			}
			if update.ETA != "" {
				card.DueDate = update.ETA
			}
			if update.Blocker != "" {
				card.Status = kanbanStatusBlocked
				card.BlockedReason = update.Blocker
				card.Tags = uniqueStrings(append(card.Tags, "blocked", "risk"))
			}
			appendMeetingNote(card, update)
			ticketChange = fmt.Sprintf("%s updated by %s", card.ID, update.Participant)
		}
	}
	board.touchLocked()

	result := map[string]any{
		"ok":                 true,
		"participant_update": "recorded",
		"update":             update,
		"meeting":            cloneScrumMeetingState(board.meeting),
	}
	if ticketChange != "" {
		result["ticket_changes"] = []string{ticketChange}
	}
	return result, true, nil
}

func (board *kanbanBoard) nextSpeaker(args map[string]any) (map[string]any, bool, error) {
	current := firstNonEmptyString(args, "current_participant", "current_participant_id")

	board.mu.Lock()
	defer board.mu.Unlock()

	next := board.nextSpeakerLocked(current)
	if next.Name == "" {
		return map[string]any{
			"ok":      true,
			"prompt":  "Everyone has spoken. Summarize blockers, decisions, and action items.",
			"meeting": cloneScrumMeetingState(board.meeting),
		}, false, nil
	}
	board.meeting.CurrentSpeaker = next.Name
	board.touchLocked()

	return map[string]any{
		"ok":               true,
		"next_participant": next,
		"speaker":          next.Name,
		"prompt":           fmt.Sprintf("%s, what did you complete, what are you doing next, and are you blocked?", next.Name),
		"meeting":          cloneScrumMeetingState(board.meeting),
	}, true, nil
}

func (board *kanbanBoard) summarizeMeeting() (map[string]any, bool, error) {
	board.mu.Lock()
	defer board.mu.Unlock()

	summary := board.meetingSummaryLocked()
	summary["ok"] = true
	return summary, false, nil
}

func (board *kanbanBoard) endMeeting(args map[string]any) (map[string]any, bool, error) {
	board.mu.Lock()
	defer board.mu.Unlock()

	if decision := truncateString(asString(args["decision"]), 1000); decision != "" {
		board.meeting.Decisions = uniqueStrings(append(board.meeting.Decisions, decision))
	}
	if actionItems := asStringSlice(args["action_items"]); len(actionItems) > 0 {
		board.meeting.ActionItems = uniqueStrings(append(board.meeting.ActionItems, actionItems...))
	}
	board.meeting.Active = false
	board.meeting.EndedAt = time.Now().UTC().Format(time.RFC3339)
	board.touchLocked()

	summary := board.meetingSummaryLocked()
	summary["ok"] = true
	summary["ended"] = true
	summary["status"] = "meeting_ended"
	summary["meeting"] = cloneScrumMeetingState(board.meeting)
	return summary, true, nil
}

func (board *kanbanBoard) cardIndexLocked(cardID string) int {
	for index := range board.cards {
		if board.cards[index].ID == cardID {
			return index
		}
	}
	return -1
}

func cloneToolArgs(args map[string]any) map[string]any {
	cloned := make(map[string]any, len(args)+2)
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}

func normalizeCustomFieldValue(value any, valueType string) any {
	switch strings.ToLower(strings.TrimSpace(valueType)) {
	case "", "json", "object", "array":
		return value
	case "string", "text":
		return asString(value)
	case "number", "float":
		if parsed, ok := asFloat64(value); ok {
			return parsed
		}
	case "integer", "int":
		if parsed, ok := asInt(value); ok {
			return parsed
		}
	case "boolean", "bool":
		if parsed, ok := value.(bool); ok {
			return parsed
		}
	}
	return value
}

func userFromArgsOrSearch(args map[string]any) (kanbanUser, map[string]any, error) {
	user := kanbanUser{
		AccountID:    asString(args["account_id"]),
		DisplayName:  firstNonEmptyString(args, "display_name", "name"),
		EmailAddress: asString(args["email_address"]),
		Active:       true,
	}
	if user.AccountID != "" {
		if user.DisplayName == "" {
			user.DisplayName = user.AccountID
		}
		return user, nil, nil
	}
	query := asString(args["query"])
	if query == "" {
		return kanbanUser{}, map[string]any{"ok": false, "error": "account_id or query is required."}, nil
	}
	resolved, candidates, err := resolveAssignableUser(query)
	if err != nil {
		return kanbanUser{}, map[string]any{"ok": false, "error": err.Error()}, nil
	}
	if resolved.AccountID == "" {
		return kanbanUser{}, map[string]any{"ok": false, "error": "user search did not resolve to exactly one Jira user.", "candidates": candidates}, nil
	}
	return resolved, nil, nil
}

func fallbackJiraMetadata() map[string]any {
	return map[string]any{
		"ok":           true,
		"source":       "local_fallback",
		"issue_types":  []string{"Epic", "Story", "Task", "Bug", "Sub-task"},
		"fields":       []string{"summary", "description", "labels", "assignee", "reporter", "duedate", "priority", "components", "fixVersions", "timetracking"},
		"link_types":   []string{"Blocks", "Relates", "Duplicate", "Cloners"},
		"components":   []string{},
		"fix_versions": []string{},
		"sprints":      []kanbanSprint{},
		"priorities":   []string{"Highest", "High", "Medium", "Low", "Lowest"},
		"statuses":     kanbanStatuses,
	}
}

func normalizeScrumMeetingMode(value string) scrumMeetingMode {
	switch scrumMeetingMode(strings.ToLower(strings.TrimSpace(value))) {
	case scrumMeetingModePlanning:
		return scrumMeetingModePlanning
	case scrumMeetingModeGrooming, "refinement", "backlog_refinement":
		return scrumMeetingModeGrooming
	case scrumMeetingModeReview:
		return scrumMeetingModeReview
	case scrumMeetingModeRetro:
		return scrumMeetingModeRetro
	default:
		return scrumMeetingModeStandup
	}
}

func meetingParticipantsFromArg(value any) []scrumParticipant {
	rawValues, ok := value.([]any)
	if !ok {
		names := asStringSlice(value)
		participants := make([]scrumParticipant, 0, len(names))
		for _, name := range names {
			participants = append(participants, scrumParticipant{Name: truncateString(name, 120)})
		}
		return participants
	}

	participants := make([]scrumParticipant, 0, len(rawValues))
	for _, raw := range rawValues {
		switch typed := raw.(type) {
		case string:
			if name := truncateString(typed, 120); name != "" {
				participants = append(participants, scrumParticipant{Name: name})
			}
		case map[string]any:
			participant := participantFromArgs(typed)
			if participant.Name != "" {
				participants = append(participants, participant)
			}
		}
	}
	return participants
}

func participantFromArgs(args map[string]any) scrumParticipant {
	name := truncateString(firstNonEmptyString(args, "name", "display_name", "participant"), 120)
	participantID := truncateString(firstNonEmptyString(args, "participant_id", "account_id"), 120)
	if name == "" {
		name = participantID
	}
	return scrumParticipant{
		ParticipantID: participantID,
		Name:          name,
		Role:          truncateString(asString(args["role"]), 120),
	}
}

func firstRelatedCardID(args map[string]any) string {
	if cardID := asString(args["card_id"]); cardID != "" {
		return cardID
	}
	for _, ref := range asStringSlice(args["ticket_refs"]) {
		if ref != "" {
			return ref
		}
	}
	return ""
}

func compactStatusSummary(completed, planned, blockers, risks []string) string {
	var parts []string
	if len(completed) > 0 {
		parts = append(parts, "Completed: "+strings.Join(completed, "; "))
	}
	if len(planned) > 0 {
		parts = append(parts, "Planned: "+strings.Join(planned, "; "))
	}
	if len(blockers) > 0 {
		parts = append(parts, "Blockers: "+strings.Join(blockers, "; "))
	}
	if len(risks) > 0 {
		parts = append(parts, "Risks: "+strings.Join(risks, "; "))
	}
	return strings.Join(parts, " ")
}

func appendMeetingNote(card *kanbanCard, update scrumParticipantUpdate) {
	note := fmt.Sprintf("Meeting update from %s: %s", update.Participant, update.Summary)
	if update.Blocker != "" {
		note += "\nBlocker: " + update.Blocker
	}
	if update.ETA != "" {
		note += "\nETA: " + update.ETA
	}
	if card.Notes == "" {
		card.Notes = note
		return
	}
	if !strings.Contains(card.Notes, update.Summary) {
		card.Notes = strings.TrimSpace(card.Notes) + "\n\n" + note
	}
}

func (board *kanbanBoard) upsertParticipantLocked(participant scrumParticipant) {
	if participant.Name == "" {
		return
	}
	for index := range board.meeting.Participants {
		existing := &board.meeting.Participants[index]
		if sameParticipant(*existing, participant.ParticipantID, participant.Name) {
			if participant.ParticipantID != "" {
				existing.ParticipantID = participant.ParticipantID
			}
			if participant.Role != "" {
				existing.Role = participant.Role
			}
			if participant.LastUpdate != "" {
				existing.LastUpdate = participant.LastUpdate
			}
			if participant.HasSpoken {
				existing.HasSpoken = true
			}
			return
		}
	}
	board.meeting.Participants = append(board.meeting.Participants, participant)
}

func (board *kanbanBoard) markParticipantSpokenLocked(participantID string, name string, lastUpdate string) {
	for index := range board.meeting.Participants {
		participant := &board.meeting.Participants[index]
		if sameParticipant(*participant, participantID, name) {
			participant.HasSpoken = true
			participant.LastUpdate = lastUpdate
			if participantID != "" {
				participant.ParticipantID = participantID
			}
			if name != "" {
				participant.Name = name
			}
			return
		}
	}
	board.meeting.Participants = append(board.meeting.Participants, scrumParticipant{
		ParticipantID: participantID,
		Name:          name,
		HasSpoken:     true,
		LastUpdate:    lastUpdate,
	})
}

func (board *kanbanBoard) nextSpeakerLocked(current string) scrumParticipant {
	if len(board.meeting.Participants) == 0 {
		return scrumParticipant{}
	}
	currentIndex := -1
	for index, participant := range board.meeting.Participants {
		if sameParticipant(participant, current, current) || strings.EqualFold(participant.Name, board.meeting.CurrentSpeaker) {
			currentIndex = index
			break
		}
	}
	for offset := 1; offset <= len(board.meeting.Participants); offset++ {
		index := (currentIndex + offset + len(board.meeting.Participants)) % len(board.meeting.Participants)
		if !board.meeting.Participants[index].HasSpoken {
			return board.meeting.Participants[index]
		}
	}
	return scrumParticipant{}
}

func (board *kanbanBoard) meetingSummaryLocked() map[string]any {
	unspoken := make([]string, 0)
	for _, participant := range board.meeting.Participants {
		if !participant.HasSpoken {
			unspoken = append(unspoken, participant.Name)
		}
	}
	ticketChanges := make([]string, 0)
	for _, update := range board.meeting.Updates {
		if update.CardID != "" {
			ticketChanges = append(ticketChanges, fmt.Sprintf("%s: %s", update.CardID, update.Summary))
		}
	}
	summaryText := fmt.Sprintf("%s meeting has %d participant updates, %d blockers/risks, and %d action items.",
		board.meeting.Mode, len(board.meeting.Updates), len(board.meeting.Risks), len(board.meeting.ActionItems))
	return map[string]any{
		"meeting_id":          board.meeting.MeetingID,
		"summary":             summaryText,
		"participants":        append([]scrumParticipant(nil), board.meeting.Participants...),
		"unspoken":            unspoken,
		"updates":             append([]scrumParticipantUpdate(nil), board.meeting.Updates...),
		"blockers":            append([]string(nil), board.meeting.Risks...),
		"risks":               append([]string(nil), board.meeting.Risks...),
		"decisions":           append([]string(nil), board.meeting.Decisions...),
		"action_items":        append([]string(nil), board.meeting.ActionItems...),
		"parking_lot":         append([]string(nil), board.meeting.ParkingLot...),
		"follow_ups":          append([]scrumFollowUp(nil), board.meeting.FollowUps...),
		"unresolved_blockers": append([]scrumBlocker(nil), board.meeting.UnresolvedBlockers...),
		"ownership":           append([]scrumOwnership(nil), board.meeting.Ownership...),
		"briefing":            board.meeting.LastBriefing,
		"ticket_changes":      ticketChanges,
		"current_speaker":     board.meeting.CurrentSpeaker,
	}
}

func sameParticipant(participant scrumParticipant, participantID string, name string) bool {
	if participantID != "" && strings.EqualFold(participant.ParticipantID, participantID) {
		return true
	}
	return name != "" && strings.EqualFold(participant.Name, name)
}
