package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type kanbanStatus string

const (
	kanbanStatusBacklog    kanbanStatus = "Backlog"
	kanbanStatusInProgress kanbanStatus = "In Progress"
	kanbanStatusBlocked    kanbanStatus = "Blocked"
	kanbanStatusDone       kanbanStatus = "Done"
)

var kanbanStatuses = []kanbanStatus{
	kanbanStatusBacklog,
	kanbanStatusInProgress,
	kanbanStatusBlocked,
	kanbanStatusDone,
}

type kanbanCard struct {
	ID     string       `json:"id"`
	Status kanbanStatus `json:"status"`
	Title  string       `json:"title"`
	Notes  string       `json:"notes"`
	Tags   []string     `json:"tags"`
}

type kanbanBoardState struct {
	Cards     []kanbanCard `json:"cards"`
	UpdatedAt string       `json:"updatedAt,omitempty"`
}

type kanbanBoard struct {
	mu               sync.Mutex
	cards            []kanbanCard
	nextCreatedIndex int
	updatedAt        time.Time
	handledCalls     map[string]struct{}
}

var initialKanbanBoardCards = []kanbanCard{
	{
		ID:     "card-002",
		Status: kanbanStatusBacklog,
		Title:  "Add RTP Retransmission Buffer",
		Notes:  "Keep recent RTP packets available for NACK-driven retransmission without unbounded memory growth.",
		Tags:   []string{"webrtc", "rtp", "nack"},
	},
	{
		ID:     "card-003",
		Status: kanbanStatusBacklog,
		Title:  "Implement ICE Restart Handling",
		Notes:  "Support renegotiation paths that refresh ICE credentials and reconnect peers after network changes.",
		Tags:   []string{"webrtc", "ice", "signaling"},
	},
	{
		ID:     "card-004",
		Status: kanbanStatusBacklog,
		Title:  "Harden DTLS/SRTP Cleanup",
		Notes:  "Ensure failed and closed peer connections release transports, tracks, and SRTP state promptly.",
		Tags:   []string{"webrtc", "dtls", "srtp"},
	},
	{
		ID:     "card-005",
		Status: kanbanStatusBacklog,
		Title:  "Add Simulcast Forwarding Controls",
		Notes:  "Choose forwarded RTP layers per subscriber so the server can adapt streams to bandwidth and viewport size.",
		Tags:   []string{"webrtc", "simulcast", "bandwidth"},
	},
	{
		ID:     "card-001",
		Status: kanbanStatusBacklog,
		Title:  "Finish RTP HEVC Packetizer",
		Notes:  "Complete HEVC payload fragmentation, aggregation, and marker-bit handling for outbound RTP streams.",
		Tags:   []string{"webrtc", "rtp", "hevc"},
	},
}

func newKanbanBoard() *kanbanBoard {
	return &kanbanBoard{
		cards:            cloneKanbanCards(initialKanbanBoardCards),
		nextCreatedIndex: 1,
		updatedAt:        time.Now().UTC(),
		handledCalls:     map[string]struct{}{},
	}
}

const maxHandledCalls = 1000

// MarkCallHandled returns true if the callID was already handled (duplicate).
func (board *kanbanBoard) MarkCallHandled(callID string) bool {
	board.mu.Lock()
	defer board.mu.Unlock()

	if _, ok := board.handledCalls[callID]; ok {
		return true
	}
	if len(board.handledCalls) >= maxHandledCalls {
		// Evict oldest entries (simple: clear all when limit hit)
		board.handledCalls = map[string]struct{}{}
	}
	board.handledCalls[callID] = struct{}{}
	return false
}

