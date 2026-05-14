package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	realtimeCallsURL          = "https://api.openai.com/v1/realtime/calls"
	defaultRealtimeModel      = "gpt-realtime-2"
	defaultReasoningEffort    = "low"
	realtimeEventChannelLabel = "oai-events"
	realtimeInputTrackID      = "kanban-realtime:mixed-audio"
	realtimeInputStreamID     = "kanban-realtime-input"
	realtimeMixedAudioSinkKey = "kanban-realtime"
)

type kanbanRealtimeEvent struct {
	Type       string `json:"type,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Name       string `json:"name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	CallID     string `json:"call_id,omitempty"`
	Error      *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
	Item     *kanbanRealtimeOutputItem `json:"item,omitempty"`
	Response *struct {
		Output []kanbanRealtimeOutputItem `json:"output,omitempty"`
	} `json:"response,omitempty"`
}

type kanbanRealtimeOutputItem struct {
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
}

type kanbanBoardApp struct {
	board *kanbanBoard

	mu         sync.Mutex
	model      string
	pc         *webrtc.PeerConnection
	events     *webrtc.DataChannel
	inputTrack *webrtc.TrackLocalStaticSample
	inputEnc   *opusEncoder
	connected  bool
	closeOnce  sync.Once
}

func newKanbanBoardApp(board *kanbanBoard) *kanbanBoardApp {
	return &kanbanBoardApp{
		board: board,
	}
}

func (app *kanbanBoardApp) JoinConferenceRoom() error {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is not configured")
	}

	peerConnection, err := newPeerConnection()
	if err != nil {
		return fmt.Errorf("create Realtime peer connection: %w", err)
	}

	inputTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: roomAudioSampleRate,
			Channels:  roomAudioChannels,
		},
		realtimeInputTrackID,
		realtimeInputStreamID,
	)
	if err != nil {
		_ = peerConnection.Close()
		return fmt.Errorf("create Realtime mixed audio input track: %w", err)
	}

	inputEnc, err := newOpusEncoder(roomAudioSampleRate, roomAudioChannels)
	if err != nil {
		_ = peerConnection.Close()
		return fmt.Errorf("create Realtime mixed audio encoder: %w", err)
	}

	// Use a sendrecv transceiver so OpenAI can both receive our mixed room
	// audio and send the assistant's voice back on the same m-line, which
	// we then fan out to browser participants via the global trackLocals
	// fanout used for participant audio.
	inputTransceiver, err := peerConnection.AddTransceiverFromTrack(inputTrack, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendrecv,
	})
	if err != nil {
		_ = peerConnection.Close()
		return fmt.Errorf("attach Realtime mixed audio input track: %w", err)
	}
	go drainRTCP(inputTransceiver.Sender())
	peerConnection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Infof("Got OpenAI Realtime track: Kind=%s, ID=%s, PayloadType=%d", t.Kind(), t.ID(), t.PayloadType())
		trackLocal := addTrack(t)
		defer removeTrack(trackLocal)
		for {
			packet, _, err := t.ReadRTP()
			if err != nil {
				return
			}
			packet.Extension = false
			packet.Extensions = nil
			if err := trackLocal.WriteRTP(packet); err != nil {
				return
			}
		}
	})

	events, err := peerConnection.CreateDataChannel(realtimeEventChannelLabel, nil)
	if err != nil {
		_ = peerConnection.Close()
		return fmt.Errorf("create Realtime event data channel: %w", err)
	}

	model := realtimeModel()
	app.mu.Lock()
	app.model = model
	app.pc = peerConnection
	app.events = events
	app.inputTrack = inputTrack
	app.inputEnc = inputEnc
	app.mu.Unlock()

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Infof("OpenAI Realtime peer state changed: %s", state.String())
		broadcastKanbanEvent("status", "OpenAI Realtime: "+state.String())
	})
	events.OnOpen(func() {
		log.Infof("OpenAI Realtime event channel opened")
		_ = app.SendEvent(app.sessionUpdateEvent())
		broadcastKanbanEvent("status", "Kanban assistant is listening")
	})
	events.OnMessage(func(message webrtc.DataChannelMessage) {
		app.handleRealtimeEvent(message.Data)
	})

	go func() {
		if err := app.connectRealtimePeer(apiKey, model); err != nil {
			log.Errorf("Failed to connect OpenAI Realtime peer: %v", err)
			broadcastKanbanEvent("status", "OpenAI Realtime disabled: "+err.Error())
			_ = peerConnection.Close()
			return
		}
		if roomMixer != nil {
			roomMixer.setSink(realtimeMixedAudioSinkKey, app)
		}
	}()

	return nil
}

