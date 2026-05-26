// Package voice owns the voice-meeting runtime: session lifecycle, audio
// path, LiveKit integration, Nova Sonic + OpenAI Realtime providers, and
// the scrum tool dispatch into board mutations.
//
// Sprint 0 status: skeleton. cmd/server/nova_sonic*.go and
// scrum_tools.go remain in cmd/server during Sprint 0 and migrate in a
// later sprint after the board package is stable.
package voice
