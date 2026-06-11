# Deploying auto-bot on Kubernetes

This guide covers a production deployment with the Helm chart, ingress and TLS options,
AWS Bedrock auth via IAM Roles Anywhere, optional no-popup auth, and troubleshooting.
For a fast path, see the Quickstart in the [README](../README.md).

## Contents
- [Prerequisites](#prerequisites)
- [The container image](#the-container-image)
- [Secrets](#secrets)
- [AWS Bedrock via IAM Roles Anywhere](#aws-bedrock-via-iam-roles-anywhere)
- [Installing the chart](#installing-the-chart)
- [Ingress & TLS](#ingress--tls)
- [Access control & the no-popup auth pattern](#access-control--the-no-popup-auth-pattern)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- A Kubernetes cluster (k3s, kind, EKS, GKE, …) with a default `StorageClass` and an
  ingress controller.
- `kubectl` and `helm` 3.x.
- A [LiveKit Cloud](https://cloud.livekit.io) project (URL + API key/secret).
- An AWS account with Bedrock model access in **us-east-1** or **us-west-2**. Nova Sonic
  (`amazon.nova-2-sonic-v1:0`) is **not available in us-east-2**.

## The container image

Official images are published to **GitHub Container Registry** and are what the chart uses by
default — no build required:

```
ghcr.io/somoore/auto-bot:<version>   # e.g. :0.1.2, or :latest
```

They are multi-tag (`MAJOR.MINOR.PATCH`, `MAJOR.MINOR`, `latest`, `sha-<commit>`), built by the
release workflow from tagged commits, and **signed with cosign** (+ SBOM and provenance). See
[Verifying the published image](#verifying-the-published-image).

To build your own instead (e.g. a private registry):

```bash
docker build -t <your-registry>/auto-bot:<tag> .
docker push <your-registry>/auto-bot:<tag>
```

The image runs as uid/gid `10001` and reads `web/` templates relative to `/srv`, so the chart
sets `workingDir: /srv` and `fsGroup: 10001` for you. It includes `libopus` (needed by the
voice codecs).

> Building for a registry your cluster can't reach over the internet? A small in-cluster
> registry plus your container runtime's mirror config works well (e.g. containerd's
> `registries.yaml` on k3s). Push multi-arch images with `skopeo copy`.

### Verifying the published image

Released images are **signed with cosign (keyless)** and carry **SBOM + SLSA provenance**
attestations, all produced by the GitHub Actions release workflow. Verify the signature
before deploying:

```bash
cosign verify ghcr.io/somoore/auto-bot:0.0.2-prealpha \
  --certificate-identity-regexp 'https://github.com/somoore/auto-bot/.github/workflows/release.yml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Inspect the attached SBOM and provenance:

```bash
cosign tree ghcr.io/somoore/auto-bot:0.0.2-prealpha          # list attestations
cosign verify-attestation ghcr.io/somoore/auto-bot:0.0.2-prealpha \
  --type spdxjson \
  --certificate-identity-regexp 'https://github.com/somoore/auto-bot/.github/workflows/release.yml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Secrets

The chart never contains secrets. It reads them from a Secret you create (`existingSecret`,
default `auto-bot-secrets`) and injects the keys in `secretEnvKeys`:

```bash
kubectl create secret generic auto-bot-secrets \
  --from-literal=APP_API_TOKEN="$(openssl rand -hex 32)" \
  --from-literal=LIVEKIT_URL="wss://your-project.livekit.cloud" \
  --from-literal=LIVEKIT_API_KEY="..." \
  --from-literal=LIVEKIT_API_SECRET="..." \
  --from-literal=LIVEKIT_BROWSER_URL="wss://your-project.livekit.cloud"
```

For GitOps, encrypt with [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets),
[External Secrets](https://external-secrets.io), or SOPS so the manifest is safe to commit.
The cluster's etcd stores Secrets base64-encoded; enable encryption-at-rest if your threat
model requires it.

## AWS Bedrock via IAM Roles Anywhere

This lets the pod call Bedrock with **no long-lived AWS key**. A sidecar
(`aws_signing_helper serve`) presents an X.509 client cert to AWS, gets short-lived STS
credentials, and serves them on an IMDS-compatible localhost endpoint that the AWS SDK reads.

### 1. Create the AWS resources

```bash
cd deploy/terraform/roles-anywhere
./gen-certs.sh                                  # CA + leaf cert in ./certs
cp terraform.tfvars.example terraform.tfvars    # set agent_model_arns for your region
terraform init && terraform apply               # outputs the 3 ARNs the chart needs
```

`agent_model_arns` for the Claude agent path is the fiddly part: the `us.anthropic.*` IDs are
**inference profiles** that route across regions, so the IAM policy needs permission on **both**
the profile ARN **and** each underlying foundation-model ARN. List them with:

```bash
aws bedrock get-inference-profile \
  --inference-profile-identifier us.anthropic.claude-haiku-4-5-20251001-v1:0 \
  --query 'models[].modelArn'
```

### 2. Build the sidecar image

> Unlike the app image, the Roles Anywhere sidecar is **not published** — build it yourself and
> set `awsRolesAnywhere.image` to your registry. (The chart's default value is a placeholder.)

The AWS-published `aws_signing_helper` binary targets a modern CPU baseline (x86-64-v3 / AVX2).
If your nodes present a generic virtual CPU without AVX2 (common on some hypervisors), the binary
exits with `CPU ISA level is lower than required`. Build from source with `GOAMD64=v1`:

```dockerfile
# Pin the upstream release tag and digest-pin the base images.
FROM golang:1.26-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends git libpcsclite-dev gcc pkg-config
RUN git clone --depth 1 --branch v1.8.4 \
    https://github.com/aws/rolesanywhere-credential-helper.git /src
WORKDIR /src
RUN CGO_ENABLED=1 GOAMD64=v1 go build -o /aws_signing_helper .
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libpcsclite1
COPY --from=build /aws_signing_helper /usr/local/bin/aws_signing_helper
ENTRYPOINT ["/usr/local/bin/aws_signing_helper"]
```

> For reproducible builds, also digest-pin the base images (`golang:1.26-bookworm@sha256:...`,
> `debian:bookworm-slim@sha256:...`) — the project's own `Dockerfile` shows the pattern.

Push it and set `awsRolesAnywhere.image`.

### 3. Store the leaf cert and enable it

```bash
kubectl create secret generic auto-bot-ra-cert \
  --from-file=leaf.crt=certs/leaf.crt --from-file=leaf.key=certs/leaf.key
```

Then enable `awsRolesAnywhere` in your values with the three ARNs from Terraform. Verify after
deploy:

```bash
kubectl exec deploy/auto-bot -c aws-signing-helper -- \
  wget -qO- http://127.0.0.1:9911/latest/meta-data/iam/security-credentials/
```

The app's `/voice/status` (authenticated) should report `"aws_ready": true`.

## Installing the chart

```bash
helm install auto-bot deploy/helm/auto-bot -f my-values.yaml
# upgrades:
helm upgrade auto-bot deploy/helm/auto-bot -f my-values.yaml
```

The deployment uses `strategy: Recreate` and a single replica on purpose — SQLite-WAL on a
ReadWriteOnce volume and in-process SFU state mean **two pods must never run at once**.

## Ingress & TLS

The app speaks plain HTTP on `:3000`. Terminate TLS wherever you like:

- **Ingress controller + cert-manager** — set `ingress.className`, `ingress.host`, and
  `ingress.tlsSecret` (e.g. a cert-manager-issued secret), plus any `ingress.annotations`.
- **TLS terminated upstream** (a tunnel, service mesh, or VPN front door) — leave
  `ingress.tlsSecret` empty; the Ingress serves plain HTTP and the upstream handles TLS.

> **WebSocket / CSP gotcha:** the app derives its Content-Security-Policy `connect-src` (which
> governs the board's `wss://` WebSocket) from `APP_BASE_URL`. The chart sets `APP_BASE_URL` to
> `https://<ingress.host>` automatically — so make sure `ingress.host` is the exact hostname the
> browser uses. If the board loads but shows "reconnecting", a host mismatch is the usual cause.

## Access control

The app has its own login gate: on first use it prompts for `APP_API_TOKEN` and then sets an
HttpOnly session cookie. There are three ways to run it, from simplest to most capable. Pick one.

### 1. Shared token (simplest)

Leave the app on token auth (`APP_AUTH_MODE=token`, the default). Users paste `APP_API_TOKEN`
once. If access is **already gated upstream** (SSO at the edge, a private network, a tunnel) you
can skip even that prompt by having your ingress inject the token so the app auto-authenticates:

```text
Authorization: Bearer <APP_API_TOKEN>
```

With Traefik, a `Middleware` with `headers.customRequestHeaders`; with nginx, a
`configuration-snippet`. Keep the token in the middleware/snippet object, never in the browser.

> **Only inject the token when access is already gated upstream** — the injected token grants
> full access to whoever reaches the route, and **everyone shares the single identity
> `api-token`** (so the meeting host gate can't distinguish users, and LiveKit participants would
> collide). For real per-user identity, use one of the SSO options below.
>
> If you mix paths — e.g. a public SSO ingress *and* a private admin ingress that injects the
> token — set `ADMIN_BEARER_HOSTS` to the comma-separated Host(s) where the injected bearer is
> allowed (the admin ingress). The bearer is then honored only on those hosts; SSO-fronted public
> hosts fail closed (401) if SSO identity is missing, instead of silently downgrading to the
> shared `api-token` identity.

### 2. Cloudflare Access (per-user identity, no app secret at the edge)

Front the app with a [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/)
application. Cloudflare does the SSO login at the edge and forwards a signed JWT; the app
verifies it (RS256, against the team JWKS), binds it to your application, and derives a distinct
per-user identity from the verified email. Set:

| Var | Value |
|---|---|
| `APP_CF_ACCESS_AUTH` | `1` |
| `CF_ACCESS_TEAM_DOMAIN` | `https://<your-team>.cloudflareaccess.com` |
| `CF_ACCESS_AUD` | the Access application's **Application Audience (AUD) tag** |
| `ALLOWED_EMAILS` and/or `ALLOWED_EMAIL_DOMAINS` | who may use the app (required — refuses to start open) |

### 3. AWS ALB OIDC / Cognito (per-user identity behind an ALB)

When running behind an Application Load Balancer with an `authenticate-cognito` action, set
`APP_ALB_OIDC_AUTH=1`, `APP_ALB_ARN=<your ALB ARN>`, and the same `ALLOWED_EMAILS` /
`ALLOWED_EMAIL_DOMAINS` allowlist. The app verifies the ALB-signed `X-Amzn-Oidc-Data` header and
derives identity from the verified email.

> Options 2 and 3 derive a **distinct, server-controlled identity per user** from their verified
> email — the meeting host gate and LiveKit participants work correctly for multi-user meetings.
> The client cannot name itself; it may only set a cosmetic display label. All of these vars are
> plain config — set them through the chart's `config:` block (non-secret) or your own env.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Pod `CrashLoopBackOff`, log `LIVEKIT_API_KEY ... required` | Missing LiveKit secret keys (or `APP_ENV` isn't `local` and creds absent). |
| `aws-signing-helper` exits 127, `CPU ISA level too low` | Sidecar built for AVX2; rebuild with `GOAMD64=v1`. |
| Voice never connects, `/voice/status` `aws_ready:false` | Bedrock creds/scoping. Confirm Roles Anywhere with `get-caller-identity` from the sidecar; check the IAM policy covers all 3 Bedrock actions + the right model/region ARNs. |
| Board loads but "reconnecting" | CSP blocks the WebSocket — `APP_BASE_URL`/`ingress.host` mismatch with the browser hostname. |
| Token prompt appears | Expected unless you inject the bearer header (above) or log in once. |

Health endpoint for probes: `GET /healthz` (unauthenticated, returns `{"ok":true}`).
