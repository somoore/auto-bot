package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/somoore/auto-bot/internal/board"
)

// internalToolsDispatchHandler accepts MCP-shaped tool calls and routes them
// through the canonical ApplyToolCallWithMeta path so ActionLedger, risk
// classification, and confirmation gates apply uniformly. The MCP tool name
// space (card.create, card.update, card.comment) is translated to cmd/server's
// internal tool names (create_ticket, update_ticket/move_ticket/etc.,
// add_comment) before dispatch.
//
// Auth: same Bearer / session-cookie check as the rest of the server. A
// missing or wrong token returns 401.
//
// Request body:
//
//	{
//	  "tool":       "card.create" | "card.update" | "card.comment" |
//	                "board.list_cards" | "board.get_card",
//	  "args":       { ... },
//	  "dispatcher": "mcp",          // sets toolCallMeta.Source — required
//	                                //   for the risk gate to fire
//	  "tenant_id":  "default",      // optional, defaults to "default"
//	  "board_id":   "default"
//	}
//
// Response shape varies by tool — clients destructure { card, card_id,
// comment, cards } and ignore the rest.
func internalToolsDispatchHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var envelope struct {
		Tool       string          `json:"tool"`
		Args       json.RawMessage `json:"args"`
		Dispatcher string          `json:"dispatcher"`
		TenantID   string          `json:"tenant_id"`
		BoardID    string          `json:"board_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&envelope); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	tool := strings.TrimSpace(envelope.Tool)
	if tool == "" {
		writeDispatchError(w, http.StatusBadRequest, "tool is required")
		return
	}
	dispatcher := strings.TrimSpace(envelope.Dispatcher)
	if dispatcher == "" {
		dispatcher = "mcp"
	}
	meta := toolCallMeta{Dispatcher: dispatcher, Actor: dispatcher}

	switch tool {
	case "card.create":
		dispatchCardCreate(w, envelope.Args, meta)
	case "card.update":
		dispatchCardUpdate(w, envelope.Args, meta)
	case "card.comment":
		dispatchCardComment(w, envelope.Args, meta)
	case "runs.start":
		dispatchRunsStart(w, envelope.Args, meta)
	default:
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("unknown tool %q", tool))
	}
}

// dispatchRunsStart translates the MCP runs.start args into cmd/server's
// assign_ticket_to_agent tool. The Run is minted by the standard path so
// ActionLedger, the agent orchestrator hand-off, and persistence all apply.
// The response is the slim { run_id, status, agent_profile } shape the MCP
// caller expects.
func dispatchRunsStart(w http.ResponseWriter, args json.RawMessage, meta toolCallMeta) {
	var input struct {
		CardID            string `json:"card_id"`
		Objective         string `json:"objective"`
		AgentProfile      string `json:"agent_profile"`
		RequestType       string `json:"request_type"`
		RequestedBy       string `json:"requested_by"`
		Repo              string `json:"repo"`
		Branch            string `json:"branch"`
		PullRequestNumber int    `json:"pull_request_number"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode runs.start args: %v", err))
		return
	}
	if strings.TrimSpace(input.CardID) == "" {
		writeDispatchError(w, http.StatusBadRequest, "card_id is required")
		return
	}
	if strings.TrimSpace(input.Objective) == "" {
		writeDispatchError(w, http.StatusBadRequest, "objective is required")
		return
	}
	serverArgs := map[string]any{
		"card_id":   input.CardID,
		"objective": input.Objective,
	}
	if input.AgentProfile != "" {
		serverArgs["agent_profile"] = input.AgentProfile
	}
	if input.RequestType != "" {
		serverArgs["request_type"] = input.RequestType
	}
	if input.RequestedBy != "" {
		serverArgs["requested_by"] = input.RequestedBy
	}
	if input.Repo != "" {
		serverArgs["repo"] = input.Repo
	}
	if input.Branch != "" {
		serverArgs["branch"] = input.Branch
	}
	if input.PullRequestNumber > 0 {
		serverArgs["pull_request_number"] = input.PullRequestNumber
	}
	raw, err := json.Marshal(serverArgs)
	if err != nil {
		writeDispatchError(w, http.StatusInternalServerError, fmt.Sprintf("marshal assign_ticket_to_agent args: %v", err))
		return
	}
	result, _, err := sharedBoard.ApplyToolCallWithMeta("assign_ticket_to_agent", string(raw), meta)
	if err != nil {
		writeDispatchError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Medium-risk tools (assign_ticket_to_agent included) may queue a
	// pending confirmation instead of applying the call. In that case the
	// dispatcher surfaces the confirmation envelope so the MCP client can
	// either prompt the operator or wait for the Trust Ceremony queue.
	if requires, _ := result["requires_confirmation"].(bool); requires {
		writeDispatchJSON(w, http.StatusAccepted, map[string]any{
			"requires_confirmation": true,
			"confirmation_id":       result["confirmation_id"],
			"risk_level":            stringFromAny(result["risk_level"]),
			"tool_name":             result["tool_name"],
			"prompt":                result["prompt"],
			"card_id":               input.CardID,
		})
		return
	}
	runID, _ := result["run_id"].(string)
	if runID == "" {
		writeDispatchError(w, http.StatusInternalServerError, "assign_ticket_to_agent did not return a run_id")
		return
	}
	status := stringFromAny(result["status"])
	var profile string
	if view, ok := result["agent_run"].(agentRunView); ok {
		profile = view.AgentProfile
	}
	if profile == "" {
		profile = input.AgentProfile
	}
	writeDispatchJSON(w, http.StatusOK, map[string]any{
		"run_id":        runID,
		"status":        status,
		"agent_profile": profile,
		"card_id":       input.CardID,
	})
}