func (app *kanbanBoardApp) Close() error {
	var closeErr error
	app.closeOnce.Do(func() {
		if roomMixer != nil {
			roomMixer.removeSink(realtimeMixedAudioSinkKey)
		}

		app.mu.Lock()
		peerConnection := app.pc
		app.mu.Unlock()
		if peerConnection != nil {
			closeErr = peerConnection.Close()
		}
	})

	return closeErr
}

func (app *kanbanBoardApp) connectRealtimePeer(apiKey string, model string) error {
	app.mu.Lock()
	if app.connected {
		app.mu.Unlock()
		return nil
	}
	peerConnection := app.pc
	app.mu.Unlock()

	if peerConnection == nil {
		return fmt.Errorf("Realtime peer connection is unavailable")
	}

	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("create Realtime offer: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err := peerConnection.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("set Realtime local description: %w", err)
	}
	<-gatherComplete

	localDescription := peerConnection.LocalDescription()
	if localDescription == nil || strings.TrimSpace(localDescription.SDP) == "" {
		return fmt.Errorf("Realtime peer connection did not produce a local description")
	}

	answerSDP, err := app.createRealtimeCall(apiKey, model, localDescription.SDP)
	if err != nil {
		return err
	}

	if err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		return fmt.Errorf("set Realtime remote description: %w", err)
	}

	app.mu.Lock()
	app.connected = true
	app.mu.Unlock()

	return nil
}

func (app *kanbanBoardApp) WriteMixedPCM(roomPCM []int16) error {
	if len(roomPCM) == 0 {
		return nil
	}
	if len(roomPCM)%roomAudioMixFrameSize != 0 {
		return fmt.Errorf("mixed PCM length %d must be a multiple of %d samples", len(roomPCM), roomAudioMixFrameSize)
	}

	app.mu.Lock()
	inputTrack := app.inputTrack
	inputEnc := app.inputEnc
	app.mu.Unlock()

	if inputTrack == nil || inputEnc == nil {
		return fmt.Errorf("Realtime mixed audio input is unavailable")
	}

	for offset := 0; offset < len(roomPCM); offset += roomAudioMixFrameSize {
		frame := roomPCM[offset : offset+roomAudioMixFrameSize]

		opusFrame, err := inputEnc.Encode(frame)
		if err != nil {
			return fmt.Errorf("encode mixed room audio: %w", err)
		}

		if err := inputTrack.WriteSample(media.Sample{
			Data:     opusFrame,
			Duration: roomAudioMixInterval,
		}); err != nil {
			return fmt.Errorf("write mixed room audio sample: %w", err)
		}
	}

	return nil
}

func drainRTCP(sender *webrtc.RTPSender) {
	buffer := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}

func (app *kanbanBoardApp) createRealtimeCall(apiKey string, model string, offerSDP string) (string, error) {
	contentType, body, err := buildRealtimeCallRequest(offerSDP, app.sessionConfig(model))
	if err != nil {
		return "", err
	}

	request, err := http.NewRequest(http.MethodPost, realtimeCallsURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create Realtime request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", contentType)

	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("create Realtime session: %w", err)
	}
	defer response.Body.Close()

	answerSDP, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read Realtime answer: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Realtime session failed: status=%s body=%s", response.Status, strings.TrimSpace(string(answerSDP)))
	}

	return string(answerSDP), nil
}

func buildRealtimeCallRequest(offerSDP string, session map[string]any) (string, []byte, error) {
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return "", nil, fmt.Errorf("marshal Realtime session: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("sdp", offerSDP); err != nil {
		return "", nil, fmt.Errorf("write SDP offer: %w", err)
	}
	if err := writer.WriteField("session", string(sessionJSON)); err != nil {
		return "", nil, fmt.Errorf("write session config: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", nil, fmt.Errorf("finalize multipart request: %w", err)
	}

	return writer.FormDataContentType(), body.Bytes(), nil
}

func (app *kanbanBoardApp) SendEvent(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal Realtime event: %w", err)
	}

	app.mu.Lock()
	events := app.events
	app.mu.Unlock()
	if events == nil || events.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("Realtime event channel is unavailable")
	}

	return events.SendText(string(raw))
}

