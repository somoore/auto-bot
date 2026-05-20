# AWS Infrastructure

This directory uses Terragrunt to keep Terraform DRY across environments.

## Layout

```text
infra/
  terragrunt.hcl              Root remote-state/provider generation
  env.hcl                     Shared dev inputs
  live/dev/terragrunt.hcl     Dev environment wrapper
  modules/auto-bot/           Reusable Terraform module
```

## State

The root Terragrunt config generates an S3 backend in every live module:

- Region: `us-east-1`
- State bucket: `auto-bot-terraform-state-<aws-account-id>`
- DynamoDB lock table: `auto-bot-terraform-locks`

Terragrunt can create the S3 bucket and DynamoDB table during init when your AWS identity has permission.

## Provider

The root Terragrunt config pins Terraform CLI `1.15.2`, generates the provider block, and pins provider releases:

```hcl
hashicorp/aws = 6.45.0
hashicorp/cloudinit = 2.4.0
```

The version was verified against the HashiCorp releases index on 2026-05-15.
Provider checksums are committed in `live/dev/.terraform.lock.hcl`.

## Network And Security Shape

- Region: `us-east-1`
- VPC CIDR: `10.20.0.0/16`, the AWS-canonical form of the requested `10.20.21.0/16`
- Public subnets: app ALB, optional self-hosted LiveKit NLB, and fck-nat only, starting at `10.20.21.0/24`
- Private subnets: ECS app task, optional ECS LiveKit tasks, EFS mount targets, ElastiCache Redis, and interface VPC endpoints
- Egress: private subnet default routes point at fck-nat, not AWS NAT Gateway
- App edge: AWS WAF is attached to the ALB with AWS managed rule groups and a rate limit
- LiveKit: `LIVEKIT_DEPLOYMENT_MODE=self-hosted` deploys private ECS LiveKit tasks, NLB listeners, ElastiCache Redis distributed routing, embedded TURN/UDP, optional TURN/TLS, and metrics. `LIVEKIT_DEPLOYMENT_MODE=cloud` skips the self-hosted media plane and points the app at `LIVEKIT_CLOUD_URL`.
- Secrets: app token, LiveKit API key/secret, self-hosted `LIVEKIT_KEYS`, optional custom LiveKit config, Jira token/config, OpenAI key, and GitHub App agent credentials are injected from AWS Secrets Manager
- IAM: ECS execution and task policies are inline/resource-scoped; Bedrock is narrowed to the configured model ARNs

## Dev Deploy Flow

1. Authenticate to AWS in `us-east-1`.
2. Pin the fck-nat AMI ID for `us-east-1`.

   ```bash
   aws ec2 describe-images \
     --region us-east-1 \
     --owners 568608671756 \
     --filters 'Name=name,Values=fck-nat-al2023-hvm-*' 'Name=architecture,Values=arm64' \
     --query 'sort_by(Images,&CreationDate)[-1].{ImageId:ImageId,Name:Name,CreationDate:CreationDate}' \
     --output table

   export FCK_NAT_AMI_ID=ami-xxxxxxxxxxxxxxxxx
   ```

   Review the returned AMI name/date before exporting the ID. The Terraform module passes this exact AMI ID and does not use fck-nat's latest AMI lookup.

3. Create/update secret values:

   ```bash
   # Optional self-hosted DNS/TLS.
   export HOSTED_ZONE_ID=Z123...
   export LIVEKIT_DOMAIN_NAME=livekit.example.com
   export LIVEKIT_TURN_DOMAIN_NAME=turn.example.com
   export LIVEKIT_CERTIFICATE_ARN=arn:aws:acm:us-east-1:...

   # Optional bit flip to use LiveKit Cloud instead of self-hosted ECS LiveKit.
   # export LIVEKIT_DEPLOYMENT_MODE=cloud
   # export LIVEKIT_CLOUD_URL=wss://your-project.livekit.cloud

   AWS_REGION=us-east-1 ./scripts/aws-upsert-secrets.sh
   set -a; source .env.aws.local; set +a
   ```

   To enable Jira in ECS, set `JIRA_API_TOKEN` and either `JIRA_CONFIG_JSON` or `JIRA_CONFIG_JSON_FILE` before running the script. The uploaded Jira config should use `"api_token_env": "JIRA_API_TOKEN"` instead of a local token file path. Set `JIRA_WEBHOOK_SECRET` as well if Jira will call `POST /jira/webhook`.
   To enable autonomous code-review agents, set `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY`, `GITHUB_DEFAULT_REPO`, and `GITHUB_ALLOWED_REPOS` before running the script. The GitHub App should be installed only on the target repo with `Contents: read` and `Pull requests: read`; set `GITHUB_PR_COMMENTS_ENABLED=true` only after granting `Pull requests: write`. Agent Claude models use Bedrock US inference-profile IDs such as `us.anthropic.claude-haiku-4-5-20251001-v1:0`.
   To enable TURN/TLS, set `LIVEKIT_TURN_CERTIFICATE_ARN`, set `LIVEKIT_TURN_DOMAIN_NAME`, and set the Terraform input `livekit_turn_tls_enabled = true` in `infra/env.hcl` or the live environment inputs. The ACM certificate must match the TURN domain. The dev wrapper does not currently wire a `LIVEKIT_TURN_TLS_ENABLED` environment variable.

