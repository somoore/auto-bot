# Extension Contracts

This document is for plugin authors. It locks down the seven interfaces a third party can implement to extend auto-bot, the directory each implementation belongs in, where to register it, where the contract test lives, and a minimal working example.

The public surface lives in three packages:

| Package | Contracts owned |
| --- | --- |
| `internal/core` | `Connector`, `VoiceProvider`, `ModelProvider`, `ActionLedger` |
| `internal/agent` | `RunCoordinator`, `RunStore` |
| `internal/projection` | `Projection` (outbound writes + reconciliation) |
| `internal/mcp` | `BoardClient` (for connecting `cmd/mcpd` to a non-cmd-server board) |

Two cross-cutting rules apply to every contract:

1. **Untrusted external text is data, not instructions.** Returned strings from external systems must not be fed verbatim into a model prompt — `cmd/server/guardrails.go` strips and quotes external text. Implementations should not reach around the guardrail.
2. **Stable lowercase machine names.** Every contract that participates in a registry (`core.ConnectorRegistry`, `core.VoiceRegistry`, `core.ModelRegistry`, `projection.Registry`) lowercases and trims the `Name()` return; duplicates are rejected at registration. See `internal/core/types.go:412` (`normalizeRegistryName`) and `internal/projection/registry.go:74`.

---

## 1. `core.Connector`

**Purpose.** A connector represents read or write access to one external system: Slack outbound, a webhook receiver for a non-Jira ticketing system, an internal-tool bridge, a custom audit sink. For *bidirectional sync* with a workflow system (Jira, Linear, GitHub Issues), prefer `projection.Projection` (see §2) — it has a richer reconciliation surface. Connector is for stateless or unidirectional surfaces.

**Interface** (`internal/core/types.go:118-131`):

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

Method-by-method:

- **`Name()`** — stable lowercase machine name used for registration and routing. Must be deterministic across process restarts; the registry rejects duplicates (`internal/core/types.go:155-158`).
- **`DisplayName()`** — human-readable name for the operator UI.
- **`Capabilities()`** — declares the actions this connector supports as `[]ConnectorCapability` (`internal/core/types.go:52-57`). Each entry carries a `Name`, `Description`, `Risk` (`low|medium|high`), and `SupportsUndo`. Risk is consumed by the confirmation gate in `cmd/server/board.go:315-326` — `medium`/`high` capabilities are queued for human confirmation unless the dispatcher sets `SkipConfirmation`.
- **`Health(ctx)`** — point-in-time readiness, returned as `ConnectorHealth{OK, Status, Message, CheckedAt, Details}`. `Status` must be non-empty; the operator UI uses it to paint the connector tile.
- **`Execute(ctx, action)`** — the only entry point that mutates external state. Return `ConnectorResult{OK: true, Status: "api_confirmed"}` only after the external API confirms the write. Return `OK: false` with a visible `Status` (`api_failed`, `not_configured`, `local_confirmed`, `not_undoable`) when execution did not happen. **Never return a fabricated receipt.**
- **`Undo(ctx, receipt)`** — reverse a previous successful action when `Capabilities().SupportsUndo` is true. The connector must use `receipt.ExternalID` and `receipt.UndoToken` — these were minted by the connector itself on the original `Execute` call.

**Where to put it.** Pick a leaf package under the connector's domain, e.g. `internal/integrations/slack/`. Do **not** import `cmd/server/*` — the import-boundary checker (`scripts/check-import-boundaries.sh`) will reject it.

**Where to register it.** Add a line to `setupExtensionRuntime` in `cmd/server/extensions.go:24-122`, alongside the existing `boardToolConnector`, `jiraConnectorDescriptor`, and `githubConnectorDescriptor` registrations (`cmd/server/extensions.go:107-110`).

**Where the contract test lives.** `internal/core/contracttest.Connector(t, connector, cases)` (`internal/core/contracttest/contracts.go:26-62`). It asserts:

- Name and DisplayName are non-empty.
- At least one capability is declared.
- `Health.Status` is non-empty.
- Each case's `Execute` returns the expected `OK` and a non-empty `Status`.

**Minimal working example.** Copy `examples/connector-template/connector.go.tmpl`, replace the name, wire the client, and run the contract test. The template is ~50 LOC; the core shape is:

