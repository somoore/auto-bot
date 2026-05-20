# Evaluation Harness

This directory contains repeatable validation scaffolding for meeting behavior, Jira action safety, autonomous agent runs, voice reliability, and AWS LiveKit hardening proof.

Run the fixture validation tests:

```bash
go test ./evaluation
```

The tests validate that the committed fixtures cover:

- Host/participant room access through a generated random meeting code.
- Meeting type setup and in-meeting switching across general, standup, 1:1, sprint review, and open-ended modes.
- 2-4 participant meeting behavior: interruption, overlapping speech, silence, reconnect, and late join.
- Risky Jira confirmation behavior.
- Prompt-injection no-op behavior.
- Owner, ETA, and blocker extraction.
- Executive recap expectations.
- Post-meeting intelligence expectations: Jira change summary, blockers, action items by owner, unresolved questions, setup/observability signals, and transcript evidence.
- Agent-run visibility expectations: PM classification, default Bedrock Haiku/Sonnet model use, optional Opus escalation, GitHub App setup readiness, code-review findings, Jira write-back, optional PR review comment state, and checkpoint trail.
- Voice reliability signals and user-facing failure states.
- AWS LiveKit hardening proof areas for UDP/TURN, reconnect, CloudWatch alarms, and LiveKit Cloud switching.
- Extension-contract expectations for voice providers, connectors, model providers, action replay, and import-boundary gates.
- Golden-demo validation is real-stack only; use `scripts/validate-golden-demo.sh` and `scripts/livekit-multiperson-proof.sh` with configured AWS/Jira/GitHub/LiveKit credentials.

Captured live or simulated agent outputs can be graded against the same fixtures by writing one JSON result file per fixture and running:

```bash
AUTO_BOT_EVAL_RESULTS_DIR=/absolute/path/to/results go test ./evaluation
```

Each result file should use the shape shown by:

```bash
go test ./evaluation -run Example
```

Do not put secrets, raw Jira tokens, or raw meeting audio containing private content in this directory. Use scrubbed transcripts, generated synthetic audio, or metadata manifests unless a test explicitly needs an encrypted/private artifact.
