#!/usr/bin/env bash
set -euo pipefail

MODULE="$(go list -m)"
FAIL=0

fail() {
  printf 'import boundary violation: %s\n' "$1" >&2
  FAIL=1
}

check_internal_package() {
  local pkg="$1"
  local kind="$2"
  local allowed_internal_re="$3"
  local imports
  imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$pkg")"
  while IFS= read -r import_path; do
    [ -z "$import_path" ] && continue

    if [[ "$import_path" == "$MODULE/"* ]]; then
      if [[ "$import_path" =~ $allowed_internal_re ]]; then
        continue
      fi
      fail "$pkg ($kind) must not import application/runtime package $import_path"
      continue
    fi

    # External imports: standard library has no '.' before the first '/'.
    # Allow oklog/ulid/v2 (pure ULID helper used by internal/agent) so the
    # provider-neutral packages can mint stable IDs without pulling in any
    # provider SDK. Update this list deliberately — every entry is a
    # cross-tier dependency promise.
    if [[ "$import_path" == *"."*"/"* ]]; then
      case "$import_path" in
        github.com/oklog/ulid/v2) ;;
        *) fail "$pkg ($kind) must stay provider-neutral; external import $import_path is not allowed" ;;
      esac
    fi
  done <<< "$imports"
}

# internal/core: most-isolated tier. May import only itself.
while IFS= read -r pkg; do
  case "$pkg" in
    "$MODULE/internal/core"|"$MODULE/internal/core/"*)
      check_internal_package "$pkg" core \
        "^$MODULE/internal/core(/.*)?\$"
      ;;
  esac
done < <(go list ./internal/core/...)

# internal/agent: provider-neutral domain tier. May import internal/core
# and itself. Must not pull in cmd/server or any provider SDK.
while IFS= read -r pkg; do
  case "$pkg" in
    "$MODULE/internal/agent"|"$MODULE/internal/agent/"*)
      check_internal_package "$pkg" agent \
        "^$MODULE/internal/(core|agent)(/.*)?\$"
      ;;
  esac
done < <(go list ./internal/agent/...)

# internal/mocks: test-only fakes. May import internal/core, internal/agent,
# internal/board, internal/mcp, and itself. The board + mcp allowances are
# required by mocks.BoardClient (an in-memory mcp.BoardClient used by tests
# and by cmd/mcpd's offline fallback); the rest of the mocks remain
# provider-neutral.
while IFS= read -r pkg; do
  case "$pkg" in
    "$MODULE/internal/mocks"|"$MODULE/internal/mocks/"*)
      check_internal_package "$pkg" mocks \
        "^$MODULE/internal/(core|agent|board|mcp|mocks)(/.*)?\$"
      ;;
  esac
done < <(go list ./internal/mocks/...)

# internal/mcp: provider-neutral MCP protocol surface. May import
# internal/core, internal/agent, internal/board, and itself. Must not
# pull in cmd/server (the application entrypoint) or any provider SDK.
# cmd/mcpd is the binary that wires this package; cmd/mcpd is allowed to
# import internal/mocks (it does so for the foundational in-memory
# RunStore in S2.0).
while IFS= read -r pkg; do
  case "$pkg" in
    "$MODULE/internal/mcp"|"$MODULE/internal/mcp/"*)
      check_internal_package "$pkg" mcp \
        "^$MODULE/internal/(core|agent|board|mcp)(/.*)?\$"
      ;;
  esac
done < <(go list ./internal/mcp/...)

# internal/projection: provider-neutral Projection contract + per-system
# projection implementations (jira, linear, github-issues). May import
# internal/core, internal/board, and itself. Must not pull in cmd/server
# (the application entrypoint) or any provider SDK.
while IFS= read -r pkg; do
  case "$pkg" in
    "$MODULE/internal/projection"|"$MODULE/internal/projection/"*)
      check_internal_package "$pkg" projection \
        "^$MODULE/internal/(core|board|projection)(/.*)?\$"
      ;;
  esac
done < <(go list ./internal/projection/...)

if rg -n "github.com/somoore/auto-bot/cmd/server" --glob '*.go' . >/tmp/auto-bot-boundary-server-imports.$$ 2>/dev/null; then
  cat /tmp/auto-bot-boundary-server-imports.$$ >&2
  fail "cmd/server is an application entrypoint and must not be imported"
fi
rm -f /tmp/auto-bot-boundary-server-imports.$$

if [ "$FAIL" -ne 0 ]; then
  exit 1
fi

printf 'import boundaries ok\n'
