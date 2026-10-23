#!/usr/bin/env bash
# Submit the Go modules this project actually compiles to GitHub's dependency
# graph via the dependency submission API. Only go, jq and curl are needed.
# Usage: submit-go-deps.sh [go.mod dir] [--dry-run]
#
# The set comes from `go list -deps`, not `go list -m all`. The module graph
# also holds modules that no package here imports (for example a dependency's
# own test or tool requirements), and submitting those raises alerts for code
# that is never built. Modules reached only from _test.go files are submitted
# with scope "development". Direct means required without `// indirect` in
# go.mod.
set -euo pipefail
dir="${1:-.}"; dry="${2:-}"
cd "$dir"
manifest="$(realpath --relative-to="${GITHUB_WORKSPACE:-$PWD}" go.mod 2>/dev/null || echo go.mod)"

# One line per package: module path, version, and the replacement if any.
fmt='{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{with .Replace}} {{.Path}} {{.Version}}{{end}}{{end}}{{end}}'
runtime="$(go list -deps -f "$fmt" ./... | sort -u)"
alltest="$(go list -deps -test -f "$fmt" ./... | sort -u)"
direct="$(go mod edit -json | jq -c '[.Require[]? | select(.Indirect | not) | .Path]')"

snapshot="$(jq -n -R \
  --arg runtime "$runtime" --arg alltest "$alltest" --argjson direct "$direct" \
  --arg sha "${GITHUB_SHA:-$(git rev-parse HEAD)}" \
  --arg ref "${GITHUB_REF:-refs/heads/main}" \
  --arg run "${GITHUB_RUN_ID:-local}" \
  --arg corr "${GITHUB_WORKFLOW:-local}-go-${manifest}" \
  --arg manifest "$manifest" \
  --arg scanned "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
  def mods($s): $s | split("\n") | map(select(length > 0) | split(" "))
    # A replacement with a version is what gets built; a local-path
    # replacement has none, so keep the required version.
    | map(if length == 4 and .[3] != "" then {path: .[0], ver: .[3], src: .[2]}
          else {path: .[0], ver: .[1], src: .[0]} end)
    | map(select(.ver != ""));
  (mods($runtime) | map(.path)) as $rt
  | [ mods($alltest)[]
      # purl-spec: golang namespace/name are lowercase, and "+" in a version
      # (for example +incompatible) must be percent-encoded.
      | ("pkg:golang/\(.src | ascii_downcase)@\(.ver | gsub("\\+"; "%2B"))") as $purl
      | { key: $purl,
          value: { package_url: $purl,
                   relationship: (if (.path | IN($direct[])) then "direct" else "indirect" end),
                   scope: (if (.path | IN($rt[])) then "runtime" else "development" end) } } ]
  | from_entries as $resolved
  | { version: 0, sha: $sha, ref: $ref,
      job: { correlator: $corr, id: $run },
      detector: { name: "go-list-deps", version: "2", url: "https://pkg.go.dev/cmd/go#hdr-List_packages_or_modules" },
      scanned: $scanned,
      manifests: { ($manifest): { name: $manifest, file: { source_location: $manifest }, resolved: $resolved } } }')"
echo "modules in snapshot: $(jq '.manifests[].resolved | length' <<<"$snapshot")" \
  "(development-only: $(jq '[.manifests[].resolved[] | select(.scope == "development")] | length' <<<"$snapshot"))"
if [ "$dry" = "--dry-run" ]; then jq . <<<"$snapshot"; exit 0; fi
curl -fsS -X POST \
  -H "Authorization: Bearer ${GITHUB_TOKEN:?GITHUB_TOKEN required}" \
  -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/${GITHUB_REPOSITORY:?}/dependency-graph/snapshots" \
  --data-binary @- <<<"$snapshot"
echo
