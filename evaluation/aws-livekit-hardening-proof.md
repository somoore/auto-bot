# AWS LiveKit Hardening Proof

Self-hosted LiveKit should not be treated as production-ready until these checks have evidence attached. The runnable fixture checklist is `evaluation/fixtures/aws_livekit_hardening_proof_v1.json`.

## UDP/TURN

Prove all paths from real networks:

- Direct UDP media through the LiveKit NLB media port.
- TURN/UDP relay when direct UDP is blocked.
- TURN/TLS relay when only TCP/TLS egress is available.

Evidence to keep:

- `chrome://webrtc-internals` dump.
- Selected ICE candidate pair.
- NLB target health.
- LiveKit participant connection logs.
- 10-minute audio/video continuity notes.

## Reconnect

Prove recovery without duplicate side effects:

- Browser refresh rejoins with the same participant identity.
- Wi-Fi drop and restore recovers the meeting.
- Meeting memory survives reconnect.
- Jira mutation dedupe prevents repeated writes.

Evidence to keep:

- Browser console timestamps.
- App WebSocket reconnect logs.
- LiveKit leave/join logs.
- Meeting control center before/after snapshot.
- Audit log showing no duplicate mutation.

## CloudWatch Alarms

Alarms should exist and be forced at least once for:

- LiveKit task restarts.
- Unhealthy NLB targets.
- Redis saturation or connection failure.
- App LiveKit token failures.
- Bedrock/Nova stream restart or failure surge.
- Jira sync/write failures.

Each alarm needs an ARN, threshold, owner, runbook link, forced ALARM evidence, and recovery-to-OK evidence.

## LiveKit Cloud Switch

The Terraform bit flip should be proven before we need it:

1. Set `LIVEKIT_DEPLOYMENT_MODE=cloud`.
2. Set `LIVEKIT_CLOUD_URL` and LiveKit Cloud secrets.
3. Run Terragrunt plan.
4. Confirm self-hosted LiveKit ECS/NLB/Redis resources are skipped.
5. Deploy cloud mode in a test environment.
6. Run a two-participant 10-minute standup.

Pass criteria:

- Both participants hear the agent.
- Transcripts flow.
- Jira writes match fixture expectations.
- The app service remains unchanged except for LiveKit URL/secrets.
