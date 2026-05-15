package main

import (
	"context"
	"encoding/json"
	"flag"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

const (
	maxWSReadBytes = 64 * 1024 // 64KB per WebSocket message
	maxWSClients   = 100
	wsWriteWait    = 10 * time.Second
	wsPongWait     = 60 * time.Second
	wsPingInterval = (wsPongWait * 9) / 10
	wsRateLimit    = 60
	tokenRateLimit = 30
)

// nolint
var (
	addr           = flag.String("addr", ":3000", "http service address")
	allowedOrigins = flag.String("allowed-origins", "", "comma-separated allowed WebSocket origins (empty = same-origin only)")
	apiToken       = ""

	upgrader      = websocket.Upgrader{}
	indexTemplate = &template.Template{}

	listLock        sync.RWMutex
	peerConnections []peerConnectionState
	trackLocals     map[string]*webrtc.TrackLocalStaticRTP

	log = logging.NewDefaultLoggerFactory().NewLogger("openai-realtime-meeting-assistant")

	sharedBoard   *kanbanBoard
	kanbanApp     *kanbanBoardApp
	novaSonic     *novaSonicApp
	roomMixer     *audioMixer
	voiceProvider string
	jiraSync      *jiraSyncer

	websocketLimiter    = newFixedWindowRateLimiter(wsRateLimit, time.Minute)
	livekitTokenLimiter = newFixedWindowRateLimiter(tokenRateLimit, time.Minute)
)

var validIdentityRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type websocketMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

type peerConnectionState struct {
	peerConnection *webrtc.PeerConnection
	websocket      *threadSafeWriter
	acceptTrack    func(*webrtc.TrackLocalStaticRTP) bool
	shouldSignal   func(desiredTrackCount int) bool
	signal         func(gatherComplete <-chan struct{}) error
}

func (p peerConnectionState) acceptsTrack(track *webrtc.TrackLocalStaticRTP) bool {
	if p.acceptTrack == nil {
		return true
	}

	return p.acceptTrack(track)
}

func (p peerConnectionState) shouldSignalWithDesiredTrackCount(desiredTrackCount int) bool {
	if p.shouldSignal == nil {
		return true
	}

	return p.shouldSignal(desiredTrackCount)
}

func main() {
	flag.Parse()

	voiceProvider = strings.TrimSpace(os.Getenv("VOICE_PROVIDER"))
	if voiceProvider == "" {
		voiceProvider = "openai"
	}

	apiToken = strings.TrimSpace(os.Getenv("APP_API_TOKEN"))
	if apiToken == "" && strings.EqualFold(getEnvDefault("APP_ENV", "production"), "local") {
		apiToken = defaultLocalAPIToken
		log.Warnf("Using local-only default APP_API_TOKEN; set APP_API_TOKEN before sharing this server")
	}
	if err := configureAppSecurity(); err != nil {
		panic(err)
	}

	upgrader.CheckOrigin = makeOriginChecker(*allowedOrigins)

	boardStore, err := setupBoardStore()
	if err != nil {
		panic(err)
	}
	if boardStore != nil {
		defer boardStore.Close()
	}
	sharedBoard, err = newPersistentKanbanBoard(appBoardID, boardStore)
	if err != nil {
		panic(err)
	}
	appContext := context.Background()
	configuredJiraSync, err := setupJiraSync(appContext, sharedBoard)
	if err != nil {
		log.Errorf("Jira sync disabled: %v", err)
	} else {
		jiraSync = configuredJiraSync
	}

	switch voiceProvider {
	case "openai":
		trackLocals = map[string]*webrtc.TrackLocalStaticRTP{}
		roomMixer = newAudioMixer()
		defer roomMixer.close()
		kanbanApp = newKanbanBoardApp(sharedBoard)
		defer kanbanApp.Close()
		if err := kanbanApp.JoinConferenceRoom(); err != nil {
			log.Errorf("Kanban Realtime peer disabled: %v", err)
		}

	case "nova-sonic":
		novaSonic = newNovaSonicApp(sharedBoard)
		go func() {
			for attempt := 1; attempt <= 15; attempt++ {
				if err := novaSonic.JoinConferenceRoom(); err != nil {
					log.Errorf("Nova Sonic connect attempt %d/15: %v", attempt, err)
					time.Sleep(2 * time.Second)
					continue
				}
				return
			}
			log.Errorf("Nova Sonic agent disabled: could not connect after 15 attempts")
		}()
		defer novaSonic.Close()

	default:
		log.Errorf("Unknown VOICE_PROVIDER=%q, defaulting to openai", voiceProvider)
		voiceProvider = "openai"
		trackLocals = map[string]*webrtc.TrackLocalStaticRTP{}
		roomMixer = newAudioMixer()
		defer roomMixer.close()
		kanbanApp = newKanbanBoardApp(sharedBoard)
		defer kanbanApp.Close()
		if err := kanbanApp.JoinConferenceRoom(); err != nil {
			log.Errorf("Kanban Realtime peer disabled: %v", err)
		}
	}

	indexHTMLFile := "web/index.html"
	if voiceProvider == "nova-sonic" {
		indexHTMLFile = "web/index_livekit.html"
	}
	indexHTML, err := os.ReadFile(indexHTMLFile)
	if err != nil {
		panic(err)
	}
	indexTemplate = template.Must(template.New("").Parse(string(indexHTML)))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/auth/session", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sessionStatusHandler(w, r)
		case http.MethodPost:
			createSessionHandler(w, r)
		case http.MethodDelete:
			deleteSessionHandler(w, r)
		default:
			setSecurityHeaders(w)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/websocket", websocketHandler)

	baseURL := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		wsURL := baseURL
		if wsURL == "" {
			scheme := "ws"
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				scheme = "wss"
			}
			wsURL = scheme + "://" + r.Host + "/websocket"
		}
		if err = indexTemplate.Execute(w, map[string]string{
			"WS": wsURL,
		}); err != nil {
			log.Errorf("Failed to parse index template: %v", err)
		}
	})

	if voiceProvider == "nova-sonic" {
		mux.HandleFunc("/livekit-token", livekitTokenHandler)
	}

	if voiceProvider == "openai" {
		go func() {
			for range time.NewTicker(time.Second * 3).C {
				dispatchKeyFrame()
			}
		}()
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	log.Infof("Starting server on %s with VOICE_PROVIDER=%s", *addr, voiceProvider)
	if err = srv.ListenAndServe(); err != nil {
		log.Errorf("Failed to start http server: %v", err)
	}
}

func livekitTokenHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)

	if !livekitTokenLimiter.Allow(clientAddress(r)) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	authCtx, ok := authorizeRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	identity := normalizeParticipantIdentity(r.URL.Query().Get("identity"))
	if identity == "" {
		identity = authCtx.Identity
	}
	if identity == "" {
		http.Error(w, "invalid identity: must be 1-64 alphanumeric/dash/underscore characters", http.StatusBadRequest)
		return
	}
	if authCtx.SessionID != "" && identity != authCtx.Identity {
		http.Error(w, "identity does not match authenticated session", http.StatusForbidden)
		return
	}

	token, err := generateLivekitToken(authCtx.RoomID, identity)
	if err != nil {
		log.Errorf("Failed to generate LiveKit token: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":       token,
		"livekit_url": browserLiveKitURL(r),
		"room_id":     authCtx.RoomID,
		"board_id":    authCtx.BoardID,
		"identity":    identity,
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func newPeerConnection() (*webrtc.PeerConnection, error) {
	settingEngine := webrtc.SettingEngine{}
	if nat1To1IP := os.Getenv("PION_NAT1TO1_IP"); nat1To1IP != "" {
		settingEngine.SetNAT1To1IPs([]string{nat1To1IP}, webrtc.ICECandidateTypeHost)
	}

	return webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)).NewPeerConnection(webrtc.Configuration{})
}

func newBrowserPeerConnection() (*webrtc.PeerConnection, error) {
	settingEngine := webrtc.SettingEngine{}
	if nat1To1IP := os.Getenv("PION_NAT1TO1_IP"); nat1To1IP != "" {
		settingEngine.SetNAT1To1IPs([]string{nat1To1IP}, webrtc.ICECandidateTypeHost)
	}
	if os.Getenv("CONFERENCE_LOOPBACK_ONLY") == "1" {
		settingEngine.SetInterfaceFilter(func(name string) bool { return name == "lo0" })
		settingEngine.SetIncludeLoopbackCandidate(true)
	}

	return webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)).NewPeerConnection(webrtc.Configuration{})
}

func addTrack(t *webrtc.TrackRemote) *webrtc.TrackLocalStaticRTP { // nolint
	listLock.Lock()
	defer func() {
		listLock.Unlock()
		signalPeerConnections()
	}()

	trackLocal, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
	if err != nil {
		panic(err)
	}

	trackLocals[t.ID()] = trackLocal

	return trackLocal
}

func removeTrack(t *webrtc.TrackLocalStaticRTP) {
	listLock.Lock()
	defer func() {
		listLock.Unlock()
		signalPeerConnections()
	}()

	delete(trackLocals, t.ID())
}

