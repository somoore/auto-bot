# Extension Contracts

The public extension surface is in `internal/core`.

## Voice Provider

Use `core.VoiceProvider` for full-duplex speech systems:

```go
type VoiceProvider interface {
    Name() string
    DisplayName() string
    Capabilities() VoiceCapabilities
    Health(context.Context) VoiceHealth
    StartSession(context.Context, VoiceSessionRequest) (VoiceSession, error)
}
```

Provider responsibilities:

- Publish health that explains what is broken.
- Stream transcript/audio events with speaker evidence.
- Support barge-in if the backend can handle it.
- Avoid direct Jira/GitHub mutation.
- Declare model capability boundaries. For example, a full-duplex voice-to-action model can run the meeting agent with Jira/GitHub tools, while a transcription- or translation-only profile must be registered without Jira/GitHub write authority.

Current runtime limitation: the server still owns the concrete browser/media session startup path in `cmd/server/main.go`. Registering a provider in `cmd/server/extensions.go` exposes health and option metadata, but a new live provider also needs runtime selection, token/WebSocket behavior, model options, UI setup controls, and eval fixtures before it can carry a real meeting.

## Connector

Use `core.Connector` for external systems:

```go
type Connector interface {
    Name() string
    DisplayName() string
    Capabilities() []ConnectorCapability
    Health(context.Context) ConnectorHealth
    Execute(context.Context, ConnectorAction) (ConnectorResult, error)
    Undo(context.Context, ActionReceipt) (ConnectorResult, error)
}
```

Connector responsibilities:

- Declare risk per capability.
- Return receipts for successful external writes.
- Return visible failure statuses when external APIs reject a write.
- Keep untrusted external text as data only.

## Model Provider

Use `core.ModelProvider` for model backends:

```go
type ModelProvider interface {
    Name() string
    DisplayName() string
    Capabilities() ModelCapabilities
    Complete(context.Context, ModelRequest) (ModelResponse, error)
}
```

The current production path is AWS Bedrock.

## Action Ledger

Use `core.ActionLedger` to preserve the trust story:

```go
type ActionLedger interface {
    RecordIntent(context.Context, ActionIntent) (ActionIntent, error)
    RecordToolCall(context.Context, ToolCallRecord) (ToolCallRecord, error)
    RecordExternalConfirmation(context.Context, ExternalConfirmation) (ExternalConfirmation, error)
    Replay(context.Context, string) (ActionReplay, error)
}
```

Every externally visible action should be replayable as:

1. The sentence or evidence that created intent.
2. The policy/confidence/risk decision.
3. The tool call and arguments.
4. The external API result.
5. The user-visible statement the agent was allowed to make.