4. Bootstrap remote state and ECR:

   ```bash
   cd infra/live/dev
   terragrunt init
   terragrunt apply -target=aws_ecr_repository.app
   cd ../../..
   ```

5. Build and push the app image:

   ```bash
   set -a; source .env.aws.local; set +a
   ./scripts/aws-build-push.sh
   ```

   The script tags the image with the current git SHA by default and updates `.env.aws.local` with `APP_IMAGE`. Full Terraform deploys intentionally fail if `APP_IMAGE` is missing or uses a moving tag.

6. Deploy the full stack:

   ```bash
   ./scripts/aws-deploy-dev.sh
   ```

7. Read outputs:

   ```bash
   cd infra/live/dev
   terragrunt output
   ```

## Notes

- The app runs behind an ALB protected by AWS WAF.
- Self-hosted LiveKit runs behind an internet-facing NLB with TCP/TLS signaling, TCP fallback, one muxed UDP RTC media port, embedded TURN/UDP, optional TURN/TLS, Redis distributed routing, and Prometheus metrics. WAF does not protect NLB UDP/TCP media traffic, so keep `allowed_ingress_cidrs` as narrow as your test audience allows.
- LiveKit Cloud mode is a Terraform input switch: set `LIVEKIT_DEPLOYMENT_MODE=cloud`, `LIVEKIT_CLOUD_URL`, and the LiveKit Cloud API key/secret before running `aws-upsert-secrets.sh`.
- ElastiCache Redis is private, encrypted at rest, uses in-transit TLS, and is only reachable from the LiveKit task security group.
- A CloudWatch dashboard is created for ECS, ALB/WAF, NLB, and Redis signals.
- ECS tasks and EFS mount targets run in private subnets with `assign_public_ip = false`.
- The fck-nat module version is pinned to `1.4.0`, and full deploys require an explicit `FCK_NAT_AMI_ID`.
- The app runs with `APP_ENV=production`, token-backed HttpOnly browser sessions, and `APP_ROOM_ID` / `APP_BOARD_ID` authorization for this deployment.
- Do not set `APP_LOCAL_LOGIN_TOKEN` in AWS. The Keychain-backed `/auth/local-login` path is local-only and production startup rejects that variable.
- The app mounts EFS at `/srv/data` and uses `BOARD_SQLITE_PATH=/srv/data/board.sqlite` for board snapshots and event history.
- Jira sync is injected through Secrets Manager. The app supports the same advanced Jira config used locally: project-key safety, status mappings, transition IDs, blocked flag fallback, story points field, sprint field, epic link field, rank custom field ID, named custom field mappings, authenticated Jira webhooks, and visible conflict resolution.
- Autonomous agent runs use AWS Bedrock only. The default PM classifier is Claude Haiku 4.5 and the default code-review specialist is Claude Sonnet 4.6; both are included in the default narrowed Bedrock model ARN list. Configure Opus only for escalation-grade reviews. GitHub repo access uses short-lived GitHub App installation tokens scoped to the requested repo.
- Fargate can run UDP services through NLB target groups, but LiveKit self-hosting is more sensitive than the Go app because WebRTC needs publicly reachable UDP media. LiveKit Cloud remains the lower-ops production path until self-hosting is proven.
- Secrets are read from AWS Secrets Manager into ECS tasks. Do not pass secret values as Terraform variables.
