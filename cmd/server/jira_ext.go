package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (client *jiraClient) UpdateIssueFields(ctx context.Context, cardID string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{"fields": fields}, nil)
}

func (client *jiraClient) SetStoryPoints(ctx context.Context, cardID string, points float64) error {
	fieldID := strings.TrimSpace(client.config.StoryPointsField)
	if fieldID == "" {
		return fmt.Errorf("story_points_field is not configured")
	}
	return client.UpdateIssueFields(ctx, cardID, map[string]any{fieldID: points})
}

func (client *jiraClient) SetEstimate(ctx context.Context, cardID string, originalEstimate string, remainingEstimate string) error {
	timetracking := map[string]any{}
	if original := strings.TrimSpace(originalEstimate); original != "" {
		timetracking["originalEstimate"] = original
	}
	if remaining := strings.TrimSpace(remainingEstimate); remaining != "" {
		timetracking["remainingEstimate"] = remaining
	}
	return client.UpdateIssueFields(ctx, cardID, map[string]any{"timetracking": timetracking})
}

func (client *jiraClient) AddWorklog(ctx context.Context, cardID string, timeSpent string, timeSpentSeconds int64, started string, comment string) error {
	timeSpent = strings.TrimSpace(timeSpent)
	if timeSpent == "" && timeSpentSeconds <= 0 {
		return fmt.Errorf("time_spent or time_spent_seconds is required")
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	body := map[string]any{}
	if timeSpent != "" {
		body["timeSpent"] = timeSpent
	}
	if timeSpentSeconds > 0 {
		body["timeSpentSeconds"] = timeSpentSeconds
	}
	if formatted := jiraWorklogStarted(started); formatted != "" {
		body["started"] = formatted
	}
	if strings.TrimSpace(comment) != "" {
		body["comment"] = jiraADFDocument(comment)
	}
	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/worklog", body, nil)
}

func (client *jiraClient) LinkIssues(ctx context.Context, sourceID string, targetID string, linkType string, direction string, comment string) error {
	sourceID = strings.TrimSpace(sourceID)
	targetID = strings.TrimSpace(targetID)
	if err := client.validateIssueKey(sourceID); err != nil {
		return err
	}
	if err := client.validateIssueKey(targetID); err != nil {
		return err
	}
	linkType = strings.TrimSpace(linkType)
	if linkType == "" {
		linkType = "Relates"
	}
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "outward"
	}
	body := map[string]any{
		"type": map[string]any{"name": linkType},
	}
	if direction == "inward" {
		body["inwardIssue"] = map[string]any{"key": targetID}
		body["outwardIssue"] = map[string]any{"key": sourceID}
	} else {
		body["inwardIssue"] = map[string]any{"key": sourceID}
		body["outwardIssue"] = map[string]any{"key": targetID}
	}
	if strings.TrimSpace(comment) != "" {
		body["comment"] = map[string]any{"body": jiraADFDocument(comment)}
	}
	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issueLink", body, nil)
}

func (client *jiraClient) SetSprint(ctx context.Context, cardID string, sprintID int) error {
	if sprintID <= 0 {
		return fmt.Errorf("sprint_id is required")
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPost, fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue", sprintID), map[string]any{
		"issues": []string{cardID},
	}, nil)
}

