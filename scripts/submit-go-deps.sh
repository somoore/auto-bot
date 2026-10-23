#!/usr/bin/env bash
# Submit the Go module build list (MVS-selected versions, direct + indirect) to
# GitHub's dependency graph via the dependency submission API. Self-contained:
# only go, jq and curl. Usage: submit-go-deps.sh [go.mod dir] [--dry-run]
set -euo pipefail
dir="${1:-.}"; dry="${2:-}"
cd "$dir"
manifest="$(realpath --relative-to="${GITHUB_WORKSPACE:-$PWD}" go.mod 2>/dev/null || echo go.mod)"
snapshot="$(go list -m -json all | jq -s \
  --arg sha "${GITHUB_SHA:-$(git rev-parse HEAD)}" \
  --arg ref "${GITHUB_REF:-refs/heads/main}" \
  --arg run "${GITHUB_RUN_ID:-local}" \
  --arg corr "${GITHUB_WORKFLOW:-local}-go-${manifest}" \
  --arg manifest "$manifest" \
  --arg scanned "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
  [ .[] | select(.Main != true) | select(.Version != null)
    | (if .Replace and .Replace.Version then .Replace else . end) as $m
    # purl-spec: golang namespace/name are lowercase
    | ("pkg:golang/\($m.Path | ascii_downcase)@\($m.Version)") as $purl
    | { key: $purl,
        value: { package_url: $purl,
                 relationship: (if .Indirect then "indirect" else "direct" end),
                 scope: "runtime" } } ] | from_entries as $resolved
  | { version: 0, sha: $sha, ref: $ref,
      job: { correlator: $corr, id: $run },
      detector: { name: "go-list-m-all", version: "1", url: "https://pkg.go.dev/cmd/go#hdr-List_packages_or_modules" },
      scanned: $scanned,
      manifests: { ($manifest): { name: $manifest, file: { source_location: $manifest }, resolved: $resolved } } }')"
count="$(jq '.manifests[].resolved | length' <<<"$snapshot")"
echo "modules in snapshot: $count"
if [ "$dry" = "--dry-run" ]; then jq . <<<"$snapshot"; exit 0; fi
curl -fsS -X POST \
  -H "Authorization: Bearer ${GITHUB_TOKEN:?GITHUB_TOKEN required}" \
  -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/${GITHUB_REPOSITORY:?}/dependency-graph/snapshots" \
  --data-binary @- <<<"$snapshot"
echo
