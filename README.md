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

## How to use

### Running the application

1. **Set up the OpenAI API:**

   - If you're new to the OpenAI API, [sign up for an account](https://platform.openai.com/signup).
   - Follow the [Quickstart](https://platform.openai.com/docs/quickstart) to retrieve your API key.

2. **Clone the Repository:**

   ```bash
   git clone https://github.com/openai/openai-realtime-meeting-assistant.git
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
   go run .
   ```

   The app will be available at [http://localhost:3000](http://localhost:3000).

   To use another port:

   ```bash
   go run . -addr :8080
   ```

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
- Ignore filler, handoffs, or wrap-up phrases when no board operation is needed.

For more details about the instructions and tools used by the model, see `kanban.go`.

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
