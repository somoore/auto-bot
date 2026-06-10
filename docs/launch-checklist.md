# Launch checklist

Steps to take the repo public and ship the first release. Some require the repo to be
**public** first (GitHub gates branch protection and free CI minutes on visibility).

## 1. Before going public
- [ ] Confirm no secrets in history: `gitleaks detect --source . -v` (should report no leaks).
- [ ] `go build ./... && go test ./...` green.
- [ ] `govulncheck ./...` clean.
- [ ] `helm lint deploy/helm/auto-bot` clean.
- [ ] Review the README and `docs/deployment.md` for anything environment-specific.

## 2. Flip to public
- [ ] Settings → Danger Zone → Change visibility → **Public**.

## 3. Branch protection (only available once public on a free plan)
Protect `main` so all changes go through PRs + green CI:
```bash
gh api -X PUT repos/somoore/auto-bot/branches/main/protection \
  -H "Accept: application/vnd.github+json" --input - <<'JSON'
{
  "required_status_checks": { "strict": true, "contexts": ["go", "secrets", "helm"] },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_linear_history": true,
  "required_conversation_resolution": true
}
JSON
```
> The status-check contexts (`go`, `secrets`, `helm`) only become selectable after the CI
> workflow has run at least once. Push a commit / open a PR first, then apply this.

Outside contributors **fork** the repo and open PRs — they never get direct write access.
Branch protection makes even maintainers go through the same gated path.

## 4. Publishing images (release workflow)
GHCR publishes automatically (built-in token). Docker Hub is **optional**:
- [ ] (Optional) To also publish to Docker Hub: add repo secrets `DOCKERHUB_USERNAME`
      and `DOCKERHUB_TOKEN`, and set repo **variable** `DOCKERHUB_ENABLED=true`
      (Settings → Secrets and variables → Actions). Without it, only GHCR is used.
- [ ] After first publish, make the GHCR package **public** (the package's own settings —
      it's private by default even when the repo is public), or use an `imagePullSecret`.
- [ ] Tag a release to trigger the build + push:
  ```bash
  git tag v0.1.0 && git push origin v0.1.0
  ```
- [ ] Confirm the images exist and the chart pulls them:
  ```bash
  docker manifest inspect ghcr.io/somoore/auto-bot:0.1.0
  helm install auto-bot deploy/helm/auto-bot --dry-run --debug -f my-values.yaml
  ```

## 5. Dogfood the published artifacts
Before announcing, deploy from the **published** image (not a local build) to confirm the
community gets exactly what you ran:
```bash
helm install auto-bot deploy/helm/auto-bot \
  --set image.repository=ghcr.io/somoore/auto-bot \
  --set image.tag=0.1.0 \
  -f my-values.yaml
```

## 6. Nice-to-haves
- [ ] Add a `CODEOWNERS` file to auto-request your review on PRs.
- [ ] Enable Dependabot (Settings → Code security) for Go modules + Actions.
- [ ] Enable secret scanning + push protection (Settings → Code security).
- [ ] Add a repo description, topics, and the social-preview image.