func signalPeerConnections() { // nolint
	listLock.Lock()
	defer func() {
		listLock.Unlock()
		dispatchKeyFrame()
	}()

	attemptSync := func() (tryAgain bool) {
		for i := range peerConnections {
			if peerConnections[i].peerConnection.ConnectionState() == webrtc.PeerConnectionStateClosed {
				peerConnections = append(peerConnections[:i], peerConnections[i+1:]...)

				return true
			}

			peer := &peerConnections[i]

			desiredTrackCount := 0
			for _, trackLocal := range trackLocals {
				if peer.acceptsTrack(trackLocal) {
					desiredTrackCount++
				}
			}
			if !peer.shouldSignalWithDesiredTrackCount(desiredTrackCount) {
				continue
			}

			existingSenders := map[string]bool{}

			for _, sender := range peer.peerConnection.GetSenders() {
				if sender.Track() == nil {
					continue
				}

				trackID := sender.Track().ID()
				existingSenders[trackID] = true

				trackLocal, ok := trackLocals[trackID]
				if !ok || !peer.acceptsTrack(trackLocal) {
					if err := peer.peerConnection.RemoveTrack(sender); err != nil {
						return true
					}
				}
			}

			for _, receiver := range peer.peerConnection.GetReceivers() {
				if receiver.Track() == nil {
					continue
				}

				existingSenders[receiver.Track().ID()] = true
			}

			for trackID, trackLocal := range trackLocals {
				if !peer.acceptsTrack(trackLocal) {
					continue
				}

				if _, ok := existingSenders[trackID]; !ok {
					if _, err := peer.peerConnection.AddTrack(trackLocal); err != nil {
						return true
					}
				}
			}

			offer, err := peer.peerConnection.CreateOffer(nil)
			if err != nil {
				return true
			}

			var gatherComplete <-chan struct{}
			if peer.signal != nil {
				gatherComplete = webrtc.GatheringCompletePromise(peer.peerConnection)
			}

			if err = peer.peerConnection.SetLocalDescription(offer); err != nil {
				return true
			}

			if peer.signal != nil {
				if err = peer.signal(gatherComplete); err != nil {
					log.Errorf("Failed to signal peer: %v", err)
					return true
				}

				continue
			}

			offerString, err := json.Marshal(offer)
			if err != nil {
				log.Errorf("Failed to marshal offer to json: %v", err)

				return true
			}

			log.Infof("Send offer to client (redacted)")

			if err = peer.websocket.WriteJSON(&websocketMessage{
				Event: "offer",
				Data:  string(offerString),
			}); err != nil {
				return true
			}
		}

		return tryAgain
	}

	for syncAttempt := 0; ; syncAttempt++ {
		if syncAttempt == 25 {
			go func() {
				time.Sleep(time.Second * 3)
				signalPeerConnections()
			}()

			return
		}

		if !attemptSync() {
			break
		}
	}
}

func dispatchKeyFrame() {
	listLock.Lock()
	defer listLock.Unlock()

	for i := range peerConnections {
		for _, receiver := range peerConnections[i].peerConnection.GetReceivers() {
			if receiver.Track() == nil {
				continue
			}

			_ = peerConnections[i].peerConnection.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{
					MediaSSRC: uint32(receiver.Track().SSRC()),
				},
			})
		}
	}
}

func websocketHandler(w http.ResponseWriter, r *http.Request) { // nolint
	if !websocketLimiter.Allow(clientAddress(r)) {
		setSecurityHeaders(w)
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	authCtx, ok := authorizeRequest(r)
	if !ok {
		setSecurityHeaders(w)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	unsafeConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("Failed to upgrade HTTP to Websocket: %v", err)
		return
	}
	unsafeConn.SetReadLimit(maxWSReadBytes)

	c := &threadSafeWriter{unsafeConn, sync.Mutex{}} // nolint

	defer c.Close() //nolint

	if !registerWSClient(c, authCtx.BoardID) {
		log.Warnf("Rejecting WebSocket: max clients (%d) reached", maxWSClients)
		return
	}
	defer unregisterWSClient(c)

	if err := sendKanbanEvent(c, "board", sharedBoard.SnapshotState()); err != nil {
		log.Errorf("Failed to send Kanban board state: %v", err)
	}
	if err := sendKanbanEvent(c, "status", "Connected to conference room"); err != nil {
		log.Errorf("Failed to send Kanban status: %v", err)
	}

	if voiceProvider == "nova-sonic" {
		websocketHandlerNovaSonic(c)
		return
	}

	websocketHandlerOpenAI(c)
}

func websocketHandlerNovaSonic(c *threadSafeWriter) {
	message := &websocketMessage{}
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}

		if err := json.Unmarshal(raw, &message); err != nil {
			log.Errorf("Failed to unmarshal json to message: %v", err)
			return
		}

		switch message.Event {
		case "confirm_board":
			broadcastKanbanEvent("status", "Board confirmed by team")
		default:
			log.Infof("Nova Sonic WS: ignoring event %q", message.Event)
		}
	}
}

