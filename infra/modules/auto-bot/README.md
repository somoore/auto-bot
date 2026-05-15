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
- Secrets Manager wiring for OpenAI, LiveKit, Jira API token, Jira webhook secret, and inline Jira sync config.
- Production runtime variables for room/board authorization and durable board state.

The module is consumed through Terragrunt from `infra/live/dev`. Do not add backend or provider blocks here; the root `infra/terragrunt.hcl` generates them.
