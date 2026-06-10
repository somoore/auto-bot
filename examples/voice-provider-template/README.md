# Voice Provider Template

Use this template when adding a new full-duplex speech model or transport.

1. Copy `voice_provider.go.tmpl` into the package that owns the provider.
2. Implement session startup, health, event streaming, and cleanup.
3. Add a contract test with `internal/core/contracttest.VoiceProvider`.
4. Register the provider in `cmd/server/extensions.go`.
5. Update `/voice/providers` UI behavior if the provider needs setup controls.
6. Add eval fixtures for interruptions, overlap, silence, reconnect, and late join.

Voice providers should emit evidence. They should not directly perform Jira or GitHub actions.