func (board *kanbanBoard) ApplyToolCall(toolName string, rawArgs string) (map[string]any, bool, error) {
	args := map[string]any{}
	if trimmed := strings.TrimSpace(rawArgs); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return nil, false, fmt.Errorf("parse %s arguments: %w", toolName, err)
		}
	}

	switch toolName {
	case "create_ticket":
		return board.createTicket(args)
	case "move_ticket":
		return board.moveTicket(args)
	case "add_tags":
		return board.addTags(args)
	case "update_ticket":
		return board.updateTicket(args)
	case "delete_ticket":
		return board.deleteTicket(args)
	case "do_nothing":
		reason := asString(args["reason"])
		if reason == "" {
			reason = "No board update requested."
		}
		return map[string]any{
			"ok":     true,
			"reason": reason,
		}, false, nil
	case "show_ticket":
		cardID := asString(args["card_id"])
		if cardID == "" {
			return nil, false, fmt.Errorf("card_id is required")
		}
		board.mu.Lock()
		var clone kanbanCard
		var found bool
		for i := range board.cards {
			if board.cards[i].ID == cardID {
				clone = cloneKanbanCard(board.cards[i])
				found = true
				break
			}
		}
		board.mu.Unlock()
		if !found {
			return map[string]any{"ok": false, "error": "card not found"}, false, nil
		}
		broadcastKanbanEvent("highlight", map[string]any{"card_id": cardID})
		return map[string]any{
			"ok":      true,
			"card_id": clone.ID,
			"title":   clone.Title,
			"status":  clone.Status,
			"notes":   clone.Notes,
			"tags":    clone.Tags,
		}, false, nil
	case "close_detail":
		broadcastKanbanEvent("close_detail", nil)
		return map[string]any{"ok": true}, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported function %q", toolName)
	}
}

func (board *kanbanBoard) SnapshotState() kanbanBoardState {
	board.mu.Lock()
	defer board.mu.Unlock()

	state := kanbanBoardState{
		Cards: cloneKanbanCards(board.cards),
	}
	if !board.updatedAt.IsZero() {
		state.UpdatedAt = board.updatedAt.UTC().Format(time.RFC3339Nano)
	}

	return state
}

func (board *kanbanBoard) BoardContextJSON() string {
	raw, err := json.Marshal(board.SnapshotState().Cards)
	if err != nil {
		return "[]"
	}

	return string(raw)
}

func (board *kanbanBoard) SessionInstructions() string {
	return strings.Join([]string{
		"You are a voice-operated Kanban board scrum master.",
		"You run the standup meeting. Track each speaker and what they report.",
		"Listen to the user and decide whether they want to create a ticket, move a ticket between columns, add tags to a ticket, update a ticket, delete a ticket, show/open a ticket, or do nothing.",
		"Use the board card ids exactly as provided when operating on existing tickets.",
		"Users may say ticket, card, task, issue, or sticky note; treat those as Kanban cards.",
		"CRITICAL: When a user says 'open a task' or 'open the ticket', they mean SHOW it (call show_ticket), NOT complete/finish it. Only move to Done when they explicitly say finish, complete, ship, close, or done AS AN ACTION VERB, not when those words appear in a card title. For example, 'show me the Finish RTP HEVC Packetizer' means call show_ticket for the card titled 'Finish RTP HEVC Packetizer' — the word Finish is part of the title, not an instruction to complete it. Always check if the user's words match an existing card title before interpreting them as board operations.",
		"Available columns are Backlog, In Progress, Blocked, and Done.",
		"This is used during standups and meetings. Treat concrete first-person status updates as implicit board operations; do not wait for the user to say create a ticket.",
		"If a user says they shipped, fixed, completed, closed, or finished work, move an existing related ticket to Done if one exists; otherwise create a concise Done ticket.",
		"If a user says they started, began, picked up, or are working on something, move an existing related ticket to In Progress if one exists; otherwise create a concise In Progress ticket.",
		"If a user says they are blocked, waiting on something, dependent on another team, or that work might slip, move or create the related ticket in Blocked and add blocked, dependency, or risk tags as appropriate.",
		"Track meeting context across turns. If a follow-up sentence adds dependency, blocker, or schedule-risk context for the most recently discussed related card, update or move that existing ticket instead of creating a duplicate.",
		"If a transcript includes a speaker label such as Sean:, do not include the label in the title; use it only as context for notes or tags when useful.",
		"If a user asks to start, work on, pick up, or begin a ticket, move it to In Progress.",
		"If a user asks to block, mark blocked, or note a dependency for a ticket, move it to Blocked and preserve the blocker details in notes.",
		"If a user asks to ship, finish, complete, close, or mark done, move it to Done.",
		"If a user asks to park, punt, defer, or move something back, move it to Backlog.",
		"If a user asks to add a tag, call add_tags; do not replace existing tags.",
		"If a user asks to open, show, view, display, pull up, or look at a ticket, you MUST call show_ticket — this opens the detail modal on their screen. Do NOT just describe the card in speech; the user needs to see it visually. After calling show_ticket, say a brief confirmation like 'Opening the ticket.' IMPORTANT: 'open' means show/display a ticket, NOT complete or finish it. If the user says 'open' and no matching ticket exists on the board, do NOT create one automatically — instead, verbally tell the user that no matching ticket was found and ask if they would like to create a new one.",
		"If one transcript contains multiple status updates, call one tool for each board operation.",
		"If the user asks for an operation or gives an implicit status update, call the relevant tool. Prefer tools over text replies.",
		"If the user is only wrapping up, handing off, giving filler, or saying something like That's it from me, call do_nothing with a short reason.",
		"If the user is not asking for a board operation and is not giving a concrete status update, call do_nothing with a short reason.",
		"After every board operation tool call, briefly speak a one-sentence confirmation of what you did, e.g. \"Moved ICE restart handling to In Progress.\"",
		"When calling do_nothing, stay silent unless the user asked a direct question.",
		"At the end of the meeting, summarize all changes made and ask the team to confirm everything looks correct.",
		fmt.Sprintf("Current Kanban board JSON: %s", board.BoardContextJSON()),
	}, " ")
}

