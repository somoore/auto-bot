# auto-bot AWS Module

This module deploys the production-shaped runtime:

- ECS Fargate cluster.
- Go web app / voice agent service behind an Application Load Balancer.
- Self-hosted LiveKit service behind a Network Load Balancer with TCP signaling, TCP fallback, and a muxed UDP RTC media port.
- ECR repository for the app image.
- CloudWatch log groups.
- EFS-backed `/srv/data` mount for the app's SQLite board snapshot/event store.
- Task execution role with Secrets Manager access.
- App task role with Bedrock invoke permission.
- Secrets Manager wiring for OpenAI, LiveKit, Jira API token, Jira webhook secret, inline Jira sync config, and GitHub App credentials.
- Autonomous Jira/GitHub agents use AWS Bedrock only. The PM classifier defaults to Claude Haiku 4.5 and the code-review specialist defaults to Claude Sonnet 4.6 through Bedrock US inference-profile IDs and explicit Bedrock IAM resources, not the Anthropic API. Opus should be configured only for escalation-grade reviews.
- Production runtime variables for room/board authorization and durable board state.

The module is production-shaped and should not set `APP_LOCAL_LOGIN_TOKEN`; the local Keychain one-click login path is intentionally excluded from AWS.

The module is consumed through Terragrunt from `infra/live/dev`. Do not add backend or provider blocks here; the root `infra/terragrunt.hcl` generates them.