```go
package myslack

import (
    "context"
    "time"

    "github.com/somoore/auto-bot/internal/core"
)

type Connector struct {
    webhookURL string
    client     *http.Client
}

func (c Connector) Name() string        { return "slack" }
func (c Connector) DisplayName() string { return "Slack Outbound" }

func (c Connector) Capabilities() []core.ConnectorCapability {
    return []core.ConnectorCapability{
        {Name: "post_message", Description: "Post a channel message.", Risk: core.RiskMedium, SupportsUndo: false},
    }
}

func (c Connector) Health(ctx context.Context) core.ConnectorHealth {
    return core.ConnectorHealth{OK: c.webhookURL != "", Status: "ready", CheckedAt: time.Now().UTC()}
}

func (c Connector) Execute(ctx context.Context, a core.ConnectorAction) (core.ConnectorResult, error) {
    // POST to c.webhookURL, return OK only after the API confirms.
    return core.ConnectorResult{OK: true, Status: "api_confirmed", Receipt: core.ActionReceipt{
        ID: a.IdempotencyKey, Connector: c.Name(), Undoable: false, At: time.Now().UTC(),
    }}, nil
}

func (c Connector) Undo(ctx context.Context, r core.ActionReceipt) (core.ConnectorResult, error) {
    return core.ConnectorResult{OK: false, Status: "not_undoable"}, nil
}
```

---

## 2. `projection.Projection`

**Purpose.** The modern way to ship bidirectional sync with a workflow system. Where `Connector.Execute` is a single one-shot mutation, `Projection.Project` accepts a `BoardDelta` (multiple changed + deleted cards) and pairs it with `Reconcile` (read everything back) and `ResolveConflict` (decide what wins on divergence). This is the contract Jira already implements; Linear, GitHub Issues, and Asana adapters should also implement it.

**Interface** (`internal/projection/projection.go:15-22`):

```go
type Projection interface {
    Name() string
    Capabilities() Capabilities
    Project(ctx context.Context, delta BoardDelta) error
    Reconcile(ctx context.Context) ([]board.Card, error)
    ResolveConflict(ctx context.Context, conflict Conflict) (Resolution, error)
    Health(ctx context.Context) Health
}
```

Method-by-method:

- **`Name()`** — lowercase machine name. `internal/projection/registry.go:74` normalizes it; duplicates rejected.
- **`Capabilities()`** — returns `Capabilities{SupportsCreate, SupportsUpdate, SupportsDelete, SupportsWebhook, BiDirectional}` (`internal/projection/projection.go:25-31`). The Jira projection returns all five true (`internal/projection/jira/projection.go:54-58`).
- **`Project(ctx, delta)`** — outbound write of one delta. `BoardDelta` (`:34-39`) is `{TenantID, BoardID, Changed []board.Card, Deleted []string}`. Implementations should be idempotent: re-projecting the same delta should not duplicate the external record. The Jira projection does this by treating an empty `card.ID` as create and a non-empty `card.ID` as update (`internal/projection/jira/projection.go:82-98`).
- **`Reconcile(ctx)`** — full read-back. Used at startup, on webhook signal, and on operator-triggered refresh. Returns the *canonical* board.Card shape (assignees as `Actor` with `Kind: human|agent`, statuses as `board.Status` constants). The cmd/server Jira client already returns these directly.
- **`ResolveConflict(ctx, conflict)`** — when local and remote disagree, choose a strategy. Must return one of `ResolutionKeepLocal`, `ResolutionKeepRemote`, `ResolutionMerge`, `ResolutionAskUser` (`internal/projection/projection.go:56-61`). `Merge` requires a non-nil `Merged` card. Jira returns `keep-remote` by default (`internal/projection/jira/projection.go:119-121`).
- **`Health(ctx)`** — point-in-time readiness. `Status: "not_configured"` until the client is wired; `"ready"` once it is.

**Where to put it.** `internal/projection/<system>/`. The Jira example lives at `internal/projection/jira/projection.go`. Define a narrow `Client` interface (mirror the existing one — `internal/projection/jira/projection.go:22-29` — listing exactly the methods the projection needs) and inject it; this keeps the projection testable without a live HTTP backend.

**Where to register it.** `setupExtensionRuntime` in `cmd/server/extensions.go:111-119`. Wire your HTTP client + `Config`, then call `runtime.projections.Register(yourpkg.NewProjection(client, config))`. The Jira example only registers when the syncer client is configured (`cmd/server/extensions.go:111`) so an unconfigured projection does not pollute the registry.

