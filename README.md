# Realtime Meeting Assistant Demo

[![MIT License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
![Go](https://img.shields.io/badge/Built_with-Go-blue)
![WebRTC](https://img.shields.io/badge/Uses-WebRTC-blueviolet)
![OpenAI API](https://img.shields.io/badge/Powered_by-OpenAI_API-orange)

This demo showcases how to use the [OpenAI Realtime API](https://platform.openai.com/docs/guides/realtime) to interact through voice with a Kanban board during a standup. Multiple users can join the same WebRTC room and update the shared board with natural voice.

It is implemented as a Go application using Pion WebRTC, Gorilla WebSocket, Opus audio encoding/decoding, and the [Realtime + WebRTC integration](https://developers.openai.com/api/docs/guides/realtime-webrtc/). The server mixes participant audio, sends it to an OpenAI Realtime peer, and uses [function calling](https://developers.openai.com/api/docs/guides/realtime-conversations/) to trigger board updates.

![screenshot](./public/screenshot.png)

> [!IMPORTANT]
> This demo does not include built-in authentication or access control. While the server is running, anyone who can reach the app URL can join and access the meeting room.

## Quickstart (macOS, voice replies on)

```bash
# 1. Prereqs
brew install go opus pkg-config

# 2. Clone
git clone <this-repo-url> && cd openai-realtime-meeting-assistant

# 3. Run with voice replies + macOS-friendly ICE
CONFERENCE_LOOPBACK_ONLY=1 \
OPENAI_API_KEY=sk-... \
go run .
```

Open [http://localhost:3000](http://localhost:3000), click **Join room**, and start talking. The assistant moves cards on the Kanban board and speaks one-line confirmations through your speakers.

> Use headphones — when voice replies are on, the assistant's audio can bleed into the room mic and trigger spurious board updates.

## How to use

### Running the application

1. **Set up the OpenAI API:**

   - If you're new to the OpenAI API, [sign up for an account](https://platform.openai.com/signup).
   - Follow the [Quickstart](https://platform.openai.com/docs/quickstart) to retrieve your API key.

2. **Clone the Repository:**

   ```bash
   git clone <this-repo-url>
   ```

3. **Set your API key:**

   Export `OPENAI_API_KEY` in the shell where you start the server:

   ```bash
   export OPENAI_API_KEY=<your_api_key>
   ```

   The server reads environment variables directly. A `.env` file is not loaded automatically.

4. **Install dependencies:**

   You need Go 1.24 or newer and the Opus library available through `pkg-config`.

   ```bash
   brew install opus pkg-config
   ```

5. **Run the app:**

   ```bash
   CONFERENCE_LOOPBACK_ONLY=1 go run .
   ```

   The app will be available at [http://localhost:3000](http://localhost:3000).

   To use another port:

   ```bash
   CONFERENCE_LOOPBACK_ONLY=1 go run . -addr :8080
   ```

### Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `OPENAI_API_KEY` | _(required)_ | Auth for the OpenAI Realtime API. Without it the UI loads but the assistant is disabled. |
| `OPENAI_REALTIME_MODEL` | `gpt-realtime-2` | Realtime model to use. |
| `CONFERENCE_LOOPBACK_ONLY` | _unset_ | When `1`, the browser-facing WebRTC peer only gathers ICE candidates on `lo0`. Required on macOS when the browser and server run on the same machine — see [macOS Local Network note](#macos-local-network-permission). |
| `PION_NAT1TO1_IP` | _unset_ | Advertise this IP as the server's host ICE candidate (set when deploying behind a 1:1 NAT). |
| `PION_LOG_INFO` | _unset_ | Set to `all` for verbose Pion WebRTC logs while debugging. |

### macOS Local Network permission

On the same Mac, the Pion ICE agent will otherwise bind to the host's LAN IP (e.g. `192.168.x.y`) and try to reach the browser at its LAN IP. macOS Local Network privacy and the application firewall reject those LAN→LAN UDP sends with `sendto: broken pipe`, the conference WebRTC peer fails ICE, and the browser kicks itself out of the room.

Setting `CONFERENCE_LOOPBACK_ONLY=1` restricts the browser-facing peer to the loopback interface, which sidesteps the OS restriction. The OpenAI Realtime peer is **not** loopback-restricted — it still uses public network for STUN.

If you deploy the server to a host other than the client's machine, leave `CONFERENCE_LOOPBACK_ONLY` unset.

### Start a session

When the server starts, it creates the OpenAI Realtime peer if `OPENAI_API_KEY` is configured. If the key is missing or the Realtime connection fails, the browser room still loads, but the Kanban assistant will not listen or update cards.

1. Open [http://localhost:3000](http://localhost:3000).
2. Click **Join room**.
3. Allow camera and microphone access.
4. Speak naturally about the work on the board. The mixed room audio is sent to the Realtime assistant, and board changes are broadcast to everyone in the room.
5. Open the same URL in another browser tab or on another device to join as another participant.
6. Click **Leave** to disconnect that browser from the room and stop its local media tracks.

## Demo flow

**Use headphones or keep speaker volume low to avoid echo. Background audio can be picked up by the meeting mix and interpreted as board updates.**

The demo starts with a few WebRTC-related Kanban cards in the Backlog column. Try saying:

1. "I started the ICE restart handling ticket."
2. "The DTLS cleanup work is blocked on a transport shutdown issue."
3. "We shipped the RTP HEVC packetizer."
4. "Create a ticket to add subscription controls for simulcast forwarding."
5. "Add the bandwidth tag to the simulcast card."
6. "Delete the packet retransmission buffer ticket."

The board should update in place. Card moves animate, completed work triggers confetti, and note updates can show a short comment preview.

### Configured interactions

The assistant is configured as a voice-operated Kanban board operator. It can:

- Create tickets from explicit requests or concrete standup updates.
- Move existing tickets between **Backlog**, **In Progress**, **Blocked**, and **Done**.
- Add tags without replacing existing tags.
- Update ticket titles or notes when follow-up context arrives.
- Delete tickets by request.
- Speak a one-sentence confirmation after each board operation.
- Ignore filler, handoffs, or wrap-up phrases (stays silent on `do_nothing`).

For more details about the instructions and tools used by the model, see `kanban.go`.

### Voice replies

The assistant speaks back through your speakers/headphones. The server:

1. Mixes participant audio and forwards it to the OpenAI Realtime peer over a **sendrecv** Opus track.
2. Receives the assistant's voice on the same m-line via `OnTrack` and fans it out to every browser participant through the global track fanout used for participant audio.
3. The browser plays incoming audio tracks on hidden `<audio autoplay>` elements.

Session config in `kanban.go`:

- `output_modalities: ["audio"]` — voice output (the model does not also emit a text reply on the data channel in this mode).
- `tool_choice: "auto"` — lets the model both call a tool and speak; the instructions tell it to acknowledge each board operation with a one-line sentence.

To turn voice replies **off** and go back to silent tool-only behavior, set `output_modalities: ["text"]` and `tool_choice: "required"` in `sessionConfig`.

## Customization

You can update:

- The initial cards in `initialKanbanBoardCards` in `kanban.go`.
- The Realtime instructions in `sessionInstructions` in `kanban.go`.
- The tools exposed to the model in `kanbanTools` in `kanban.go`.
- The default Realtime model by setting `OPENAI_REALTIME_MODEL`; otherwise the app uses `gpt-realtime-2`.
- The browser UI in `index.html`.
- The HTTP bind address with the `-addr` flag in `main.go`.

## License

This project is licensed under the MIT License. See the LICENSE file for details.
