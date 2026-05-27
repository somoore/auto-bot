package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

const jiraDefaultIssueType = "Task"

type jiraConfig struct {
	BaseURL                  string                    `json:"base_url"`
	Email                    string                    `json:"email"`
	APIToken                 string                    `json:"api_token,omitempty"`
	APITokenFile             string                    `json:"api_token_file,omitempty"`
	APITokenCommand          []string                  `json:"api_token_command,omitempty"`
	APITokenEnv              string                    `json:"api_token_env,omitempty"`
	ProjectKey               string                    `json:"project_key"`
	IssueType                string                    `json:"issue_type"`
	JQL                      string                    `json:"jql"`
	StatusMappings           map[string]string         `json:"status_mappings"`
	Transitions              map[string]string         `json:"transitions"`
	RequiredTransitionFields map[string]map[string]any `json:"required_transition_fields,omitempty"`
	DeleteTransition         string                    `json:"delete_transition,omitempty"`
	BlockedFlagField         string                    `json:"blocked_flag_field,omitempty"`
	BlockedFlagValue         string                    `json:"blocked_flag_value,omitempty"`
	BoardID                  int                       `json:"board_id,omitempty"`
	StoryPointsField         string                    `json:"story_points_field,omitempty"`
	SprintField              string                    `json:"sprint_field,omitempty"`
	EpicLinkField            string                    `json:"epic_link_field,omitempty"`
	RankCustomFieldID        int                       `json:"rank_custom_field_id,omitempty"`
	CustomFieldMappings      map[string]string         `json:"custom_field_mappings,omitempty"`
	PollIntervalSeconds      int                       `json:"poll_interval_seconds,omitempty"`
	WebhookSecret            string                    `json:"webhook_secret,omitempty"`
}

type jiraClient struct {
	config     *jiraConfig
	baseURL    string
	email      string
	apiToken   string
	httpClient *http.Client
}

type jiraSyncer struct {
	board  *kanbanBoard
	client *jiraClient
	config *jiraConfig
}

func setupJiraSync(ctx context.Context, board *kanbanBoard) (*jiraSyncer, error) {
	configPath := strings.TrimSpace(os.Getenv("JIRA_CONFIG_PATH"))
	if configPath == "" && strings.TrimSpace(os.Getenv("JIRA_CONFIG_JSON")) == "" {
		return nil, nil
	}

	config, err := loadJiraConfig(ctx, configPath)
	if err != nil {
		return nil, err
	}

	client := newJiraClient(config)
	syncer := &jiraSyncer{
		board:  board,
		client: client,
		config: config,
	}

	cards, err := client.SearchKanbanCards(ctx)
	if err != nil {
		return nil, fmt.Errorf("initial Jira board sync: %w", err)
	}
	board.ReplaceCards(cards)
	auditBoardRefresh("jira-startup", board.SnapshotState())
	log.Infof("Jira sync enabled: loaded %d issues from %s", len(cards), config.BaseURL)

	syncer.startPolling(ctx)
	return syncer, nil
}

func loadJiraConfig(ctx context.Context, path string) (*jiraConfig, error) {
	var raw []byte
	if inlineConfig := strings.TrimSpace(os.Getenv("JIRA_CONFIG_JSON")); inlineConfig != "" {
		raw = []byte(inlineConfig)
	} else {
		var err error
		raw, err = os.ReadFile(path) // #nosec G304 G703 -- Jira config path is operator-controlled deployment configuration.
		if err != nil {
			return nil, fmt.Errorf("read Jira config: %w", err)
		}
	}

	var config jiraConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse Jira config JSON: %w", err)
	}
	if config.IssueType == "" {
		config.IssueType = jiraDefaultIssueType
	}
	if config.StatusMappings == nil {
		config.StatusMappings = map[string]string{}
	}
	if config.Transitions == nil {
		config.Transitions = map[string]string{}
	}
	if config.CustomFieldMappings == nil {
		config.CustomFieldMappings = map[string]string{}
	}
	if config.BlockedFlagField != "" && strings.TrimSpace(config.BlockedFlagValue) == "" {
		config.BlockedFlagValue = "Impediment"
	}
	if config.WebhookSecret == "" {
		config.WebhookSecret = strings.TrimSpace(os.Getenv("JIRA_WEBHOOK_SECRET"))
	}

	token, err := config.resolveAPIToken(ctx)
	if err != nil {
		return nil, err
	}
	config.APIToken = token

	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (config *jiraConfig) resolveAPIToken(ctx context.Context) (string, error) {
	if config.APITokenFile != "" {
		raw, err := os.ReadFile(config.APITokenFile)
		if err != nil {
			return "", fmt.Errorf("read Jira API token file: %w", err)
		}
		if token := strings.TrimSpace(string(raw)); token != "" {
			return token, nil
		}
	}

	if len(config.APITokenCommand) > 0 {
		commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(commandCtx, config.APITokenCommand[0], config.APITokenCommand[1:]...) // #nosec G204 -- explicit operator-configured token command, no shell expansion.
		raw, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("run Jira API token command: %w", err)
		}
		if token := strings.TrimSpace(string(raw)); token != "" {
			return token, nil
		}
	}

	if config.APITokenEnv != "" {
		if token := strings.TrimSpace(os.Getenv(config.APITokenEnv)); token != "" {
			return token, nil
		}
	}

	if token := strings.TrimSpace(config.APIToken); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("jira API token is required; set api_token_file, api_token_command, api_token_env, or api_token in the Jira config")
}