**Where the contract test lives.** `internal/projection/contracttest.RunProjectionContract(t, factory)` (`internal/projection/contracttest/projection.go:19-94`). It exercises:

- `Name` is non-blank and already-lowercase.
- `Capabilities` is callable.
- `Project(empty)` is a no-op (no error).
- `Project(delta)` accepts both changed and deleted.
- `Reconcile` returns `[]board.Card`.
- `ResolveConflict` returns a known strategy; `Merge` requires `Merged != nil`.
- `Health.Status` is non-blank.

Example: `internal/projection/jira/projection_test.go` runs this harness against a fake `Client`.

**Minimal working example.** A skeleton Linear projection in `internal/projection/linear/projection.go` (~30 LOC):

```go
package linear

import (
    "context"
    "time"

    "github.com/somoore/auto-bot/internal/board"
    "github.com/somoore/auto-bot/internal/projection"
)

type Client interface {
    SearchCards(ctx context.Context) ([]board.Card, error)
    UpsertCard(ctx context.Context, card board.Card) (string, error)
    DeleteCard(ctx context.Context, cardID string) error
}

type Projection struct{ client Client }

func New(client Client) *Projection                  { return &Projection{client: client} }
func (p *Projection) Name() string                   { return "linear" }
func (p *Projection) Capabilities() projection.Capabilities {
    return projection.Capabilities{SupportsCreate: true, SupportsUpdate: true, SupportsDelete: true, BiDirectional: true}
}
func (p *Projection) Project(ctx context.Context, d projection.BoardDelta) error {
    for _, c := range d.Changed {
        if _, err := p.client.UpsertCard(ctx, c); err != nil { return err }
    }
    for _, id := range d.Deleted {
        if err := p.client.DeleteCard(ctx, id); err != nil { return err }
    }
    return nil
}
func (p *Projection) Reconcile(ctx context.Context) ([]board.Card, error) {
    return p.client.SearchCards(ctx)
}
func (p *Projection) ResolveConflict(_ context.Context, _ projection.Conflict) (projection.Resolution, error) {
    return projection.Resolution{Strategy: projection.ResolutionKeepRemote}, nil
}
func (p *Projection) Health(_ context.Context) projection.Health {
    return projection.Health{OK: p.client != nil, Status: "ready", CheckedAt: time.Now().UTC()}
}
```

---

## 3. `core.ModelProvider`

**Purpose.** A model backend used outside the realtime voice path: PM classification, code review, post-meeting summary generation, intake free-text parsing. The current production provider is AWS Bedrock (`cmd/server/extensions.go:127`, `:379-408`). New providers (Anthropic API, OpenAI Chat Completions, a private Llama deployment) implement this contract.

**Interface** (`internal/core/types.go:347-357`):

```go
type ModelProvider interface {
    Name() string
    DisplayName() string
    Capabilities() ModelCapabilities
    Complete(context.Context, ModelRequest) (ModelResponse, error)
}
```

Method-by-method:

- **`Name()`** — lowercase machine name. Used to route a `ModelRequest` to one of several backends.
- **`Capabilities()`** — `ModelCapabilities{JSON, Streaming, Modalities, MaxInputHint}` (`internal/core/types.go:318-323`). `JSON: true` means `Complete` will honour `ModelRequest.Metadata["response_format"] = "json"` and return the raw bytes in `ModelResponse.JSON`. `Streaming: false` is the default; the realtime voice path uses `VoiceProvider`, not `ModelProvider`.
- **`Complete(ctx, request)`** — one round-trip. `ModelRequest` (`internal/core/types.go:327-334`) carries `ModelID`, `System`, `Prompt`, `MaxTokens`, `Temperature`, and free-form `Metadata`. `ModelResponse` (`internal/core/types.go:337-345`) carries `Text` and/or `JSON`, plus `Usage` for token accounting.

**Where to put it.** `internal/integrations/<provider>/model.go`. Sprint 0 reserved `internal/cost/` for per-meeting and per-run cost metering; once that lands, model providers report `Usage` and `internal/cost` aggregates it.

**Where to register it.** `cmd/server/extensions.go:123-128` — `registerAgentModelProvider(client)` is called from the orchestrator setup. A new provider can either replace the Bedrock registration or be registered alongside under a different `Name()`.

**Where the contract test lives.** No shared contract-test helper today — model providers are exercised through the consuming code (`cmd/server/agent_runs.go:422` for classification, `:474` for review). Add a unit test in your provider package that calls `Complete` against a stub HTTP client and asserts `ModelResponse.Text` / `ModelResponse.JSON` are populated.

