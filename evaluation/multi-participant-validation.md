# Multi-Participant Meeting Validation

Use this plan to prove the meeting agent works like a scrum master before relying on it in a real team meeting.

## Room Access

1. Host creates a meeting and receives a generated random code.
2. Participant attempts to join with the wrong code.
3. Expected: participant is rejected before any LiveKit token is minted.
4. Participant joins with the generated code.
5. Expected: participant enters the meeting, the agent sees the participant, and the control center tracks the participant by identity.
6. Host regenerates the code.
7. Expected: the old code no longer works for new joins, existing authorized participants remain connected unless explicitly removed.

The generated code should be scoped to one meeting, have at least 72 bits of entropy, expire when the meeting ends, and never be logged with secrets.

## Meeting Modes

Validate that the host can create and switch among these modes:

- General meeting
- Standup
- 1:1
- Sprint review
- Open-ended

During a live session, the host should be able to say things like:

- "Switch this into a standup."
- "Make this a one on one with Avery."
- "Switch to sprint review mode."
- "Keep the rest open-ended."

Expected: the agent updates facilitation style, agenda pressure, recap format, and turn-taking behavior without losing meeting memory.

## Four-Person Standup Drill

Use `evaluation/fixtures/daily_standup_multi_participant_v1.json` as the transcript oracle and `evaluation/fixtures/multi_participant_audio_manifest_v1.json` as the synthetic audio timing oracle.

Participants:

- Scott Moore, host
- Sarah Lee, engineer
- Devon Patel, engineer
- Priya Shah, product manager

Required events:

- Sarah gives an EMAL-14 blocker, owner, and ETA.
- Devon interrupts and corrects the owner/ETA.
- Sarah and Devon overlap.
- The room is silent for at least 10 seconds.
- Devon disconnects and reconnects.
- Priya joins late and does not give a status update.
- Scott asks to close EMAL-12.

Expected:

- Control center shows who has spoken and who has not.
- EMAL-14 blocker is captured.
- Devon owns the EMAL-14 follow-up.
- EMAL-12 close request remains pending confirmation.
- No duplicate Jira mutation occurs after reconnect.
- End recap includes Jira changes, blockers, action items by owner, unresolved questions, and changes since meeting start.

## Pass Criteria

The run passes only when:

- All participants hear the agent.
- Transcription flows for every speaker who talks.
- Overlap does not create a destructive Jira action.
- Silence does not disconnect the agent.
- Reconnect preserves meeting memory.
- Late join updates the participant roster.
- Risky Jira actions require confirmation.
- The recap is usable as a Slack update without manual cleanup.