func (config *jiraConfig) validate() error {
	if strings.TrimSpace(config.BaseURL) == "" {
		return fmt.Errorf("jira base_url is required")
	}
	parsedURL, err := url.Parse(config.BaseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("jira base_url must be an absolute URL")
	}
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return fmt.Errorf("jira base_url must use http or https")
	}
	if strings.TrimSpace(config.Email) == "" {
		return fmt.Errorf("jira email is required")
	}
	if strings.TrimSpace(config.ProjectKey) == "" {
		return fmt.Errorf("jira project_key is required")
	}
	if strings.TrimSpace(config.IssueType) == "" {
		return fmt.Errorf("jira issue_type is required")
	}

	for jiraStatus, kanbanStatusValue := range config.StatusMappings {
		if strings.TrimSpace(jiraStatus) == "" {
			return fmt.Errorf("jira status mapping contains an empty jira status")
		}
		if _, err := parseKanbanStatus(kanbanStatusValue); err != nil {
			return fmt.Errorf("invalid Kanban status mapping for Jira status %q: %w", jiraStatus, err)
		}
	}
	for kanbanStatusValue := range config.Transitions {
		if kanbanStatusValue == "Deleted" {
			continue
		}
		if _, err := parseKanbanStatus(kanbanStatusValue); err != nil {
			return fmt.Errorf("invalid Jira transition status %q: %w", kanbanStatusValue, err)
		}
	}

	return nil
}