func websocketHandlerOpenAI(c *threadSafeWriter) {
	peerConnection, err := newBrowserPeerConnection()
	if err != nil {
		log.Errorf("Failed to creates a PeerConnection: %v", err)
		return
	}

	defer peerConnection.Close() //nolint

	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := peerConnection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			log.Errorf("Failed to add transceiver: %v", err)
			return
		}
	}

	listLock.Lock()
	peerConnections = append(peerConnections, peerConnectionState{
		peerConnection: peerConnection,
		websocket:      c,
	})
	listLock.Unlock()

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}
		candidateString, err := json.Marshal(i.ToJSON())
		if err != nil {
			log.Errorf("Failed to marshal candidate to json: %v", err)
			return
		}

		log.Infof("Send candidate to client (redacted)")

		if writeErr := c.WriteJSON(&websocketMessage{
			Event: "candidate",
			Data:  string(candidateString),
		}); writeErr != nil {
			log.Errorf("Failed to write JSON: %v", writeErr)
		}
	})

	peerConnection.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		log.Infof("Connection state change: %s", p)

		switch p {
		case webrtc.PeerConnectionStateFailed:
			if err := peerConnection.Close(); err != nil {
				log.Errorf("Failed to close PeerConnection: %v", err)
			}
		case webrtc.PeerConnectionStateClosed:
			signalPeerConnections()
		default:
		}
	})

	peerConnection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Infof("Got remote track: Kind=%s, ID=%s, PayloadType=%d", t.Kind(), t.ID(), t.PayloadType())

		trackLocal := addTrack(t)
		defer removeTrack(trackLocal)

		audioDecoder, audioChannels, err := newRoomAudioDecoder(t)
		if err != nil {
			log.Errorf("Failed to create audio decoder for track=%s: %v", t.ID(), err)
		}
		audioTrackKey := roomAudioTrackKey(t)
		if audioDecoder != nil {
			defer roomMixer.removeTrack(audioTrackKey)
		}
		audioDecodeBuffer := make([]int16, roomAudioDecodeBufferSize(audioChannels))

		for {
			packet, _, err := t.ReadRTP()
			if err != nil {
				return
			}

			if audioDecoder != nil {
				pcm, decodeErr := decodeOpusToRoomPCM(audioDecoder, audioDecodeBuffer, audioChannels, packet.Payload)
				if decodeErr != nil {
					log.Errorf("Failed to decode room audio for track=%s: %v", t.ID(), decodeErr)
				} else {
					roomMixer.submit(audioTrackKey, pcm)
				}
			}

			packet.Extension = false
			packet.Extensions = nil

			if err = trackLocal.WriteRTP(packet); err != nil {
				return
			}
		}
	})

	peerConnection.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		log.Infof("ICE connection state changed: %s", is)
	})

	signalPeerConnections()

	message := &websocketMessage{}
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Errorf("Failed to read message: %v", err)
			return
		}

		log.Infof("Got message: event=%s", message.Event)

		if err := json.Unmarshal(raw, &message); err != nil {
			log.Errorf("Failed to unmarshal json to message: %v", err)
			return
		}

		switch message.Event {
		case "candidate":
			candidate := webrtc.ICECandidateInit{}
			if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
				log.Errorf("Failed to unmarshal json to candidate: %v", err)
				return
			}

			log.Infof("Got candidate (redacted)")

			if err := peerConnection.AddICECandidate(candidate); err != nil {
				log.Errorf("Failed to add ICE candidate: %v", err)
				return
			}
		case "answer":
			answer := webrtc.SessionDescription{}
			if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
				log.Errorf("Failed to unmarshal json to answer: %v", err)
				return
			}

			log.Infof("Got answer (redacted)")

			if err := peerConnection.SetRemoteDescription(answer); err != nil {
				log.Errorf("Failed to set remote description: %v", err)
				return
			}
		default:
			log.Errorf("unknown message: %+v", message)
		}
	}
}

// Helper to make Gorilla Websockets threadsafe.
type threadSafeWriter struct {
	*websocket.Conn
	sync.Mutex
}

func (t *threadSafeWriter) WriteJSON(v any) error {
	t.Lock()
	defer t.Unlock()

	return t.Conn.WriteJSON(v)
}

// --- Security helpers ---

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; img-src 'self' data: blob:; media-src 'self' blob:; frame-ancestors 'none'")
}

func makeOriginChecker(allowed string) func(r *http.Request) bool {
	if allowed == "" {
		return func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return u.Host == r.Host
		}
	}
	allowSet := map[string]bool{}
	for _, o := range strings.Split(allowed, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowSet[o] = true
		}
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return allowSet[origin]
	}
}