func (client *jiraClient) RankIssue(ctx context.Context, cardID string, beforeID string, afterID string) error {
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	body := map[string]any{"issues": []string{cardID}}
	if strings.TrimSpace(beforeID) != "" {
		if err := client.validateIssueKey(beforeID); err != nil {
			return err
		}
		body["rankBeforeIssue"] = strings.TrimSpace(beforeID)
	} else if strings.TrimSpace(afterID) != "" {
		if err := client.validateIssueKey(afterID); err != nil {
			return err
		}
		body["rankAfterIssue"] = strings.TrimSpace(afterID)
	} else {
		return nil
	}
	if client.config.RankCustomFieldID > 0 {
		body["rankCustomFieldId"] = client.config.RankCustomFieldID
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/agile/1.0/issue/rank", body, nil)
}

func (client *jiraClient) SetComponents(ctx context.Context, cardID string, components []string) error {
	return client.UpdateIssueFields(ctx, cardID, map[string]any{"components": jiraNameObjects(components)})
}

func (client *jiraClient) SetFixVersions(ctx context.Context, cardID string, versions []string) error {
	return client.UpdateIssueFields(ctx, cardID, map[string]any{"fixVersions": jiraNameObjects(versions)})
}

func (client *jiraClient) SetCustomField(ctx context.Context, cardID string, fieldID string, value any) error {
	fieldID = strings.TrimSpace(fieldID)
	if fieldID == "" {
		return fmt.Errorf("field_id is required")
	}
	return client.UpdateIssueFields(ctx, cardID, map[string]any{fieldID: value})
}

func (client *jiraClient) AddRemoteLink(ctx context.Context, cardID string, rawURL string, title string, summary string) error {
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	rawURL = strings.TrimSpace(rawURL)
	title = strings.TrimSpace(title)
	if rawURL == "" || title == "" {
		return fmt.Errorf("url and title are required")
	}
	body := map[string]any{
		"object": map[string]any{
			"url":   rawURL,
			"title": title,
		},
	}
	if strings.TrimSpace(summary) != "" {
		body["object"].(map[string]any)["summary"] = strings.TrimSpace(summary)
	}
	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/remotelink", body, nil)
}

func (client *jiraClient) SetReporter(ctx context.Context, cardID string, accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("reporter account_id is required")
	}
	return client.UpdateIssueFields(ctx, cardID, map[string]any{"reporter": map[string]any{"accountId": accountID}})
}

func (client *jiraClient) AddWatcher(ctx context.Context, cardID string, accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("watcher account_id is required")
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/watchers", accountID, nil)
}

func (client *jiraClient) GetMetadata(ctx context.Context) (map[string]any, error) {
	metadata := fallbackJiraMetadata()
	metadata["source"] = "jira"
	var errors []string

	var project struct {
		ID         string `json:"id"`
		Key        string `json:"key"`
		Name       string `json:"name"`
		IssueTypes []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Subtask bool   `json:"subtask"`
		} `json:"issueTypes"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/project/"+url.PathEscape(client.config.ProjectKey), nil, &project); err != nil {
		errors = append(errors, "project: "+err.Error())
	} else {
		metadata["project"] = map[string]any{"id": project.ID, "key": project.Key, "name": project.Name}
		var issueTypes []map[string]any
		for _, issueType := range project.IssueTypes {
			issueTypes = append(issueTypes, map[string]any{"id": issueType.ID, "name": issueType.Name, "subtask": issueType.Subtask})
		}
		if len(issueTypes) > 0 {
			metadata["issue_types"] = issueTypes
		}
	}

	var components []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/project/"+url.PathEscape(client.config.ProjectKey)+"/components", nil, &components); err != nil {
		errors = append(errors, "components: "+err.Error())
	} else {
		names := make([]string, 0, len(components))
		for _, component := range components {
			if strings.TrimSpace(component.Name) != "" {
				names = append(names, component.Name)
			}
		}
		metadata["components"] = names
	}

	var versions []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Archived bool   `json:"archived"`
		Released bool   `json:"released"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/project/"+url.PathEscape(client.config.ProjectKey)+"/versions", nil, &versions); err != nil {
		errors = append(errors, "versions: "+err.Error())
	} else {
		var versionValues []map[string]any
		for _, version := range versions {
			versionValues = append(versionValues, map[string]any{
				"id":       version.ID,
				"name":     version.Name,
				"archived": version.Archived,
				"released": version.Released,
			})
		}
		metadata["fix_versions"] = versionValues
	}

	var linkTypes struct {
		IssueLinkTypes []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Inward  string `json:"inward"`
			Outward string `json:"outward"`
		} `json:"issueLinkTypes"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/issueLinkType", nil, &linkTypes); err != nil {
		errors = append(errors, "link_types: "+err.Error())
	} else {
		metadata["link_types"] = linkTypes.IssueLinkTypes
	}

	var fields []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
		Schema struct {
			Type   string `json:"type"`
			Custom string `json:"custom"`
		} `json:"schema"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/field", nil, &fields); err != nil {
		errors = append(errors, "fields: "+err.Error())
	} else {
		metadata["fields"] = fields
	}

	if priorities, err := client.ListPriorities(ctx); err != nil {
		errors = append(errors, "priorities: "+err.Error())
	} else {
		metadata["priorities"] = priorities
	}

	if client.config.BoardID > 0 {
		var sprints struct {
			Values []kanbanSprint `json:"values"`
		}
		path := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=active,future", client.config.BoardID)
		if err := client.doJSON(ctx, http.MethodGet, path, nil, &sprints); err != nil {
			errors = append(errors, "sprints: "+err.Error())
		} else {
			metadata["sprints"] = sprints.Values
		}
	}

	metadata["configured_fields"] = map[string]any{
		"story_points_field":    client.config.StoryPointsField,
		"sprint_field":          client.config.SprintField,
		"epic_link_field":       client.config.EpicLinkField,
		"blocked_flag_field":    client.config.BlockedFlagField,
		"rank_custom_field_id":  client.config.RankCustomFieldID,
		"custom_field_mappings": client.config.CustomFieldMappings,
	}
	if len(errors) > 0 {
		metadata["metadata_errors"] = errors
	}
	return metadata, nil
}

