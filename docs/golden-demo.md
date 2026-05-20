# Golden Demo Path

The demo that proves the product should stay narrow and real:

1. Host starts a meeting.
2. Participants join with the generated code.
3. Host says: "Create a Jira task to review PR 2 for security and assign it to the agent."
4. Agent creates or selects the Jira issue.
5. Agent requests confirmation where risk requires it.
6. Bedrock project-manager agent classifies the task.
7. Bedrock review specialist reads GitHub PR context through the GitHub App.
8. Findings are written back to Jira and optionally to the PR.
9. The meeting UI shows API-confirmed mutations and warnings for any failed publish.
10. Audit replay shows: speech evidence -> selected tool -> external API result -> user-visible statement.

## Required Setup

- Local or AWS stack running with real Jira sync.
- AWS credentials in `us-east-1`.
- GitHub App installed only on the target repo.
- `GITHUB_DEFAULT_REPO` and allowed repo settings configured.
- Jira token with issue read/write and user-read scopes.
- LiveKit self-hosted or cloud provider configured.

## Preflight

Run:

```bash
AUTO_BOT_BASE_URL=http://localhost:3001 \
AUTO_BOT_ACCESS_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/app-api-token "$USER")" \
scripts/validate-golden-demo.sh
```

The script checks real server endpoints and setup readiness. It does not use mock providers.

## Pass Criteria

- Host and at least one participant can join.
- Voice dashboard shows mic, LiveKit, Nova Sonic, Bedrock, participant audio, paced agent audio, transcription, Jira, and agent participant health.
- Jira writes are only claimed after API confirmation.
- GitHub/Jira publish failures appear as warnings, not success claims.
- Audit replay includes speech evidence, tool call, confidence/risk, and API confirmation.