func (app *kanbanBoardApp) sessionConfig(model string) map[string]any {
	// Format tool defs for OpenAI Realtime wire format
	toolDefs := app.board.KanbanToolDefs()
	tools := make([]map[string]any, 0, len(toolDefs))
	for _, def := range toolDefs {
		tools = append(tools, map[string]any{
			"type":        "function",
			"name":        def.Name,
			"description": def.Description,
			"parameters":  def.Parameters,
		})
	}

	session := map[string]any{
		"type":              "realtime",
		"model":             model,
		"output_modalities": []string{"audio"},
		"audio": map[string]any{
			"input": map[string]any{
				"noise_reduction": map[string]any{
					"type": "near_field",
				},
				"transcription": map[string]any{
					"model":    "gpt-4o-mini-transcribe",
					"language": "en",
				},
				"turn_detection": map[string]any{
					"type":                "server_vad",
					"threshold":           0.5,
					"prefix_padding_ms":   300,
					"silence_duration_ms": 200,
					"create_response":     true,
					"interrupt_response":  false,
				},
			},
		},
		"instructions": app.board.SessionInstructions(),
		"tools":        tools,
		"tool_choice":  "auto",
	}

	if usesAdvancedCommandProfile(model) {
		session["reasoning"] = map[string]any{
			"effort": defaultReasoningEffort,
		}
	}

	return session
}

func (app *kanbanBoardApp) sessionUpdateEvent() map[string]any {
	return map[string]any{
		"type":    "session.update",
		"session": app.sessionConfig(app.model),
	}
}

func realtimeModel() string {
	if model := strings.TrimSpace(os.Getenv("OPENAI_REALTIME_MODEL")); model != "" {
		return model
	}

	return defaultRealtimeModel
}

func usesAdvancedCommandProfile(model string) bool {
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	return normalizedModel == "gpt-realtime-2"
}

func (app *kanbanBoardApp) handleRealtimeEvent(raw []byte) {
	var event kanbanRealtimeEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		log.Errorf("Failed to parse OpenAI Realtime event: %v", err)
		return
	}

	switch event.Type {
	case "error":
		if event.Error != nil {
			log.Errorf("OpenAI Realtime error code=%s message=%s", event.Error.Code, event.Error.Message)
			broadcastKanbanEvent("status", event.Error.Message)
		}
	case "response.output_item.done":
		if event.Item != nil && event.Item.Type == "function_call" {
			app.handleToolCall(*event.Item)
		}
	case "response.function_call_arguments.done":
		app.handleToolCall(kanbanRealtimeOutputItem{
			Type:      "function_call",
			Name:      event.Name,
			Arguments: event.Arguments,
			CallID:    event.CallID,
		})
	case "response.done":
		if event.Response == nil {
			return
		}
		for _, outputItem := range event.Response.Output {
			if outputItem.Type == "function_call" {
				app.handleToolCall(outputItem)
			}
		}
	}
}

func (app *kanbanBoardApp) handleToolCall(outputItem kanbanRealtimeOutputItem) {
	if strings.TrimSpace(outputItem.CallID) == "" {
		log.Errorf("Ignoring Kanban tool call %q without call_id", outputItem.Name)
		return
	}

	if app.board.MarkCallHandled(outputItem.CallID) {
		return
	}

	result, changed, err := app.board.ApplyToolCall(outputItem.Name, outputItem.Arguments)
	if err != nil {
		result = map[string]any{
			"ok":    false,
			"error": "tool call failed",
		}
	}

	if err := app.SendEvent(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": outputItem.CallID,
			"output":  mustMarshalJSON(result),
		},
	}); err != nil {
		log.Errorf("Failed to send Kanban function output: %v", err)
	}

	if !changed {
		return
	}

	broadcastKanbanEvent("board", app.board.SnapshotState())
	if err := app.SendEvent(app.sessionUpdateEvent()); err != nil {
		log.Errorf("Failed to refresh Kanban Realtime session: %v", err)
	}
}
