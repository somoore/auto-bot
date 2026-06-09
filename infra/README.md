# AWS Infrastructure

Terragrunt keeps Terraform DRY across environments. The dev stack is tuned for **low idle
cost, security-first defaults, and fast webapp iteration**.

## Layout

```text
infra/
  terragrunt.hcl              Root remote-state/provider generation
  env.hcl                     Shared dev inputs (existing VPC/subnets, scale-to-zero)
  live/dev/terragrunt.hcl     Dev environment wrapper
  modules/auto-bot/           Reusable module (network/app/security/storage/observability)
```

## State

The root config generates an S3 backend in every live module:

- Region: `us-east-1`
- State bucket: `auto-bot-terraform-state-<aws-account-id>`
- DynamoDB lock table: `auto-bot-terraform-locks`

Terragrunt creates the bucket and lock table during `init` when your identity has
permission.

## Provider pinning

Terraform CLI `= 1.15.2`, `hashicorp/aws = 6.45.0`, `hashicorp/cloudinit = 2.4.0`.
Provider checksums for all platforms are committed in `live/dev/.terraform.lock.hcl`.

## Architecture (dev)

- **Existing network**: deploys into the existing `bert-demo` VPC (`10.220.0.0/16`) and its
  two public subnets. The module looks them up by ID (`vpc_id`, `public_subnet_ids` in
  `env.hcl`) and creates **no** VPC, subnets, NAT, or route tables.
- **App**: ECS Fargate, **ARM64**, behind an ALB + WAF. The task runs in the public subnets
  with a public IP for NAT-free egress, but its security group admits inbound **only from
  the ALB**. `app_desired_count` defaults to **0** (scale-to-zero).
- **Voice**: **LiveKit Cloud** — set `LIVEKIT_CLOUD_URL` and the LiveKit Cloud project
  key/secret. No self-hosted media plane.
- **Persistence**: EFS-backed `/srv/data` for the SQLite board store.
- **Supply chain**: ECR immutable + scan-on-push; app image signed with cosign against a
  KMS key and verified on deploy.
- **Idle cost**: ALB (~$16/mo) + EFS/ECR storage (pennies). App task = $0 when scaled to
  zero; LiveKit Cloud = $0 on the dev tier.

## Prerequisites

- AWS auth in `us-east-1` (e.g. `assume test_AccountA/AdministratorAccess`).
- `terraform` 1.15.2, `terragrunt`, `docker` (with buildx), `cosign`, `aws` CLI.
- A LiveKit Cloud project (free/dev tier) — its URL, API key, and API secret.

## Deploy flow

1. **Create secrets** (writes to Secrets Manager, generates `.env.aws.local`):

   ```bash
   AWS_REGION=us-east-1 \
   LIVEKIT_CLOUD_URL=wss://your-project.livekit.cloud \
   LIVEKIT_API_KEY=APIxxxxxxxx \
   LIVEKIT_API_SECRET=xxxxxxxx \
     scripts/aws-upsert-secrets.sh
   set -a; source .env.aws.local; set +a
   ```

   Optional: set `OPENAI_API_KEY`, `JIRA_API_TOKEN` + `JIRA_CONFIG_JSON`/`_FILE`,
   `JIRA_WEBHOOK_SECRET`, and `GITHUB_APP_*` before running to enable those integrations.

2. **Bootstrap remote state + ECR**:

   ```bash
   cd infra/live/dev
   terragrunt init
   terragrunt apply -target=aws_ecr_repository.app
   cd ../../..
   ```

3. **Build, sign, and deploy** (linux/arm64 build → cosign sign → terragrunt apply):

   ```bash
   make aws-deploy        # = scripts/aws-app.sh deploy
   ```

   This builds the ARM64 image, pushes it to ECR, signs the digest with the cosign KMS key,
   records the immutable digest as `APP_IMAGE`, then applies Terraform so the task
   definition pins the new digest.

4. **Bring the app up / down** (scale-to-zero control):

   ```bash
   make aws-up            # scale to 1 task
   make aws-status        # desired/running + rollout state
   make aws-logs          # tail app logs
   make aws-down          # scale back to 0 (idle cost ~ ALB only)
   ```

5. **Read the URL**:

   ```bash
   scripts/aws-app.sh url       # http://<alb-dns>
   ```

## Fast iteration

After the first deploy, a webapp change is one command:

```bash
make aws-deploy
```

It rebuilds linux/arm64 natively (Apple Silicon, no emulation), re-signs, and rolls a new
task in ~1–2 min. Use `scripts/aws-app.sh redeploy` to bounce the current image without a
rebuild.

## Notes

- App runs with `APP_ENV=production`, token-backed HttpOnly sessions, and `APP_ROOM_ID` /
  `APP_BOARD_ID` authorization.
- Do not set `APP_LOCAL_LOGIN_TOKEN` in AWS — production startup rejects it.
- Secrets are read from Secrets Manager into the task; never pass secret values as
  Terraform variables.
- Bedrock model access is narrowed to explicit ARNs; the task role grants only
  `bedrock:InvokeModel*` on those.