func newJiraClient(config *jiraConfig) *jiraClient {
	return &jiraClient{
		config:     config,
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		email:      strings.TrimSpace(config.Email),
		apiToken:   strings.TrimSpace(config.APIToken),
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (client *jiraClient) SearchKanbanCards(ctx context.Context) ([]kanbanCard, error) {
	const maxResults = 100
	jql := strings.TrimSpace(client.config.JQL)
	if jql == "" {
		jql = fmt.Sprintf("project = %s ORDER BY updated DESC", client.config.ProjectKey)
	}

	var cards []kanbanCard
	nextPageToken := ""
	for {
		fields := []string{"summary", "description", "labels", "status", "assignee", "reporter", "duedate", "priority", "comment", "issuetype", "parent", "components", "fixVersions", "issuelinks", "worklog", "timetracking", "updated"}
		if client.config.BlockedFlagField != "" {
			fields = append(fields, client.config.BlockedFlagField)
		}
		if client.config.StoryPointsField != "" {
			fields = append(fields, client.config.StoryPointsField)
		}
		if client.config.SprintField != "" {
			fields = append(fields, client.config.SprintField)
		}
		if client.config.EpicLinkField != "" {
			fields = append(fields, client.config.EpicLinkField)
		}
		for _, fieldID := range client.config.CustomFieldMappings {
			if strings.TrimSpace(fieldID) != "" {
				fields = append(fields, strings.TrimSpace(fieldID))
			}
		}
		requestBody := map[string]any{
			"jql":        jql,
			"maxResults": maxResults,
			"fields":     fields,
		}
		if nextPageToken != "" {
			requestBody["nextPageToken"] = nextPageToken
		}

		var response struct {
			NextPageToken string `json:"nextPageToken"`
			IsLast        bool   `json:"isLast"`
			Issues        []struct {
				Key    string         `json:"key"`
				Fields map[string]any `json:"fields"`
			} `json:"issues"`
		}
		if err := client.doJSON(ctx, http.MethodPost, "/rest/api/3/search/jql", requestBody, &response); err != nil {
			return nil, err
		}

		for _, issue := range response.Issues {
			if err := client.validateIssueKey(issue.Key); err != nil {
				return nil, fmt.Errorf("jira search returned issue outside configured project: %w", err)
			}
			status := client.config.mapJiraStatus(jiraObjectName(issue.Fields["status"]))
			blocked := client.config.hasBlockedFlag(issue.Fields[client.config.BlockedFlagField])
			if blocked {
				status = kanbanStatusBlocked
			}
			blockedReason := ""
			if blocked {
				blockedReason = latestBlockedReason(jiraComments(issue.Fields["comment"]))
			}
			cards = append(cards, kanbanCard{
				ID:                issue.Key,
				Status:            status,
				Title:             asString(issue.Fields["summary"]),
				Notes:             jiraADFPlainText(issue.Fields["description"]),
				Tags:              asStringSlice(issue.Fields["labels"]),
				IssueType:         jiraObjectName(issue.Fields["issuetype"]),
				ParentID:          jiraObjectKey(issue.Fields["parent"]),
				EpicKey:           asString(issue.Fields[client.config.EpicLinkField]),
				Assignee:          jiraAssigneeActor(issue.Fields["assignee"]),
				Reporter:          jiraUser(issue.Fields["reporter"]),
				DueDate:           asString(issue.Fields["duedate"]),
				Priority:          jiraObjectName(issue.Fields["priority"]),
				StoryPoints:       jiraStoryPoints(issue.Fields[client.config.StoryPointsField]),
				Estimate:          jiraEstimate(issue.Fields["timetracking"]),
				OriginalEstimate:  jiraEstimateValue(issue.Fields["timetracking"], "originalEstimate"),
				RemainingEstimate: jiraEstimateValue(issue.Fields["timetracking"], "remainingEstimate"),
				Sprint:            jiraSprint(issue.Fields[client.config.SprintField]),
				Components:        jiraObjectNames(issue.Fields["components"]),
				FixVersions:       jiraObjectNames(issue.Fields["fixVersions"]),
				BlockedReason:     blockedReason,
				Comments:          jiraComments(issue.Fields["comment"]),
				IssueLinks:        jiraIssueLinks(issue.Fields["issuelinks"]),
				Worklogs:          jiraWorklogs(issue.Fields["worklog"]),
				CustomFields:      jiraCustomFields(issue.Fields, client.config.CustomFieldMappings),
			})
		}

		if response.IsLast || response.NextPageToken == "" {
			break
		}
		nextPageToken = response.NextPageToken
	}

	return cards, nil
}

func (client *jiraClient) CreateIssue(ctx context.Context, card kanbanCard) (string, error) {
	issueType := strings.TrimSpace(card.IssueType)
	if issueType == "" {
		issueType = client.config.IssueType
	}
	parentID := strings.TrimSpace(card.ParentID)
	fields := map[string]any{
		"project": map[string]any{
			"key": client.config.ProjectKey,
		},
		"issuetype":   client.issueTypeFieldForCreate(ctx, issueType, parentID),
		"summary":     card.Title,
		"description": jiraADFDocument(card.Notes),
	}
	if parentID != "" {
		fields["parent"] = map[string]any{"key": parentID}
	}
	if strings.TrimSpace(card.EpicKey) != "" && strings.TrimSpace(client.config.EpicLinkField) != "" {
		fields[client.config.EpicLinkField] = strings.TrimSpace(card.EpicKey)
	}
	if labels := jiraLabels(card.Tags); len(labels) > 0 {
		fields["labels"] = labels
	}
	if card.Assignee != nil && card.Assignee.Kind == kanbanActorKindHuman && strings.TrimSpace(card.Assignee.ID) != "" {
		fields["assignee"] = map[string]any{"accountId": card.Assignee.ID}
	}
	if strings.TrimSpace(card.DueDate) != "" {
		fields["duedate"] = card.DueDate
	}
	if strings.TrimSpace(card.Priority) != "" {
		fields["priority"] = map[string]any{"name": card.Priority}
	}
	if card.StoryPoints != nil && strings.TrimSpace(client.config.StoryPointsField) != "" {
		fields[client.config.StoryPointsField] = *card.StoryPoints
	}
	if len(card.Components) > 0 {
		fields["components"] = jiraNameObjects(card.Components)
	}
	if len(card.FixVersions) > 0 {
		fields["fixVersions"] = jiraNameObjects(card.FixVersions)
	}

	var response struct {
		Key string `json:"key"`
	}
	if err := client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue", map[string]any{"fields": fields}, &response); err != nil {
		return "", err
	}
	if response.Key == "" {
		return "", fmt.Errorf("jira create issue response did not include an issue key")
	}
	if err := client.validateIssueKey(response.Key); err != nil {
		return "", fmt.Errorf("jira create issue returned key outside configured project: %w", err)
	}
	return response.Key, nil
}

func (client *jiraClient) issueTypeFieldForCreate(ctx context.Context, issueType string, parentID string) map[string]any {
	issueType = strings.TrimSpace(issueType)
	field := map[string]any{"name": issueType}
	if parentID == "" || !isJiraSubtaskIssueTypeName(issueType) {
		return field
	}
	resolved, err := client.resolveProjectSubtaskIssueType(ctx, issueType)
	if err != nil {
		log.Warnf("Jira subtask issue type metadata lookup failed; falling back to name %q: %v", issueType, err)
		return field
	}
	return resolved
}

func (client *jiraClient) resolveProjectSubtaskIssueType(ctx context.Context, requested string) (map[string]any, error) {
	var project struct {
		IssueTypes []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Subtask bool   `json:"subtask"`
		} `json:"issueTypes"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/project/"+url.PathEscape(client.config.ProjectKey), nil, &project); err != nil {
		return nil, err
	}

	requestedNormalized := normalizeJiraIssueTypeName(requested)
	var firstSubtask map[string]any
	for _, issueType := range project.IssueTypes {
		if !issueType.Subtask {
			continue
		}
		field := jiraIssueTypeField(issueType.ID, issueType.Name)
		if firstSubtask == nil {
			firstSubtask = field
		}
		nameNormalized := normalizeJiraIssueTypeName(issueType.Name)
		if nameNormalized == requestedNormalized || isJiraSubtaskIssueTypeName(issueType.Name) {
			return field, nil
		}
	}
	if firstSubtask != nil {
		return firstSubtask, nil
	}
	return nil, fmt.Errorf("project %s does not expose a subtask issue type", client.config.ProjectKey)
}

func jiraIssueTypeField(id string, name string) map[string]any {
	if strings.TrimSpace(id) != "" {
		return map[string]any{"id": strings.TrimSpace(id)}
	}
	return map[string]any{"name": strings.TrimSpace(name)}
}

func isJiraSubtaskIssueTypeName(value string) bool {
	normalized := normalizeJiraIssueTypeName(value)
	return normalized == "sub task" || normalized == "subtask"
}

func normalizeJiraIssueTypeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func (client *jiraClient) UpdateIssue(ctx context.Context, cardID string, title string, notes string) error {
	fields := map[string]any{}
	if strings.TrimSpace(title) != "" {
		fields["summary"] = title
	}
	if strings.TrimSpace(notes) != "" {
		fields["description"] = jiraADFDocument(notes)
	}
	if len(fields) == 0 {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{"fields": fields}, nil)
}

func (client *jiraClient) AddLabels(ctx context.Context, cardID string, tags []string) error {
	labels := jiraLabels(tags)
	if len(labels) == 0 {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}

	updates := make([]map[string]any, 0, len(labels))
	for _, label := range labels {
		updates = append(updates, map[string]any{"add": label})
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{
		"update": map[string]any{
			"labels": updates,
		},
	}, nil)
}

func (client *jiraClient) RemoveLabels(ctx context.Context, cardID string, tags []string) error {
	labels := jiraLabels(tags)
	if len(labels) == 0 {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}

	updates := make([]map[string]any, 0, len(labels))
	for _, label := range labels {
		updates = append(updates, map[string]any{"remove": label})
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{
		"update": map[string]any{
			"labels": updates,
		},
	}, nil)
}

func (client *jiraClient) SearchAssignableUsers(ctx context.Context, query string) ([]kanbanUser, error) {
	values := url.Values{}
	values.Set("project", client.config.ProjectKey)
	values.Set("maxResults", "20")
	if strings.TrimSpace(query) != "" {
		values.Set("query", strings.TrimSpace(query))
	}

	var response []struct {
		AccountID    string `json:"accountId"`
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
		Active       bool   `json:"active"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/user/assignable/search?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}

	users := make([]kanbanUser, 0, len(response))
	for _, user := range response {
		users = append(users, kanbanUser{
			AccountID:    user.AccountID,
			DisplayName:  user.DisplayName,
			EmailAddress: user.EmailAddress,
			Active:       user.Active,
		})
	}
	return users, nil
}