**Minimal working example.** From `examples/model-provider-template/model_provider.go.tmpl`:

```go
package examplemodel

import (
    "context"

    "github.com/somoore/auto-bot/internal/core"
)

type Provider struct{ client *Client }

func (p Provider) Name() string        { return "example-model" }
func (p Provider) DisplayName() string { return "Example Model" }

func (p Provider) Capabilities() core.ModelCapabilities {
    return core.ModelCapabilities{JSON: true, Streaming: false, Modalities: []string{"text"}}
}

func (p Provider) Complete(ctx context.Context, req core.ModelRequest) (core.ModelResponse, error) {
    raw, err := p.client.Call(ctx, req.ModelID, req.System, req.Prompt, req.MaxTokens)
    if err != nil { return core.ModelResponse{}, err }
    return core.ModelResponse{JSON: raw, Text: string(raw), ModelID: req.ModelID}, nil
}
```

---

## 4. `core.VoiceProvider`

**Purpose.** A full-duplex speech backend that can carry a meeting. Today two are wired live: AWS Nova Sonic (`cmd/server/nova_sonic.go`) and OpenAI Realtime over WebRTC (`cmd/server/kanban.go`). New backends — Deepgram Voice, Google Speech, a self-hosted Whisper+TTS pipeline — implement this contract.

**Interface** (`internal/core/types.go:250-262`):

```go
type VoiceProvider interface {
    Name() string
    DisplayName() string
    Capabilities() VoiceCapabilities
    Health(context.Context) VoiceHealth
    StartSession(context.Context, VoiceSessionRequest) (VoiceSession, error)
}
```

Method-by-method:

- **`Name()`** — lowercase machine name. The host's voice-provider picker maps a UI choice onto this name (`cmd/server/voice_models.go`).
- **`Capabilities()`** — `VoiceCapabilities{FullDuplex, Transport, Modalities, SupportsBargeIn}` (`internal/core/types.go:196-203`). `Transport` is a free-form label (`"LiveKit"`, `"WebRTC"`, `"LiveKit Cloud"`). The host UI consumes this to render the readiness pill.
- **`Health(ctx)`** — point-in-time readiness, returned as `VoiceHealth{OK, Status, Message, CheckedAt, Details}`. `Status` is one of the publish-rate-known states such as `available` or `active` (see `cmd/server/extensions.go:177-207` for how the server's existing descriptors paint these).
- **`StartSession(ctx, req)`** — kick off a provider-owned session. Returns a `VoiceSession` (`internal/core/types.go:241-248`) with `ID()`, `Events() <-chan VoiceSessionEvent`, and `Close(ctx) error`. The session streams transcript/audio/status/error events; barge-in is allowed when `Capabilities().SupportsBargeIn` is true.

**Runtime caveat.** As of v2 Sprint 4, the cmd/server runtime still owns the concrete browser/media path: `voiceProvider` is a package-level switch (`cmd/server/main.go:281`), the LiveKit token mint lives in `cmd/server/main.go:323`, and the Nova Sonic / OpenAI Realtime session lifecycles live in their dedicated files. Registering a new `VoiceProvider` in `cmd/server/extensions.go:31-105` exposes health and capabilities to the operator UI, **but does not** wire the live media path. To carry a real meeting the new provider also needs:

1. A branch in `cmd/server/main.go` that hands the WebSocket / WebRTC upgrade off to its agent loop.
2. A model-option registration in `cmd/server/voice_models.go` for the host's UI picker.
3. UI setup affordance in `web/index_livekit.html` (or `web/app/` for the React tier).
4. An eval fixture in `evaluation/` that exercises one full transcript-to-tool-call.

The existing `serverVoiceProviderDescriptor` (`cmd/server/extensions.go:157-211`) declares its sessions are owned by the server runtime by returning a stub from `StartSession` — see `cmd/server/extensions.go:209-211`.

**Where to put it.** `internal/voice/<provider>/`. (Note: the `internal/voice/` directory exists as a reservation, see `internal/voice/doc.go` — the voice runtime moves there in a later sprint.)

**Where to register it.** `cmd/server/extensions.go:31-105` for the descriptor; the live wiring requires the four steps above.

**Where the contract test lives.** `internal/core/contracttest.VoiceProvider(t, provider, cases)` (`internal/core/contracttest/contracts.go:76-114`). It asserts non-empty Name, DisplayName, Transport, and Health.Status; for each case, `StartSession` returns a session with a non-empty `ID()` and `Close()` does not error.

**Minimal working example.** From `examples/voice-provider-template/voice_provider.go.tmpl`:

```go
package examplevoice

import (
    "context"
    "time"

    "github.com/somoore/auto-bot/internal/core"
)

type Provider struct{ client *Client }

func (p Provider) Name() string        { return "example-voice" }
func (p Provider) DisplayName() string { return "Example Voice" }

func (p Provider) Capabilities() core.VoiceCapabilities {
    return core.VoiceCapabilities{
        FullDuplex: true, Transport: "example",
        Modalities: []string{"audio", "transcript"}, SupportsBargeIn: true,
    }
}

func (p Provider) Health(ctx context.Context) core.VoiceHealth {
    return core.VoiceHealth{OK: p.client != nil, Status: "available", CheckedAt: time.Now().UTC()}
}

func (p Provider) StartSession(ctx context.Context, req core.VoiceSessionRequest) (core.VoiceSession, error) {
    return p.client.OpenSession(ctx, req)
}
```

---

## 5. `agent.RunCoordinator`

**Purpose.** A `RunCoordinator` drives the lifecycle of an autonomous agent Run: start, advance, pause for human input, resume, cancel. There are two production implementations: the in-process orchestrator in `cmd/server/agent_runs.go` + `cmd/server/agent_coordinator.go` (the legacy GitHub PR-review loop), and the reference `SimpleRunCoordinator` in `internal/agent/simple_coordinator.go` (used by `cmd/mcpd` and by tests). Future runtimes — a Claude Code adapter that runs Runs in-process, a Temporal-backed coordinator, a hosted agents API — implement this contract.

**Interface** (`internal/agent/coordinator.go:48-54`):

```go
type RunCoordinator interface {
    Start(ctx context.Context, req RunRequest) (Run, error)
    Checkpoint(ctx context.Context, runID string, cp RunStepCheckpoint) error
    AskHuman(ctx context.Context, runID string, q RunQuestion) (string, error)
    Resume(ctx context.Context, answer HumanAnswer) (Run, error)
    Cancel(ctx context.Context, runID string, reason string) error
}
```

Method-by-method (see the docstring at `internal/agent/coordinator.go:36-47`):

- **`Start(ctx, req)`** — mints a `RunID` (use `agent.NewRunID()` at `internal/agent/id.go:34`), persists the Run via `RunStore.SaveRun`, kicks off execution (typically async), returns the persisted Run so callers can immediately render it. **Must honour the kill switch**: when the tenant has `agents_paused` enabled, return `agent.ErrAgentsPaused` (`internal/agent/store.go:29-34`) without persisting the Run. The cmd/server orchestrator does this at `cmd/server/agent_coordinator.go:42-48`.
- **`Checkpoint(ctx, runID, cp)`** — append a `RunStepCheckpoint` (`internal/agent/checkpoint.go:21-26`) to the durable audit log via `RunStore.AppendRunCheckpoint`, then reflect the transition on `Run.Plan[step].Status`. The four kinds are `started`, `completed`, `paused`, `failed` (`:30-35`). On audit-log failure, wrap `agent.ErrCheckpointAuditFailed` (`internal/agent/store.go:36-44`) — the SE-1 F2 fix surfaces "did not persist" rather than silently dropping the entry.
- **`AskHuman(ctx, runID, q)`** — mint a `QuestionID` (use `agent.NewQuestionID()` at `internal/agent/id.go:24`), persist the question via `RunStore.SaveRunQuestion`, transition the Run to `StatusWaitingOnHuman` (`internal/agent/types.go:34`), and broadcast `run_question_asked` + `run_paused` so the UI / drawer can render the question. The cmd/server implementation broadcasts both at `cmd/server/agent_coordinator.go:185-187`.
- **`Resume(ctx, answer)`** — mark the question answered via `RunStore.MarkRunQuestionAnswered`, clear `Run.WaitingOn`, transition the Run back to a running status, broadcast `run_resumed` (`cmd/server/agent_coordinator.go:273`). When the question already expired, return `agent.ErrRunQuestionExpired` so the UI can show "the question timed out — restart the ask" (`internal/agent/store.go:20-27`).
- **`Cancel(ctx, runID, reason)`** — idempotent transition to `StatusCancelled`. Calling on an already-terminal Run is a no-op.

**Where to put it.** `internal/agent/` if your coordinator can be implementation-agnostic (like `SimpleRunCoordinator`). If it needs cmd/server-specific helpers, put it in `cmd/server/` and add a compile-time check: `var _ agent.RunCoordinator = (*YourCoordinator)(nil)`. The cmd/server orchestrator does this at `cmd/server/agent_coordinator.go:16`.

**Where to register it.** Coordinators are not stored in a registry — they are injected into the consumer. `cmd/mcpd/main.go:68` injects a coordinator into `mcp.ToolDeps`. The cmd/server orchestrator is wired in `cmd/server/agent_runs.go:71` (`setupAgentRunOrchestrator`).

**Where the contract test lives.** `internal/agent/coordinator_test.go` exercises `SimpleRunCoordinator` against `mocks.RunStore`; copy the cases for a new coordinator.

**Minimal working example.** A coordinator that proxies every call to a remote HTTP service (~30 LOC):

```go
package remoteagent

import (
    "context"
    "fmt"

    "github.com/somoore/auto-bot/internal/agent"
)

type Coordinator struct {
    httpClient *http.Client
    baseURL    string
}

var _ agent.RunCoordinator = (*Coordinator)(nil)

func (c *Coordinator) Start(ctx context.Context, req agent.RunRequest) (agent.Run, error) {
    // POST baseURL+"/runs", expect a Run JSON back.
}
func (c *Coordinator) Checkpoint(ctx context.Context, runID string, cp agent.RunStepCheckpoint) error {
    // POST baseURL+"/runs/"+runID+"/checkpoints".
}
func (c *Coordinator) AskHuman(ctx context.Context, runID string, q agent.RunQuestion) (string, error) {
    if q.ID == "" { q.ID = agent.NewQuestionID() }
    // POST baseURL+"/runs/"+runID+"/questions", return q.ID.
}
func (c *Coordinator) Resume(ctx context.Context, a agent.HumanAnswer) (agent.Run, error) {
    // POST baseURL+"/questions/"+a.QuestionID+"/answer".
}
func (c *Coordinator) Cancel(ctx context.Context, runID, reason string) error {
    // POST baseURL+"/runs/"+runID+"/cancel".
    return nil
}
```

---

## 6. `agent.RunStore`

**Purpose.** The persistence surface a `RunCoordinator` depends on. The production implementation is `*sqliteBoardStore` in `cmd/server/board_store.go` (which also implements `pendingActionStore`, `tenantSettingsStore`, and stores meeting reports). For tests, `internal/mocks/runstore.go` is the canonical in-memory implementation. Swap targets: Postgres for hosted multi-tenant, an in-memory store for a CLI tool, Redis for ephemeral coordinators.

**Interface** (`internal/agent/store.go:54-99`):

```go
type RunStore interface {
    SaveRun(ctx context.Context, tenantID, boardID string, run Run) error
    LoadRun(ctx context.Context, tenantID, boardID, runID string) (Run, error)
    AppendRunCheckpoint(ctx context.Context, tenantID, boardID, runID string, cp RunStepCheckpoint) error
    ListRunCheckpoints(ctx context.Context, tenantID, boardID, runID string) ([]RunStepCheckpoint, error)
    SaveRunQuestion(ctx context.Context, q RunQuestion) error
    LoadRunQuestion(ctx context.Context, tenantID, boardID, questionID string) (RunQuestion, error)
    ListOpenRunQuestions(ctx context.Context, tenantID, boardID string) ([]RunQuestion, error)
    MarkRunQuestionAnswered(ctx context.Context, tenantID, boardID, questionID, answer, answeredBy, answeredVia string) error
    ExpireRunQuestions(ctx context.Context, tenantID, boardID string, now time.Time) (int, error)
}
```

Method invariants:

- **`SaveRun`** — upserts by `(tenantID, boardID, run.RunID)`. Empty tenant IDs should normalize to `"default"` to preserve compatibility with the legacy single-tenant data model (`internal/agent/store.go:51-53`).
- **`LoadRun`** — return `ErrRunNotFound` (wrappable with `%w`) when the row is missing.
- **`AppendRunCheckpoint`** — multiple checkpoints per `step_index` are allowed. `RunStepCheckpoint.PayloadJSON` is opaque to the store; coordinators marshal it.
- **`ListRunCheckpoints`** — return chronological `(created_at, step_index)` order.
- **`SaveRunQuestion`** — upsert by `(tenantID, boardID, q.ID)`. The same question can transition `open` → `answered` / `expired`.
- **`LoadRunQuestion`** — return `ErrRunQuestionNotFound` when missing.
- **`ListOpenRunQuestions`** — oldest `asked_at` first. ULID-typed `q.ID` matches this order lexically (see `internal/agent/id.go:19-23`).
- **`MarkRunQuestionAnswered`** — error if the question is already terminal.
- **`ExpireRunQuestions`** — transition every open question past `asked_at + TTLSeconds` to `expired`, return the count. **Must be idempotent**: a second pass at the same `now` returns 0. The sweeper in `cmd/server/run_question_sweeper.go` calls this on a fixed cadence.

**Where to put it.** `internal/integrations/postgres/runstore.go` for a Postgres backend, or in your own package. Add a compile-time check: `var _ agent.RunStore = (*YourStore)(nil)` (the mock does this at `internal/mocks/runstore.go:44`).

**Where to register it.** RunStores are injected, not registered. `cmd/server/main.go` constructs `*sqliteBoardStore` and passes it through `setupAgentRunOrchestrator`. `cmd/mcpd/main.go:67` constructs a `mocks.NewRunStore()`.

**Where the contract test lives.** No shared harness yet — `cmd/server/agent_coordinator_persistence_test.go` is the closest example. A future `internal/agent/contracttest` is on the roadmap.

**Minimal working example.** Adapter shape for a different SQL dialect (~30 LOC, just the `SaveRun` method to illustrate):

```go
package pgrunstore

import (
    "context"
    "database/sql"
    "encoding/json"

    "github.com/somoore/auto-bot/internal/agent"
)

type Store struct{ db *sql.DB }

var _ agent.RunStore = (*Store)(nil)

func (s *Store) SaveRun(ctx context.Context, tenant, board string, run agent.Run) error {
    body, err := json.Marshal(run)
    if err != nil { return err }
    _, err = s.db.ExecContext(ctx,
        `INSERT INTO agent_runs (tenant_id, board_id, run_id, body)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (tenant_id, board_id, run_id) DO UPDATE SET body = EXCLUDED.body`,
        normalize(tenant), normalize(board), run.RunID, body,
    )
    return err
}

func normalize(s string) string { if s == "" { return "default" }; return s }
```

---

## 7. `mcp.BoardClient`

**Purpose.** The board-state surface the five MCP tools (`board.list_cards`, `board.get_card`, `card.create`, `card.update`, `card.comment`) depend on. Today there are two production paths: `mcp.HTTPBoardClient` (`internal/mcp/tools.go:449`) which posts back to cmd/server's `/internal/tools/dispatch` so MCP-driven writes flow through the same `ApplyToolCall` audit ledger, and `mocks.BoardClient` (`internal/mocks/boardclient.go`) which is in-memory. A third party implementing this gets to bring their own board — a hosted multi-tenant board in Sprint 5, a CLI-only board that lives in a JSON file, or a board backed by a different ticketing system entirely.

**Interface** (`internal/mcp/tools.go:32-38`):

```go
type BoardClient interface {
    ListCards(ctx context.Context, tenantID, boardID string, filter CardFilter) ([]board.Card, error)
    GetCard(ctx context.Context, tenantID, boardID, cardID string) (board.Card, error)
    CreateCard(ctx context.Context, tenantID, boardID string, input CardCreate) (board.Card, error)
    UpdateCard(ctx context.Context, tenantID, boardID, cardID string, patch CardPatch) (board.Card, error)
    AddComment(ctx context.Context, tenantID, boardID, cardID string, body, author string) (board.Comment, error)
}
```

Method-by-method:

- **`ListCards`** — `CardFilter` (`internal/mcp/tools.go:43-47`) carries `Status`, `AssigneeID`, and `AgentOnly`. Empty fields mean "no filter on this dimension". Return the slim view as `[]board.Card`; the protocol layer narrows it to `CardSummary` for `board.list_cards` callers (`:76-83`).
- **`GetCard`** — return `mcp.ErrCardNotFound` (`internal/mcp/tools.go:21`) when the card is missing. The protocol layer translates this into a human-readable error surfaced to the MCP client.
- **`CreateCard`** — `CardCreate` (`internal/mcp/tools.go:51-58`) maps `Description` onto `Card.Notes`. `Assignee *board.Actor` is optional — when present and `Kind: agent`, the implementation should kick off the agent assignment / RunCoordinator.Start flow itself, exactly like the voice path does.
- **`UpdateCard`** — `CardPatch` (`internal/mcp/tools.go:64-71`) uses pointer fields to distinguish "absent" from "explicit zero value". The `TagsSet bool` field disambiguates `nil Tags` (leave alone) from an explicit empty list (clear).
- **`AddComment`** — append a `board.Comment` to the card. `Author` is the MCP client's identity.

**Where to put it.** `internal/integrations/<your-board>/mcp_client.go`. Production deployments wire `mcp.HTTPBoardClient` to a hosted board in Sprint 5; until then, a fresh BoardClient that talks to a hosted board would live alongside the projection that syncs its data.

**Where to register it.** BoardClients are injected into `mcp.ToolDeps`, not registered. `cmd/mcpd/main.go:50-65` shows the two-path wiring (HTTP client when `BOARD_URL` is set, mock fallback otherwise). The injection happens at `cmd/mcpd/main.go:70-77`:

```go
tools := mcp.BuildTools(mcp.ToolDeps{
    Board:        client,
    RunStore:     runStore,
    Coordinator:  coordinator,
    TenantID:     *tenantID,
    BoardID:      *boardID,
    DefaultActor: "mcp",
})
```

**Where the contract test lives.** `internal/mcp/server_test.go` exercises the protocol layer + tool handlers against `mocks.BoardClient`. A new BoardClient implementation should run the same handlers against itself end-to-end.

**Minimal working example.** A JSON-file BoardClient (~40 LOC, key methods):

```go
package fileboard

import (
    "context"
    "encoding/json"
    "os"
    "sync"

    "github.com/somoore/auto-bot/internal/board"
    "github.com/somoore/auto-bot/internal/mcp"
)

type Client struct {
    mu   sync.Mutex
    path string
}

var _ mcp.BoardClient = (*Client)(nil)

func (c *Client) ListCards(ctx context.Context, tenant, b string, f mcp.CardFilter) ([]board.Card, error) {
    cards, err := c.read()
    if err != nil { return nil, err }
    if f.Status != "" || f.AssigneeID != "" || f.AgentOnly {
        cards = filter(cards, f)
    }
    return cards, nil
}

func (c *Client) GetCard(ctx context.Context, tenant, b, id string) (board.Card, error) {
    cards, err := c.read()
    if err != nil { return board.Card{}, err }
    for _, card := range cards {
        if card.ID == id { return card, nil }
    }
    return board.Card{}, mcp.ErrCardNotFound
}

func (c *Client) CreateCard(ctx context.Context, tenant, b string, in mcp.CardCreate) (board.Card, error) {
    c.mu.Lock(); defer c.mu.Unlock()
    cards, _ := c.read()
    card := board.Card{ID: mintID(), Title: in.Title, Notes: in.Description, Status: board.Status(in.Status)}
    cards = append(cards, card)
    return card, c.write(cards)
}
// UpdateCard, AddComment — omitted for brevity.
```

---

## ActionLedger (cross-cutting)

**`core.ActionLedger`** (`internal/core/ledger.go`) is consumed by every contract above — it is the audit-replay surface. Every externally-visible action should round-trip as:

1. The sentence or evidence that created intent (`RecordIntent`).
2. The policy / confidence / risk decision (logged on the intent).
3. The tool call and arguments (`RecordToolCall`).
4. The external API result (`RecordExternalConfirmation`).
5. The user-visible statement the agent was allowed to make (logged on the confirmation).

The cmd/server implementation lives in `cmd/server/audit.go`. A new connector or coordinator should not write its own audit log — it should call into the ActionLedger so the operator UI's replay view sees one timeline.

---

## Where to wire it all up

A complete plugin lands in three places:

1. **The implementation package** (`internal/integrations/<name>/`, `internal/projection/<name>/`, or a separate repo importing this module).
2. **The registration call** in `cmd/server/extensions.go` (or `cmd/mcpd/main.go` for MCP-only plugins).
3. **The eval fixture** in `evaluation/fixtures/` exercising one full happy path through the new contract.

The pre-commit hook (`scripts/pre-commit`) runs `scripts/check-import-boundaries.sh`, which enforces that `internal/*` packages never import `cmd/*`. Stay below that line and the runtime can host your extension without touching cmd/server.
