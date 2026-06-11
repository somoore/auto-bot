//go:build manual

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/livekit/protocol/auth"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

func TestManualLiveKitPublishSyntheticSpeech(t *testing.T) {
	wavPath := os.Getenv("AUTO_BOT_SYNTHETIC_WAV")
	if wavPath == "" {
		t.Fatal("AUTO_BOT_SYNTHETIC_WAV is required")
	}
	mono16, err := readPCM16MonoWAV(wavPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancelProbe := context.WithCancel(context.Background())
	defer cancelProbe()
	var recorder *manualWebSocketRecorder
	if appToken := os.Getenv("AUTO_BOT_APP_TOKEN"); appToken != "" {
		var err error
		recorder, err = startManualWebSocketRecorder(ctx, appToken)
		if err != nil {
			t.Fatalf("connect app websocket probe: %v", err)
		}
		defer recorder.close()
	}

	token, err := liveKitManualToken("devkey", "secret", appRoomID, "synthetic-speaker")
	if err != nil {
		t.Fatal(err)
	}
	room, err := lksdk.ConnectToRoomWithToken("ws://localhost:7880", token, &lksdk.RoomCallback{}, lksdk.WithAutoSubscribe(false))
	if err != nil {
		t.Fatalf("connect synthetic speaker: %v", err)
	}
	defer room.Disconnect()

	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: roomAudioSampleRate,
		Channels:  roomAudioChannels,
	}, "synthetic-audio", "synthetic-stream")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{Name: "synthetic-audio"}); err != nil {
		t.Fatalf("publish synthetic audio: %v", err)
	}

	enc, err := newOpusEncoder(roomAudioSampleRate, roomAudioChannels)
	if err != nil {
		t.Fatal(err)
	}
	stereo48 := upsample16kMonoTo48kStereo(mono16)
	const frameSamples = roomAudioSampleRate / 50 * roomAudioChannels
	silenceFrame := make([]int16, frameSamples)
	for i := 0; i < 100; i++ {
		if err := writeSyntheticOpusFrame(track, enc, silenceFrame); err != nil {
			t.Fatalf("write synthetic preroll sample: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rem := len(stereo48) % frameSamples; rem != 0 {
		stereo48 = append(stereo48, make([]int16, frameSamples-rem)...)
	}
	for offset := 0; offset < len(stereo48); offset += frameSamples {
		frame := stereo48[offset : offset+frameSamples]
		if err := writeSyntheticOpusFrame(track, enc, frame); err != nil {
			t.Fatalf("write synthetic sample: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := 0; i < 100; i++ {
		if err := writeSyntheticOpusFrame(track, enc, silenceFrame); err != nil {
			t.Fatalf("write synthetic tail sample: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(8 * time.Second)

	if recorder != nil {
		cancelProbe()
		recorder.wait()
		t.Logf("websocket events: %s", recorder.summary())
		if !recorder.hasStatusContaining("receiving") {
			t.Fatal("app websocket did not report receiving synthetic audio")
		}
		if !recorder.hasTranscriptionContaining("standup") && !recorder.hasTranscriptionContaining("stand up") {
			t.Fatal("app websocket did not receive a standup transcription")
		}
		if !recorder.hasMeetingStartedAfterTranscription() {
			t.Fatal("app websocket did not receive a meeting-start board update after the standup transcription")
		}
	}
}

func TestManualAppWebSocketIdleStaysOpen(t *testing.T) {
	resp, err := http.Get("http://localhost:3001/auth/session?identity=idleprobe") //nolint:gosec,noctx
	if err != nil {
		t.Fatalf("create local browser session: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("local browser session status = %s", resp.Status)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == authCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("local browser session did not return session cookie")
	}

	header := http.Header{}
	header.Set("Cookie", sessionCookie.String())
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:3001/websocket?room_id="+appRoomID+"&board_id="+appBoardID, header)
	if err != nil {
		t.Fatalf("connect app websocket: %v", err)
	}
	defer conn.Close()

	errCh := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				errCh <- err
				return
			}
		}
	}()

	select {
	case err := <-errCh:
		t.Fatalf("app websocket closed before idle window: %v", err)
	case <-time.After(70 * time.Second):
	}
}

func writeSyntheticOpusFrame(track *webrtc.TrackLocalStaticSample, enc *opusEncoder, frame []int16) error {
	opusData, err := enc.Encode(frame)
	if err != nil {
		return err
	}
	return track.WriteSample(media.Sample{Data: opusData, Duration: 20 * time.Millisecond})
}

type manualWebSocketRecorder struct {
	conn           *websocket.Conn
	done           chan struct{}
	mu             sync.Mutex
	statuses       []string
	transcriptions []string
	meetingStarted bool
	sawTranscript  bool
	errors         []string
}

func startManualWebSocketRecorder(ctx context.Context, appToken string) (*manualWebSocketRecorder, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+appToken)
	header.Set("X-Participant-Identity", "manual-probe")
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:3001/websocket?room_id="+appRoomID+"&board_id="+appBoardID, header)
	if err != nil {
		return nil, err
	}
	recorder := &manualWebSocketRecorder{
		conn: conn,
		done: make(chan struct{}),
	}
	go recorder.read(ctx)
	return recorder, nil
}

func (r *manualWebSocketRecorder) read(ctx context.Context) {
	defer close(r.done)
	go func() {
		<-ctx.Done()
		_ = r.conn.Close()
	}()
	for {
		_, raw, err := r.conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil {
				r.recordError(err.Error())
			}
			return
		}
		r.record(raw)
	}
}

func (r *manualWebSocketRecorder) record(raw []byte) {
	var outer websocketMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		r.recordError(err.Error())
		return
	}
	if outer.Event != "kanban" {
		return
	}
	var inner struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(outer.Data), &inner); err != nil {
		r.recordError(err.Error())
		return
	}
	switch inner.Event {
	case "status":
		var status string
		if err := json.Unmarshal(inner.Data, &status); err == nil {
			r.mu.Lock()
			r.statuses = append(r.statuses, status)
			r.mu.Unlock()
		}
	case "transcription":
		var transcript struct {
			Role string `json:"role"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(inner.Data, &transcript); err == nil {
			r.mu.Lock()
			r.sawTranscript = true
			r.transcriptions = append(r.transcriptions, transcript.Role+": "+transcript.Text)
			r.mu.Unlock()
		}
	case "board":
		var state struct {
			Meeting *struct {
				Active bool `json:"active"`
			} `json:"meeting"`
		}
		if err := json.Unmarshal(inner.Data, &state); err == nil && state.Meeting != nil && state.Meeting.Active {
			r.mu.Lock()
			if r.sawTranscript {
				r.meetingStarted = true
			}
			r.mu.Unlock()
		}
	}
}

func (r *manualWebSocketRecorder) recordError(err string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, err)
}

func (r *manualWebSocketRecorder) hasStatusContaining(fragment string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return containsAnyFold(r.statuses, fragment)
}

func (r *manualWebSocketRecorder) hasTranscriptionContaining(fragment string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return containsAnyFold(r.transcriptions, fragment)
}

func (r *manualWebSocketRecorder) hasMeetingStartedAfterTranscription() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meetingStarted
}

func (r *manualWebSocketRecorder) summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprintf("statuses=%q transcriptions=%q meetingStarted=%t errors=%q", r.statuses, r.transcriptions, r.meetingStarted, r.errors)
}

func (r *manualWebSocketRecorder) close() {
	_ = r.conn.Close()
}

func (r *manualWebSocketRecorder) wait() {
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
	}
}

func containsAnyFold(values []string, fragment string) bool {
	fragment = strings.ToLower(fragment)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), fragment) {
			return true
		}
	}
	return false
}

func liveKitManualToken(apiKey, apiSecret, roomName, identity string) (string, error) {
	at := auth.NewAccessToken(apiKey, apiSecret)
	at.AddGrant(&auth.VideoGrant{RoomJoin: true, Room: roomName}).SetIdentity(identity).SetValidFor(5 * time.Minute)
	return at.ToJWT()
}

func readPCM16MonoWAV(path string) ([]int16, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a RIFF/WAVE file")
	}
	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(data) {
			return nil, fmt.Errorf("truncated WAV chunk %q", chunkID)
		}
		if chunkID == "data" {
			payload := data[chunkStart:chunkEnd]
			samples := make([]int16, len(payload)/2)
			for i := range samples {
				samples[i] = int16(binary.LittleEndian.Uint16(payload[i*2:]))
			}
			return samples, nil
		}
		offset = chunkEnd
		if offset%2 == 1 {
			offset++
		}
	}
	return nil, fmt.Errorf("WAV data chunk not found")
}