// KanbanToolDefs returns provider-agnostic tool definitions.
type kanbanToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func (board *kanbanBoard) KanbanToolDefs() []kanbanToolDef {
	statusProperty := map[string]any{
		"type":        "string",
		"description": "Kanban column for the ticket.",
		"enum":        []string{"Backlog", "In Progress", "Blocked", "Done"},
	}
	tagsProperty := map[string]any{
		"type":        "array",
		"description": "Short labels that capture people, area, state, or risk. Use blocked/dependency/risk tags for blockers when appropriate.",
		"items":       map[string]any{"type": "string"},
	}

	return []kanbanToolDef{
		{
			Name:        "create_ticket",
			Description: "Create a new Kanban ticket/card for explicit requests or implicit meeting status updates such as shipped, started, or blocked work.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":  map[string]any{"type": "string", "description": "Concise title for the work, without speaker prefixes such as Sean:."},
					"notes":  map[string]any{"type": "string", "description": "Useful context from the utterance, including blocker, dependency, or schedule-risk details."},
					"tags":   tagsProperty,
					"status": statusProperty,
				},
				"required":             []string{"title", "notes", "tags"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "move_ticket",
			Description: "Move an existing Kanban ticket/card to another column, including Blocked when work is waiting on a dependency.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"card_id": map[string]any{"type": "string", "description": "Existing board card id."},
					"status":  statusProperty,
				},
				"required":             []string{"card_id", "status"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "add_tags",
			Description: "Add one or more tags to an existing Kanban ticket/card without removing existing tags.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"card_id": map[string]any{"type": "string", "description": "Existing board card id."},
					"tags":    tagsProperty,
				},
				"required":             []string{"card_id", "tags"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "update_ticket",
			Description: "Update the title or notes of an existing Kanban ticket/card. Use this to merge follow-up standup details, dependency details, or slip-risk context into the existing notes.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"card_id": map[string]any{"type": "string", "description": "Existing board card id."},
					"title":   map[string]any{"type": "string", "description": "Replacement title, when the existing title should be made clearer."},
					"notes":   map[string]any{"type": "string", "description": "Full replacement notes. Preserve useful existing notes while adding the new context."},
				},
				"required":             []string{"card_id"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "delete_ticket",
			Description: "Delete an existing Kanban ticket/card.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"card_id": map[string]any{"type": "string", "description": "Existing board card id."},
				},
				"required":             []string{"card_id"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "do_nothing",
			Description: "Use this when the user is not asking to operate on the Kanban board, is only wrapping up, or says a handoff phrase like That's it from me.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{"type": "string"},
				},
				"required":             []string{"reason"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "show_ticket",
			Description: "REQUIRED: You MUST call this tool whenever the user asks to open, show, display, view, look at, pull up, or focus on a ticket. This tool opens the card detail modal on the user's screen. Do NOT describe the card verbally without calling this tool first — the user needs to SEE it visually.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"card_id": map[string]any{
						"description": "Existing board card id.",
						"type":        "string",
					},
				},
				"required":             []string{"card_id"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "close_detail",
			Description: "Close the currently open card detail view. Use when the user says close it, close the ticket, that's good, thanks, dismiss, never mind, or done looking.",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
}

func (board *kanbanBoard) createTicket(args map[string]any) (map[string]any, bool, error) {
	title := asString(args["title"])
	if title == "" {
		return nil, false, fmt.Errorf("title is required")
	}
	if len(title) > 200 {
		title = title[:200]
	}

	notes := asString(args["notes"])
	if len(notes) > 2000 {
		notes = notes[:2000]
	}
	tags := uniqueStrings(asStringSlice(args["tags"]))
	if len(tags) > 20 {
		tags = tags[:20]
	}
	for i, t := range tags {
		if len(t) > 50 {
			tags[i] = t[:50]
		}
	}
	status := kanbanStatusBacklog
	if rawStatus, ok := args["status"]; ok {
		parsedStatus, err := parseKanbanStatus(rawStatus)
		if err != nil {
			return nil, false, err
		}
		status = parsedStatus
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card := kanbanCard{
		ID:     board.createCardIDLocked(),
		Status: status,
		Title:  title,
		Notes:  notes,
		Tags:   tags,
	}
	board.cards = append(board.cards, card)
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"created": true,
		"card":    cloneKanbanCard(card),
	}, true, nil
}

func (board *kanbanBoard) moveTicket(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}

	status, err := parseKanbanStatus(args["status"])
	if err != nil {
		return nil, false, err
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.Status = status
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"moved":   true,
		"card_id": cardID,
		"status":  status,
	}, true, nil
}