func (client *jiraClient) GetTransitions(ctx context.Context, cardID string) ([]map[string]any, error) {
	if err := client.validateIssueKey(cardID); err != nil {
		return nil, err
	}
	var response struct {
		Transitions []map[string]any `json:"transitions"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/transitions?expand=transitions.fields", nil, &response); err != nil {
		return nil, err
	}
	return response.Transitions, nil
}

func jiraObjectKey(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return asString(object["key"])
}

func jiraObjectNames(value any) []string {
	rawValues, ok := value.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		if name := jiraObjectName(raw); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func jiraNameObjects(values []string) []map[string]any {
	objects := make([]map[string]any, 0, len(values))
	for _, value := range uniqueStrings(values) {
		if value != "" {
			objects = append(objects, map[string]any{"name": value})
		}
	}
	return objects
}

func jiraStoryPoints(value any) *float64 {
	points, ok := asFloat64(value)
	if !ok {
		return nil
	}
	return &points
}

func jiraEstimate(value any) *kanbanEstimate {
	original := jiraEstimateValue(value, "originalEstimate")
	remaining := jiraEstimateValue(value, "remainingEstimate")
	if original == "" && remaining == "" {
		return nil
	}
	return &kanbanEstimate{Original: original, Remaining: remaining}
}

func jiraEstimateValue(value any, key string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return asString(object[key])
}

func jiraSprint(value any) *kanbanSprint {
	switch typed := value.(type) {
	case []any:
		for index := len(typed) - 1; index >= 0; index-- {
			if sprint := jiraSprint(typed[index]); sprint != nil {
				return sprint
			}
		}
	case map[string]any:
		id, _ := asInt(typed["id"])
		sprint := kanbanSprint{
			ID:        id,
			Name:      asString(typed["name"]),
			State:     asString(typed["state"]),
			Goal:      asString(typed["goal"]),
			StartDate: asString(typed["startDate"]),
			EndDate:   asString(typed["endDate"]),
		}
		if sprint.ID != 0 || sprint.Name != "" {
			return &sprint
		}
	case string:
		return jiraSprintFromLegacyString(typed)
	}
	return nil
}

func jiraSprintFromLegacyString(value string) *kanbanSprint {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	sprint := kanbanSprint{}
	for _, part := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.TrimRight(val, "]"))
		switch key {
		case "id":
			sprint.ID, _ = asInt(val)
		case "name":
			sprint.Name = val
		case "state":
			sprint.State = val
		case "goal":
			sprint.Goal = val
		case "startDate":
			sprint.StartDate = val
		case "endDate":
			sprint.EndDate = val
		}
	}
	if sprint.ID == 0 && sprint.Name == "" {
		return nil
	}
	return &sprint
}

func jiraIssueLinks(value any) []kanbanIssueLink {
	rawValues, ok := value.([]any)
	if !ok {
		return nil
	}
	links := make([]kanbanIssueLink, 0, len(rawValues))
	for _, raw := range rawValues {
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		link := kanbanIssueLink{
			ID:   asString(object["id"]),
			Type: jiraObjectName(object["type"]),
		}
		if issue, ok := object["outwardIssue"].(map[string]any); ok {
			link.Direction = "outward"
			link.TargetCardID = asString(issue["key"])
			if fields, ok := issue["fields"].(map[string]any); ok {
				link.TargetSummary = asString(fields["summary"])
				link.TargetStatus = jiraObjectName(fields["status"])
			}
		}
		if issue, ok := object["inwardIssue"].(map[string]any); ok && link.TargetCardID == "" {
			link.Direction = "inward"
			link.TargetCardID = asString(issue["key"])
			if fields, ok := issue["fields"].(map[string]any); ok {
				link.TargetSummary = asString(fields["summary"])
				link.TargetStatus = jiraObjectName(fields["status"])
			}
		}
		if link.TargetCardID != "" {
			links = append(links, link)
		}
	}
	return links
}

func jiraWorklogs(value any) []kanbanWorklog {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	rawValues, ok := object["worklogs"].([]any)
	if !ok {
		return nil
	}
	worklogs := make([]kanbanWorklog, 0, len(rawValues))
	for _, raw := range rawValues {
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		author := ""
		if user := jiraUser(object["author"]); user != nil {
			author = user.DisplayName
		}
		worklogs = append(worklogs, kanbanWorklog{
			ID:               asString(object["id"]),
			Author:           author,
			TimeSpent:        asString(object["timeSpent"]),
			TimeSpentSeconds: int64FromAny(object["timeSpentSeconds"]),
			Started:          asString(object["started"]),
			Comment:          jiraADFPlainText(object["comment"]),
			CreatedAt:        asString(object["created"]),
		})
	}
	return worklogs
}

func jiraCustomFields(fields map[string]any, mappings map[string]string) map[string]kanbanField {
	if len(mappings) == 0 {
		return nil
	}
	customFields := map[string]kanbanField{}
	for displayName, fieldID := range mappings {
		fieldID = strings.TrimSpace(fieldID)
		if fieldID == "" {
			continue
		}
		value, ok := fields[fieldID]
		if !ok || value == nil {
			continue
		}
		customFields[fieldID] = kanbanField{Name: displayName, Value: value}
	}
	if len(customFields) == 0 {
		return nil
	}
	return customFields
}

func int64FromAny(value any) int64 {
	if parsed, ok := asInt(value); ok {
		return int64(parsed)
	}
	return 0
}

func jiraWorklogStarted(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05.000Z0700"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02T15:04:05.000-0700")
		}
	}
	return value
}

func jiraAccountIDFromResult(args map[string]any, result map[string]any, resultKey string) string {
	if accountID := asString(args["account_id"]); accountID != "" {
		return accountID
	}
	switch user := result[resultKey].(type) {
	case kanbanUser:
		return user.AccountID
	case *kanbanUser:
		if user != nil {
			return user.AccountID
		}
	case map[string]any:
		return asString(user["accountId"])
	}
	return ""
}
