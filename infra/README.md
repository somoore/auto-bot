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

The root Terragrunt config generates the provider block and pins:

```hcl
hashicorp/aws = 6.45.0
```

The version was verified against the HashiCorp releases index on 2026-05-15.

## Dev Deploy Flow

1. Authenticate to AWS in `us-east-1`.
2. Create/update secret values:

   ```bash
   AWS_REGION=us-east-1 ./scripts/aws-upsert-secrets.sh
   set -a; source .env.aws.local; set +a
   ```

   To enable Jira in ECS, set `JIRA_API_TOKEN` and either `JIRA_CONFIG_JSON` or `JIRA_CONFIG_JSON_FILE` before running the script. The uploaded Jira config should use `"api_token_env": "JIRA_API_TOKEN"` instead of a local token file path.

3. Bootstrap remote state and ECR:

   ```bash
   cd infra/live/dev
   terragrunt init
   terragrunt apply -target=aws_ecr_repository.app
   cd ../../..
   ```

4. Build and push the app image:

   ```bash
   set -a; source .env.aws.local; set +a
   ./scripts/aws-build-push.sh
   ```

5. Deploy the full stack:

   ```bash
   ./scripts/aws-deploy-dev.sh
   ```

6. Read outputs:

   ```bash
   cd infra/live/dev
   terragrunt output
   ```

## Notes

- The app runs behind an ALB.
- LiveKit runs behind an NLB with TCP signaling, TCP fallback, and one muxed UDP RTC media port.
- The app runs with `APP_ENV=production`, token-backed HttpOnly browser sessions, and `APP_ROOM_ID` / `APP_BOARD_ID` authorization for this deployment.
- The app mounts EFS at `/srv/data` and uses `BOARD_SQLITE_PATH=/srv/data/board.sqlite` for board snapshots and event history.
- Jira sync is injected through Secrets Manager. The app supports the same advanced Jira config used locally: project-key safety, status mappings, transition IDs, blocked flag fallback, story points field, sprint field, epic link field, rank custom field ID, and named custom field mappings.
- Fargate can run UDP services through NLB target groups, but LiveKit self-hosting is more sensitive than the Go app because WebRTC needs publicly reachable UDP media. LiveKit Cloud remains the lower-ops production path until self-hosting is proven.
- Secrets are read from AWS Secrets Manager into ECS tasks. Do not pass secret values as Terraform variables.