// stringFromAny coerces a value that may be a `string` or a defined string
// type (e.g. agent.RunStatus) into its underlying string. Returns "" for
// nil or any type that fmt cannot stringify usefully.
func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// Typed string aliases (e.g. agent.RunStatus) format as their string
	// value under %v.
	return fmt.Sprintf("%v", v)
}

// dispatchCardCreate translates the MCP card.create args to cmd/server's
// create_ticket arg shape (description→notes), routes the call through
// ApplyToolCallWithMeta, and surfaces the resulting Card.
func dispatchCardCreate(w http.ResponseWriter, args json.RawMessage, meta toolCallMeta) {
	var input struct {
		Title       string         `json:"title"`
		Description string         `json:"description"`
		Notes       string         `json:"notes"`
		Status      string         `json:"status"`
		Assignee    *board.Actor   `json:"assignee"`
		Tags        []string       `json:"tags"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode card.create args: %v", err))
		return
	}
	if strings.TrimSpace(input.Title) == "" {
		writeDispatchError(w, http.StatusBadRequest, "title is required")
		return
	}
	notes := input.Description
	if notes == "" {
		notes = input.Notes
	}
	serverArgs := map[string]any{
		"title": input.Title,
		"notes": notes,
	}
	if input.Status != "" {
		serverArgs["status"] = input.Status
	}
	if len(input.Tags) > 0 {
		serverArgs["tags"] = input.Tags
	}
	raw, err := json.Marshal(serverArgs)
	if err != nil {
		writeDispatchError(w, http.StatusInternalServerError, fmt.Sprintf("marshal create_ticket args: %v", err))
		return
	}
	result, _, err := sharedBoard.ApplyToolCallWithMeta("create_ticket", string(raw), meta)
	if err != nil {
		writeDispatchError(w, http.StatusBadRequest, err.Error())
		return
	}
	card, _ := result["card"].(kanbanCard)
	cardID, _ := result["card_id"].(string)
	if cardID == "" {
		cardID = card.ID
	}
	// If the caller asked to assign on creation, fan out to assign_ticket
	// using the just-created card_id. This keeps the MCP card.create
	// idempotent semantics intact even though cmd/server's create_ticket
	// doesn't carry an assignee field today.
	if input.Assignee != nil && cardID != "" {
		assignArgs := map[string]any{
			"card_id":      cardID,
			"account_id":   input.Assignee.ID,
			"display_name": input.Assignee.DisplayName,
		}
		assignRaw, _ := json.Marshal(assignArgs)
		if _, _, assignErr := sharedBoard.ApplyToolCallWithMeta("assign_ticket", string(assignRaw), meta); assignErr == nil {
			// Re-snapshot to pick up the assignee field.
			if updated, ok := lookupCardFromBoard(cardID); ok {
				card = updated
			}
		}
	}
	writeDispatchJSON(w, http.StatusOK, map[string]any{
		"card_id": cardID,
		"card":    card,
	})
}

// dispatchCardUpdate fans out an MCP card.update patch across the cmd/server
// tools that own each field today: update_ticket (title/notes), move_ticket
// (status), assign_ticket / unassign_ticket (assignee), add_tags / remove_tags
// (tags set). Each leg runs through ApplyToolCallWithMeta independently, so
// the per-leg risk gate decides whether the change applies or queues for
// confirmation. The response returns the resulting card snapshot.
func dispatchCardUpdate(w http.ResponseWriter, args json.RawMessage, meta toolCallMeta) {
	var input struct {
		CardID string `json:"card_id"`
		Patch  struct {
			Title    *string      `json:"title,omitempty"`
			Status   *string      `json:"status,omitempty"`
			Notes    *string      `json:"notes,omitempty"`
			Assignee *board.Actor `json:"assignee,omitempty"`
			Tags     *[]string    `json:"tags,omitempty"`
		} `json:"patch"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode card.update args: %v", err))
		return
	}
	cardID := strings.TrimSpace(input.CardID)
	if cardID == "" {
		writeDispatchError(w, http.StatusBadRequest, "card_id is required")
		return
	}
	if _, ok := lookupCardFromBoard(cardID); !ok {
		writeDispatchError(w, http.StatusNotFound, fmt.Sprintf("unknown card_id: %s", cardID))
		return
	}

	// Title and/or notes — share update_ticket.
	if input.Patch.Title != nil || input.Patch.Notes != nil {
		patchArgs := map[string]any{"card_id": cardID}
		if input.Patch.Title != nil {
			patchArgs["title"] = *input.Patch.Title
		}
		if input.Patch.Notes != nil {
			patchArgs["notes"] = *input.Patch.Notes
		}
		raw, _ := json.Marshal(patchArgs)
		if _, _, err := sharedBoard.ApplyToolCallWithMeta("update_ticket", string(raw), meta); err != nil {
			writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("update_ticket: %v", err))
			return
		}
	}
	// Status — move_ticket.
	if input.Patch.Status != nil {
		raw, _ := json.Marshal(map[string]any{"card_id": cardID, "status": *input.Patch.Status})
		if _, _, err := sharedBoard.ApplyToolCallWithMeta("move_ticket", string(raw), meta); err != nil {
			writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("move_ticket: %v", err))
			return
		}
	}
	// Assignee — assign_ticket (or unassign when ID is empty).
	if input.Patch.Assignee != nil {
		actor := input.Patch.Assignee
		if strings.TrimSpace(actor.ID) == "" && strings.TrimSpace(actor.DisplayName) == "" {
			raw, _ := json.Marshal(map[string]any{"card_id": cardID})
			if _, _, err := sharedBoard.ApplyToolCallWithMeta("unassign_ticket", string(raw), meta); err != nil {
				writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("unassign_ticket: %v", err))
				return
			}
		} else {
			assignArgs := map[string]any{
				"card_id":      cardID,
				"account_id":   actor.ID,
				"display_name": actor.DisplayName,
			}
			raw, _ := json.Marshal(assignArgs)
			if _, _, err := sharedBoard.ApplyToolCallWithMeta("assign_ticket", string(raw), meta); err != nil {
				writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("assign_ticket: %v", err))
				return
			}
		}
	}
	// Tags set — diff the current tag set and emit add_tags / remove_tags
	// against the delta. This keeps the patch semantics: passing tags=[]
	// clears the tag set; tags omitted leaves the set alone (TagsSet was
	// tracked on the MCP side and rendered as `tags` only when set).
	if input.Patch.Tags != nil {
		desired := append([]string{}, (*input.Patch.Tags)...)
		current, _ := lookupCardFromBoard(cardID)
		toAdd, toRemove := diffTagSets(current.Tags, desired)
		if len(toRemove) > 0 {
			raw, _ := json.Marshal(map[string]any{"card_id": cardID, "tags": toRemove})
			if _, _, err := sharedBoard.ApplyToolCallWithMeta("remove_tags", string(raw), meta); err != nil {
				writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("remove_tags: %v", err))
				return
			}
		}
		if len(toAdd) > 0 {
			raw, _ := json.Marshal(map[string]any{"card_id": cardID, "tags": toAdd})
			if _, _, err := sharedBoard.ApplyToolCallWithMeta("add_tags", string(raw), meta); err != nil {
				writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("add_tags: %v", err))
				return
			}
		}
	}

	updated, ok := lookupCardFromBoard(cardID)
	if !ok {
		writeDispatchError(w, http.StatusNotFound, fmt.Sprintf("card %s missing after update", cardID))
		return
	}
	writeDispatchJSON(w, http.StatusOK, map[string]any{"card": updated})
}

