# auto-bot AWS Module

Cost-first, security-first dev runtime. Decomposed into focused files:

- `network.tf` — creates a **dedicated** VPC (`vpc_cidr`, default 10.40.0.0/16) with two
  public subnets + IGW + route table. Public-subnet-only, no NAT. Self-contained so it
  never couples to or drifts another project's VPC.
- `app.tf` — ECR repo, ECS Fargate cluster, ARM64 task definition, scale-to-zero service,
  ALB + listeners + target group, and the LiveKit Cloud / Bedrock / runtime env wiring.
- `security.tf` — security groups (ALB, app task, EFS), AWS WAF, the cosign KMS signing
  key, and scoped IAM (execution role + Bedrock-narrowed task role).
- `storage.tf` — EFS file system + access point for the app's SQLite board store
  (`/srv/data`), encrypted, with a deny-unencrypted-transport policy.
- `observability.tf` — CloudWatch log group and a slim ECS + ALB/WAF dashboard.

## Shape

- **Networking**: consumes an existing VPC and its public subnets. The Fargate task gets a
  public IP for NAT-free egress over the existing internet gateway, but its security group
  admits inbound **only from the ALB** — it is not internet-reachable. No NAT instance or
  gateway is created, so idle networking cost is ~$0.
- **Compute**: ECS Fargate, **ARM64** runtime, `app_desired_count` default **0**
  (scale-to-zero). The service has `ignore_changes = [desired_count]` so the fast-iteration
  helper can scale 0↔1 via the AWS API without Terraform drift.
- **Voice**: **LiveKit Cloud** only (`livekit_cloud_url` required). No self-hosted LiveKit,
  NLB, ElastiCache Redis, or TURN — those are gone, removing the dominant idle cost.
- **Edge**: app runs behind an ALB protected by AWS WAF (managed rule groups + per-IP rate
  limit). HTTP by default; set `app_certificate_arn` for HTTPS.
- **Supply chain**: ECR is `IMMUTABLE` with scan-on-push. The app image is signed with
  cosign against the module's KMS key (`cosign_kms_key_arn`) and verified on deploy.
- **Secrets**: app token, LiveKit Cloud key/secret, and optional OpenAI/Jira/GitHub
  credentials are injected from AWS Secrets Manager. Secret values are never passed as
  Terraform variables — only their ARNs.
- **Bedrock**: app task role is narrowed to the configured model ARNs + matching
  inference-profile ARNs. Autonomous agents use Bedrock only (Haiku 4.5 classifier,
  Sonnet 4.6 reviewer by default).
- The module is production-shaped and does not set `APP_LOCAL_LOGIN_TOKEN`; the local
  Keychain one-click login path is intentionally excluded from AWS.

Consumed through Terragrunt from `infra/live/dev`. Do not add backend or provider blocks
here; the root `infra/terragrunt.hcl` generates them.