func (client *jiraClient) ListPriorities(ctx context.Context) ([]string, error) {
	var response []struct {
		Name string `json:"name"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/rest/api/3/priority", nil, &response); err != nil {
		return nil, err
	}
	priorities := make([]string, 0, len(response))
	for _, priority := range response {
		if strings.TrimSpace(priority.Name) != "" {
			priorities = append(priorities, priority.Name)
		}
	}
	return priorities, nil
}

func (client *jiraClient) AssignIssue(ctx context.Context, cardID string, accountID string) error {
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	var assignee any
	if strings.TrimSpace(accountID) != "" {
		assignee = strings.TrimSpace(accountID)
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/assignee", map[string]any{
		"accountId": assignee,
	}, nil)
}

func (client *jiraClient) AddComment(ctx context.Context, cardID string, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/comment", map[string]any{
		"body": jiraADFDocument(comment),
	}, nil)
}

func (client *jiraClient) SetDueDate(ctx context.Context, cardID string, dueDate string) error {
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	var value any
	if strings.TrimSpace(dueDate) != "" {
		value = strings.TrimSpace(dueDate)
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{
		"fields": map[string]any{
			"duedate": value,
		},
	}, nil)
}

func (client *jiraClient) SetPriority(ctx context.Context, cardID string, priority string) error {
	priority = strings.TrimSpace(priority)
	if priority == "" {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{
		"fields": map[string]any{
			"priority": map[string]any{"name": priority},
		},
	}, nil)
}

func (client *jiraClient) SetBlockedFlag(ctx context.Context, cardID string, blocked bool) error {
	field := strings.TrimSpace(client.config.BlockedFlagField)
	if field == "" {
		return nil
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}

	value := any([]map[string]any{})
	if blocked {
		value = []map[string]any{{"value": client.config.BlockedFlagValue}}
	}
	return client.doJSON(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(cardID), map[string]any{
		"fields": map[string]any{
			field: value,
		},
	}, nil)
}

func (client *jiraClient) HasTransition(status kanbanStatus) bool {
	return strings.TrimSpace(client.config.Transitions[string(status)]) != ""
}

func (client *jiraClient) TransitionIssue(ctx context.Context, cardID string, status kanbanStatus) error {
	transitionID := strings.TrimSpace(client.config.Transitions[string(status)])
	if transitionID == "" {
		return fmt.Errorf("no Jira transition configured for Kanban status %q", status)
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}

	body := map[string]any{
		"transition": map[string]any{
			"id": transitionID,
		},
	}
	if fields := client.config.RequiredTransitionFields[string(status)]; len(fields) > 0 {
		body["fields"] = fields
	}

	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/transitions", body, nil)
}

func (client *jiraClient) CloseIssue(ctx context.Context, cardID string) error {
	transitionID := strings.TrimSpace(client.config.DeleteTransition)
	if transitionID == "" {
		transitionID = strings.TrimSpace(client.config.Transitions["Deleted"])
	}
	if transitionID == "" {
		return fmt.Errorf("delete_ticket requires delete_transition or transitions.Deleted in Jira config")
	}
	if err := client.validateIssueKey(cardID); err != nil {
		return err
	}

	return client.doJSON(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(cardID)+"/transitions", map[string]any{
		"transition": map[string]any{
			"id": transitionID,
		},
	}, nil)
}

func (client *jiraClient) doJSON(ctx context.Context, method string, path string, body any, out any) (err error) {
	var requestBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Jira request: %w", err)
		}
		requestBody = bytes.NewReader(raw)
	}

	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("create Jira request: %w", err)
	}
	request.SetBasicAuth(client.email, client.apiToken)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call Jira: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close Jira response body: %w", closeErr)
		}
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("jira %s %s failed: status=%s body=%s", method, path, response.Status, strings.TrimSpace(string(raw)))
	}

	if out == nil || response.StatusCode == http.StatusNoContent {
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			return fmt.Errorf("drain Jira response body: %w", err)
		}
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Jira response: %w", err)
	}
	return nil
}

func (client *jiraClient) validateIssueKey(issueKey string) error {
	issueKey = strings.TrimSpace(issueKey)
	projectKey := strings.TrimSpace(client.config.ProjectKey)
	if issueKey == "" {
		return fmt.Errorf("jira issue key is required")
	}
	if projectKey == "" {
		return fmt.Errorf("jira project_key is required")
	}
	prefix, _, ok := strings.Cut(issueKey, "-")
	if !ok || prefix == "" {
		return fmt.Errorf("refusing Jira write for non-Jira issue key %q; configured project is %q", issueKey, projectKey)
	}
	if !strings.EqualFold(prefix, projectKey) {
		return fmt.Errorf("refusing Jira write for issue %q outside configured project %q", issueKey, projectKey)
	}
	return nil
}

func (config *jiraConfig) mapJiraStatus(status string) kanbanStatus {
	if mapped, ok := config.StatusMappings[status]; ok {
		parsed, err := parseKanbanStatus(mapped)
		if err == nil {
			return parsed
		}
	}
	return kanbanStatusBacklog
}

func (config *jiraConfig) hasBlockedFlag(value any) bool {
	if strings.TrimSpace(config.BlockedFlagField) == "" {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(config.BlockedFlagValue))
	if want == "" {
		want = "impediment"
	}

	var values []any
	switch typed := value.(type) {
	case []any:
		values = typed
	case map[string]any:
		values = []any{typed}
	case string:
		values = []any{typed}
	default:
		return false
	}

	for _, candidate := range values {
		switch typed := candidate.(type) {
		case map[string]any:
			for _, key := range []string{"value", "name", "id"} {
				if strings.ToLower(asString(typed[key])) == want {
					return true
				}
			}
		case string:
			if strings.ToLower(strings.TrimSpace(typed)) == want {
				return true
			}
		}
	}
	return false
}

func jiraObjectName(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return asString(object["name"])
}

func jiraUser(value any) *kanbanUser {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil
	}
	user := kanbanUser{
		AccountID:    asString(object["accountId"]),
		DisplayName:  asString(object["displayName"]),
		EmailAddress: asString(object["emailAddress"]),
	}
	if active, ok := object["active"].(bool); ok {
		user.Active = active
	}
	if user.AccountID == "" && user.DisplayName == "" && user.EmailAddress == "" {
		return nil
	}
	return &user
}

// jiraAssigneeActor hydrates a Jira assignee response into the canonical
// Actor shape. Jira only returns human identities, so every hydrated
// assignee is an Actor{Kind:Human}.
func jiraAssigneeActor(value any) *kanbanActor {
	user := jiraUser(value)
	if user == nil {
		return nil
	}
	return actorFromUser(*user)
}

func jiraComments(value any) []kanbanComment {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	rawComments, ok := object["comments"].([]any)
	if !ok {
		return nil
	}

	comments := make([]kanbanComment, 0, len(rawComments))
	for _, rawComment := range rawComments {
		commentObject, ok := rawComment.(map[string]any)
		if !ok {
			continue
		}
		author := ""
		if user := jiraUser(commentObject["author"]); user != nil {
			author = user.DisplayName
		}
		body := jiraADFPlainText(commentObject["body"])
		if body == "" {
			continue
		}
		comments = append(comments, kanbanComment{
			ID:        asString(commentObject["id"]),
			Body:      body,
			Author:    author,
			CreatedAt: asString(commentObject["created"]),
		})
	}
	return comments
}

func latestBlockedReason(comments []kanbanComment) string {
	for index := len(comments) - 1; index >= 0; index-- {
		body := strings.TrimSpace(comments[index].Body)
		if strings.HasPrefix(strings.ToLower(body), "blocked:") {
			return strings.TrimSpace(body[len("blocked:"):])
		}
	}
	return ""
}

func jiraAssigneeAccountID(args map[string]any, result map[string]any) string {
	if accountID := asString(args["account_id"]); accountID != "" {
		return accountID
	}
	switch assignee := result["assignee"].(type) {
	case kanbanActor:
		if assignee.Kind == kanbanActorKindHuman {
			return assignee.ID
		}
		return ""
	case *kanbanActor:
		if assignee != nil && assignee.Kind == kanbanActorKindHuman {
			return assignee.ID
		}
		return ""
	case kanbanUser:
		return assignee.AccountID
	case *kanbanUser:
		if assignee != nil {
			return assignee.AccountID
		}
	case map[string]any:
		// Support both Actor shape ({"kind","id"}) and legacy User shape
		// ({"accountId"}). Reject non-human Actors so we never push an
		// agent identity into Jira.
		if kind, ok := assignee["kind"].(string); ok && kind != "" {
			if kind == string(kanbanActorKindHuman) {
				return asString(assignee["id"])
			}
			return ""
		}
		return asString(assignee["accountId"])
	}
	return ""
}

func (syncer *jiraSyncer) ApplyToolCall(ctx context.Context, toolName string, rawArgs string, result map[string]any) error {
	args := map[string]any{}
	if trimmed := strings.TrimSpace(rawArgs); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return fmt.Errorf("parse Jira sync args for %s: %w", toolName, err)
		}
	}
	if syncer.board != nil {
		args = syncer.board.canonicalizeToolArgs(args)
	}

	switch toolName {
	case "create_ticket", "create_subtask":
		card, ok := result["card"].(kanbanCard)
		if !ok {
			return fmt.Errorf("%s result did not include a Kanban card", toolName)
		}
		issueKey, err := syncer.client.CreateIssue(ctx, card)
		if err != nil {
			return err
		}
		if syncer.board.renameCardID(card.ID, issueKey) {
			log.Infof("Jira sync: renamed local card %s to issue %s", card.ID, issueKey)
		}
		result["previous_card_id"] = card.ID
		result["card_id"] = issueKey
		result["jira_issue_key"] = issueKey
		card.ID = issueKey
		result["card"] = cloneKanbanCard(card)
		if card.Status != kanbanStatusBacklog {
			return syncer.moveIssue(ctx, issueKey, card.Status, "")
		}
	case "move_ticket":
		cardID := asString(args["card_id"])
		status, err := parseKanbanStatus(args["status"])
		if err != nil {
			return err
		}
		return syncer.moveIssue(ctx, cardID, status, "")
	case "add_tags":
		return syncer.client.AddLabels(ctx, asString(args["card_id"]), asStringSlice(args["tags"]))
	case "remove_tags":
		return syncer.client.RemoveLabels(ctx, asString(args["card_id"]), asStringSlice(args["tags"]))
	case "update_ticket":
		return syncer.client.UpdateIssue(ctx, asString(args["card_id"]), asString(args["title"]), asString(args["notes"]))
	case "append_notes":
		return syncer.client.UpdateIssue(ctx, asString(args["card_id"]), "", asString(result["notes"]))
	case "add_comment":
		return syncer.client.AddComment(ctx, asString(args["card_id"]), asString(args["comment"]))
	case "assign_ticket":
		return syncer.client.AssignIssue(ctx, asString(args["card_id"]), jiraAssigneeAccountID(args, result))
	case "unassign_ticket":
		return syncer.client.AssignIssue(ctx, asString(args["card_id"]), "")
	case "set_eta":
		return syncer.client.SetDueDate(ctx, asString(args["card_id"]), asString(result["due_date"]))
	case "set_priority":
		return syncer.client.SetPriority(ctx, asString(args["card_id"]), asString(args["priority"]))
	case "set_story_points":
		points, ok := asFloat64(args["points"])
		if !ok {
			points, _ = asFloat64(args["story_points"])
		}
		return syncer.client.SetStoryPoints(ctx, asString(args["card_id"]), points)
	case "set_estimate":
		return syncer.client.SetEstimate(ctx, asString(args["card_id"]), asString(args["original_estimate"]), asString(args["remaining_estimate"]))
	case "add_worklog":
		seconds, _ := asInt(args["time_spent_seconds"])
		return syncer.client.AddWorklog(ctx, asString(args["card_id"]), asString(args["time_spent"]), int64(seconds), firstNonEmptyString(args, "started", "started_at"), asString(args["comment"]))
	case "link_issues":
		return syncer.client.LinkIssues(ctx, firstNonEmptyString(args, "card_id", "source_card_id"), asString(args["target_card_id"]), asString(args["link_type"]), asString(args["direction"]), firstNonEmptyString(args, "comment", "relationship"))
	case "set_sprint":
		sprintID, _ := asInt(args["sprint_id"])
		return syncer.client.SetSprint(ctx, asString(args["card_id"]), sprintID)
	case "prioritize_ticket":
		cardID := asString(args["card_id"])
		if cardID == "" {
			cardID = asString(result["card_id"])
		}
		if statusRaw := asString(result["status"]); statusRaw != "" && statusRaw != asString(result["previous_status"]) {
			status, err := parseKanbanStatus(statusRaw)
			if err != nil {
				return err
			}
			if err := syncer.moveIssue(ctx, cardID, status, ""); err != nil {
				return err
			}
		}
		beforeID := firstNonEmptyString(args, "above_card_id", "before_card_id")
		afterID := firstNonEmptyString(args, "below_card_id", "after_card_id")
		if beforeID == "" && afterID == "" {
			beforeID = firstNonEmptyString(result, "above_card_id", "before_card_id")
			afterID = firstNonEmptyString(result, "below_card_id", "after_card_id")
		}
		if beforeID == "" && afterID == "" {
			return nil
		}
		return syncer.client.RankIssue(ctx, cardID, beforeID, afterID)
	case "rank_issue":
		return syncer.client.RankIssue(ctx, asString(args["card_id"]), asString(args["before_card_id"]), asString(args["after_card_id"]))
	case "set_components":
		return syncer.client.SetComponents(ctx, asString(args["card_id"]), asStringSlice(args["components"]))
	case "set_fix_versions":
		return syncer.client.SetFixVersions(ctx, asString(args["card_id"]), asStringSlice(args["fix_versions"]))
	case "set_custom_field":
		return syncer.client.SetCustomField(ctx, asString(args["card_id"]), asString(args["field_id"]), args["value"])
	case "add_remote_link":
		return syncer.client.AddRemoteLink(ctx, asString(args["card_id"]), asString(args["url"]), asString(args["title"]), asString(args["summary"]))
	case "set_reporter":
		return syncer.client.SetReporter(ctx, asString(args["card_id"]), jiraAccountIDFromResult(args, result, "reporter"))
	case "add_watcher":
		return syncer.client.AddWatcher(ctx, asString(args["card_id"]), jiraAccountIDFromResult(args, result, "watcher"))
	case "set_blocked":
		cardID := asString(args["card_id"])
		reason := asString(args["reason"])
		if err := syncer.moveIssue(ctx, cardID, kanbanStatusBlocked, ""); err != nil {
			return err
		}
		if err := syncer.client.AddLabels(ctx, cardID, jiraTagsExcept(asStringSlice(result["tags"]), "blocked")); err != nil {
			return err
		}
		if err := syncer.client.UpdateIssue(ctx, cardID, "", asString(result["notes"])); err != nil {
			return err
		}
		return syncer.client.AddComment(ctx, cardID, "Blocked: "+reason)
	case "record_participant_update":
		if update, ok := result["update"].(scrumParticipantUpdate); ok && strings.TrimSpace(update.CardID) != "" {
			if update.Status != "" {
				if err := syncer.moveIssue(ctx, update.CardID, update.Status, update.Blocker); err != nil {
					return err
				}
			}
			if update.ETA != "" {
				if err := syncer.client.SetDueDate(ctx, update.CardID, update.ETA); err != nil {
					return err
				}
			}
			return syncer.client.AddComment(ctx, update.CardID, fmt.Sprintf("Meeting update from %s: %s", update.Participant, update.Summary))
		}
	case "delete_ticket":
		return syncer.client.CloseIssue(ctx, asString(args["card_id"]))
	}

	return nil
}

func (syncer *jiraSyncer) moveIssue(ctx context.Context, cardID string, status kanbanStatus, reason string) error {
	if status == kanbanStatusBlocked {
		if syncer.client.HasTransition(status) {
			if err := syncer.client.TransitionIssue(ctx, cardID, status); err != nil {
				return err
			}
		} else if syncer.client.config.BlockedFlagField == "" {
			return fmt.Errorf("no Jira transition configured for Kanban status %q", status)
		}
		if err := syncer.client.SetBlockedFlag(ctx, cardID, true); err != nil {
			return err
		}
		if err := syncer.client.AddLabels(ctx, cardID, []string{"blocked"}); err != nil {
			return err
		}
		if strings.TrimSpace(reason) != "" {
			return syncer.client.AddComment(ctx, cardID, "Blocked: "+strings.TrimSpace(reason))
		}
		return nil
	}

	if err := syncer.client.TransitionIssue(ctx, cardID, status); err != nil {
		return err
	}
	if syncer.client.config.BlockedFlagField != "" {
		return syncer.client.SetBlockedFlag(ctx, cardID, false)
	}
	return nil
}

func (syncer *jiraSyncer) startPolling(ctx context.Context) {
	if syncer.config.PollIntervalSeconds <= 0 {
		return
	}

	interval := time.Duration(syncer.config.PollIntervalSeconds) * time.Second
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := syncer.RefreshFromJira(ctx, "jira-poll"); err != nil {
					log.Errorf("Jira poll failed: %v", err)
				}
			}
		}
	}()
}

func (syncer *jiraSyncer) RefreshFromJira(ctx context.Context, source string) error {
	cards, err := syncer.client.SearchKanbanCards(ctx)
	if err != nil {
		return err
	}
	conflicts := syncer.board.ApplyJiraCards(cards, source)
	state := syncer.board.SnapshotState()
	syncer.board.persistSnapshot(source)
	auditBoardRefresh(source, state)
	broadcastKanbanEvent("board", state)
	for _, conflict := range conflicts {
		broadcastKanbanEvent("conflict", conflict)
	}
	return nil
}

func syncJiraToolCall(toolName string, rawArgs string, result map[string]any) (bool, error) {
	jiraRequired := jiraToolRequiresSync(toolName, rawArgs, result)
	if jiraSync == nil {
		return jiraRequired, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if toolName == "confirm_action" {
		if originalTool := asString(result["original_tool_name"]); originalTool != "" {
			toolName = originalTool
			rawArgs = asString(result["original_arguments_json"])
		}
	}
	if toolName == "undo_last_mutation" {
		if record, ok := result["undo_record"].(boardMutationRecord); ok {
			if err := jiraSync.ApplyUndo(ctx, record); err != nil {
				log.Errorf("Jira undo sync failed: %v", err)
				return true, err
			}
			return true, nil
		}
		return jiraRequired, nil
	}
	if err := jiraSync.ApplyToolCall(ctx, toolName, rawArgs, result); err != nil {
		log.Errorf("Jira sync failed for %s: %v", toolName, err)
		if conflict := jiraSync.board.RecordJiraSyncFailure(toolName, rawArgs, result, err); conflict.ConflictID != "" {
			broadcastKanbanEvent("conflict", conflict)
			broadcastKanbanEvent("board", jiraSync.board.SnapshotState())
		}
		return jiraRequired, err
	}
	return jiraRequired, nil
}

func annotateJiraSyncResult(result map[string]any, jiraRequired bool, syncErr error) {
	if !jiraRequired || result == nil {
		return
	}

	confirmedAt := time.Now().UTC().Format(time.RFC3339Nano)
	syncStatus := map[string]any{
		"required":   true,
		"configured": jiraSync != nil,
		"ok":         jiraSync != nil && syncErr == nil,
	}
	confirmation := externalActionConfirmation{
		System:      "jira",
		Operation:   "write-through",
		Required:    true,
		Configured:  jiraSync != nil,
		OK:          jiraSync != nil && syncErr == nil,
		ConfirmedAt: confirmedAt,
	}
	switch {
	case jiraSync == nil:
		syncStatus["message"] = "Jira sync is not configured; only the local board changed."
		confirmation.Message = "Jira sync is not configured; only the local board changed."
		confirmation.Evidence = "No Jira client is configured for this process."
		result["assistant_instruction"] = "Do not say Jira was updated. Say the local meeting board changed, but Jira sync is not configured."
	case syncErr != nil:
		syncStatus["error"] = syncErr.Error()
		syncStatus["message"] = "The local board changed, but Jira write-through failed."
		confirmation.Error = syncErr.Error()
		confirmation.Message = "The local board changed, but Jira write-through failed."
		confirmation.Evidence = truncateString(syncErr.Error(), 500)
		result["assistant_instruction"] = "Do not say Jira was updated. Say the local board changed, but Jira write-through failed, and give the short error reason."
	default:
		syncStatus["message"] = "Jira write-through confirmed."
		confirmation.Message = "Jira write-through confirmed."
		confirmation.Evidence = "Jira API returned success for the requested write."
		result["assistant_instruction"] = "You may say Jira accepted this update."
	}
	result["jira_sync"] = syncStatus
	result["external_confirmations"] = []externalActionConfirmation{confirmation}
	result["external_action_confirmed"] = confirmation.OK
	result["external_action_status"] = externalActionStatus(confirmation)
	result["api_confirmation_summary"] = confirmation.Message
}

func externalActionStatus(confirmation externalActionConfirmation) string {
	if !confirmation.Required {
		return "local_only"
	}
	if confirmation.OK {
		return "api_confirmed"
	}
	if !confirmation.Configured {
		return "api_not_configured"
	}
	return "api_failed"
}

func jiraToolRequiresSync(toolName string, rawArgs string, result map[string]any) bool {
	if toolName == "confirm_action" {
		if originalTool := asString(result["original_tool_name"]); originalTool != "" {
			return jiraToolRequiresSync(originalTool, asString(result["original_arguments_json"]), result)
		}
	}
	if toolName == "undo_last_mutation" {
		_, ok := result["undo_record"].(boardMutationRecord)
		return ok
	}
	switch toolName {
	case "create_ticket",
		"create_subtask",
		"move_ticket",
		"add_tags",
		"remove_tags",
		"update_ticket",
		"append_notes",
		"add_comment",
		"assign_ticket",
		"unassign_ticket",
		"set_eta",
		"set_priority",
		"set_story_points",
		"set_estimate",
		"add_worklog",
		"link_issues",
		"set_sprint",
		"prioritize_ticket",
		"rank_issue",
		"set_components",
		"set_fix_versions",
		"set_custom_field",
		"add_remote_link",
		"set_reporter",
		"add_watcher",
		"set_blocked",
		"delete_ticket":
		return true
	case "record_participant_update":
		if update, ok := result["update"].(scrumParticipantUpdate); ok {
			return strings.TrimSpace(update.CardID) != ""
		}
		args := map[string]any{}
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return false
		}
		return strings.TrimSpace(firstNonEmptyString(args, "card_id", "cardId")) != ""
	default:
		return false
	}
}

func jiraADFDocument(text string) map[string]any {
	paragraph := map[string]any{
		"type": "paragraph",
	}
	if strings.TrimSpace(text) != "" {
		paragraph["content"] = []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		}
	}

	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []map[string]any{
			paragraph,
		},
	}
}

func jiraADFPlainText(value any) string {
	var parts []string
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			if text, ok := typed["text"].(string); ok {
				parts = append(parts, text)
			}
			if children, ok := typed["content"].([]any); ok {
				for _, child := range children {
					walk(child)
				}
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			parts = append(parts, typed)
		}
	}
	walk(value)
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func jiraLabels(tags []string) []string {
	seen := map[string]struct{}{}
	labels := make([]string, 0, len(tags))
	for _, tag := range tags {
		label := jiraLabel(tag)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return labels
}

func jiraTagsExcept(tags []string, excludedTags ...string) []string {
	excluded := make(map[string]struct{}, len(excludedTags))
	for _, tag := range excludedTags {
		excluded[jiraLabel(tag)] = struct{}{}
	}
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if _, ok := excluded[jiraLabel(tag)]; ok {
			continue
		}
		filtered = append(filtered, tag)
	}
	return filtered
}

func jiraLabel(tag string) string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	var builder strings.Builder
	lastWasDash := false
	for _, r := range tag {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.':
			builder.WriteRune(r)
			lastWasDash = false
		case r == '-' || unicode.IsSpace(r):
			if !lastWasDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastWasDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