func (board *kanbanBoard) addTags(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}

	tags := uniqueStrings(asStringSlice(args["tags"]))

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	card.Tags = uniqueStrings(append(card.Tags, tags...))
	board.touchLocked()

	return map[string]any{
		"ok":         true,
		"tags_added": true,
		"card_id":    cardID,
		"tags":       append([]string(nil), tags...),
	}, true, nil
}

func (board *kanbanBoard) updateTicket(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}

	title := asString(args["title"])
	notes := asString(args["notes"])

	board.mu.Lock()
	defer board.mu.Unlock()

	card, ok := board.findCardLocked(cardID)
	if !ok {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	if title != "" {
		card.Title = title
	}
	if notes != "" {
		card.Notes = notes
	}
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"updated": true,
		"card_id": cardID,
	}, true, nil
}

func (board *kanbanBoard) deleteTicket(args map[string]any) (map[string]any, bool, error) {
	cardID := asString(args["card_id"])
	if cardID == "" {
		return nil, false, fmt.Errorf("card_id is required")
	}

	board.mu.Lock()
	defer board.mu.Unlock()

	index := -1
	for candidateIndex, card := range board.cards {
		if card.ID == cardID {
			index = candidateIndex
			break
		}
	}
	if index == -1 {
		return nil, false, fmt.Errorf("unknown card_id: %s", cardID)
	}
	board.cards = append(board.cards[:index], board.cards[index+1:]...)
	board.touchLocked()

	return map[string]any{
		"ok":      true,
		"deleted": true,
		"card_id": cardID,
	}, true, nil
}