// dispatchCardComment translates the MCP card.comment args to cmd/server's
// add_comment shape (body→comment) and routes the call through
// ApplyToolCallWithMeta.
func dispatchCardComment(w http.ResponseWriter, args json.RawMessage, meta toolCallMeta) {
	var input struct {
		CardID  string `json:"card_id"`
		Body    string `json:"body"`
		AsActor string `json:"as_actor"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeDispatchError(w, http.StatusBadRequest, fmt.Sprintf("decode card.comment args: %v", err))
		return
	}
	if strings.TrimSpace(input.CardID) == "" {
		writeDispatchError(w, http.StatusBadRequest, "card_id is required")
		return
	}
	if strings.TrimSpace(input.Body) == "" {
		writeDispatchError(w, http.StatusBadRequest, "body is required")
		return
	}
	if strings.TrimSpace(input.AsActor) != "" {
		meta.Actor = input.AsActor
	}
	serverArgs := map[string]any{
		"card_id": input.CardID,
		"comment": input.Body,
	}
	raw, _ := json.Marshal(serverArgs)
	result, _, err := sharedBoard.ApplyToolCallWithMeta("add_comment", string(raw), meta)
	if err != nil {
		writeDispatchError(w, http.StatusBadRequest, err.Error())
		return
	}
	comment, _ := result["comment"].(kanbanComment)
	if strings.TrimSpace(comment.Author) == "" {
		comment.Author = meta.Actor
	}
	writeDispatchJSON(w, http.StatusOK, map[string]any{
		"card_id": input.CardID,
		"comment": comment,
	})
}

// internalBoardCardsHandler returns the live card snapshot from sharedBoard.
// GET /internal/board/cards            → { "cards": [...] }
// GET /internal/board/cards/{card_id}  → { "card":  ... }
func internalBoardCardsHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := authorizeBaseRequest(r); !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Strip the prefix and treat the remainder as the optional card_id.
	rest := strings.TrimPrefix(r.URL.Path, "/internal/board/cards")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		snap := sharedBoard.SnapshotState()
		writeDispatchJSON(w, http.StatusOK, map[string]any{"cards": snap.Cards})
		return
	}
	if card, ok := lookupCardFromBoard(rest); ok {
		writeDispatchJSON(w, http.StatusOK, map[string]any{"card": card})
		return
	}
	writeDispatchError(w, http.StatusNotFound, fmt.Sprintf("unknown card_id: %s", rest))
}

// lookupCardFromBoard returns a clone of the card with the given ID from
// the current board snapshot. Used by the dispatch handlers when they need
// to confirm a card exists or read its post-mutation state.
func lookupCardFromBoard(cardID string) (kanbanCard, bool) {
	snap := sharedBoard.SnapshotState()
	for _, c := range snap.Cards {
		if c.ID == cardID {
			return c, true
		}
	}
	return kanbanCard{}, false
}

// diffTagSets returns the tags to add and to remove to transform current
// into desired. Both result slices are sorted by their underlying order in
// the desired/current lists; duplicates within either list are dropped.
func diffTagSets(current, desired []string) (toAdd, toRemove []string) {
	desiredSet := map[string]struct{}{}
	for _, t := range desired {
		desiredSet[t] = struct{}{}
	}
	currentSet := map[string]struct{}{}
	for _, t := range current {
		currentSet[t] = struct{}{}
	}
	for _, t := range desired {
		if _, ok := currentSet[t]; !ok {
			toAdd = append(toAdd, t)
		}
	}
	for _, t := range current {
		if _, ok := desiredSet[t]; !ok {
			toRemove = append(toRemove, t)
		}
	}
	return toAdd, toRemove
}

func writeDispatchJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDispatchError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