func (board *kanbanBoard) createCardIDLocked() string {
	for {
		cardID := fmt.Sprintf("kanban-card-%03d", board.nextCreatedIndex)
		board.nextCreatedIndex++
		if _, exists := board.findCardLocked(cardID); exists {
			continue
		}
		return cardID
	}
}

func (board *kanbanBoard) findCardLocked(cardID string) (*kanbanCard, bool) {
	for index := range board.cards {
		if board.cards[index].ID == cardID {
			return &board.cards[index], true
		}
	}

	return nil, false
}

func (board *kanbanBoard) touchLocked() {
	board.updatedAt = time.Now().UTC()
}

// --- WebSocket client registry for board event broadcasting ---

var (
	wsClientsLock sync.RWMutex
	wsClients     []*threadSafeWriter
)

func registerWSClient(c *threadSafeWriter) bool {
	wsClientsLock.Lock()
	defer wsClientsLock.Unlock()
	if len(wsClients) >= maxWSClients {
		return false
	}
	wsClients = append(wsClients, c)
	return true
}

func unregisterWSClient(c *threadSafeWriter) {
	wsClientsLock.Lock()
	for i, client := range wsClients {
		if client == c {
			wsClients = append(wsClients[:i], wsClients[i+1:]...)
			break
		}
	}
	wsClientsLock.Unlock()
}

func sendKanbanEvent(ws *threadSafeWriter, event string, data any) error {
	raw, err := json.Marshal(map[string]any{
		"event": event,
		"data":  data,
	})
	if err != nil {
		return err
	}

	return ws.WriteJSON(&websocketMessage{
		Event: "kanban",
		Data:  string(raw),
	})
}

func broadcastKanbanEvent(event string, data any) {
	raw, err := json.Marshal(map[string]any{
		"event": event,
		"data":  data,
	})
	if err != nil {
		log.Errorf("Failed to encode Kanban event: %v", err)
		return
	}

	wsClientsLock.RLock()
	clients := make([]*threadSafeWriter, len(wsClients))
	copy(clients, wsClients)
	wsClientsLock.RUnlock()

	for _, ws := range clients {
		if err := ws.WriteJSON(&websocketMessage{
			Event: "kanban",
			Data:  string(raw),
		}); err != nil {
			log.Errorf("Failed to send Kanban event: %v", err)
		}
	}
}

// --- Utility functions ---

func cloneKanbanCards(cards []kanbanCard) []kanbanCard {
	clonedCards := make([]kanbanCard, 0, len(cards))
	for _, card := range cards {
		clonedCards = append(clonedCards, cloneKanbanCard(card))
	}

	return clonedCards
}

func cloneKanbanCard(card kanbanCard) kanbanCard {
	return kanbanCard{
		ID:     card.ID,
		Status: card.Status,
		Title:  card.Title,
		Notes:  card.Notes,
		Tags:   append([]string(nil), card.Tags...),
	}
}

func asString(value any) string {
	candidate, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(candidate)
}

func asStringSlice(value any) []string {
	rawValues, ok := value.([]any)
	if !ok {
		return nil
	}

	values := make([]string, 0, len(rawValues))
	for _, rawValue := range rawValues {
		if value := asString(rawValue); value != "" {
			values = append(values, value)
		}
	}

	return values
}

func parseKanbanStatus(value any) (kanbanStatus, error) {
	status := kanbanStatus(asString(value))
	for _, candidate := range kanbanStatuses {
		if candidate == status {
			return status, nil
		}
	}

	return "", fmt.Errorf("unknown Kanban status: %v", value)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalizedValue := strings.TrimSpace(value)
		if normalizedValue == "" {
			continue
		}
		if _, ok := seen[normalizedValue]; ok {
			continue
		}
		seen[normalizedValue] = struct{}{}
		result = append(result, normalizedValue)
	}

	return result
}

func mustMarshalJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return `{"ok":false,"error":"Could not encode function output."}`
	}

	return string(raw)
}
