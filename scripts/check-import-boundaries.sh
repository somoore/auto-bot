#!/usr/bin/env bash
set -euo pipefail

MODULE="$(go list -m)"
FAIL=0

fail() {
  printf 'import boundary violation: %s\n' "$1" >&2
  FAIL=1
}

check_pure_internal_package() {
  local pkg="$1"
  local imports
  imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$pkg")"
  while IFS= read -r import_path; do
    [ -z "$import_path" ] && continue

    if [[ "$import_path" == "$MODULE/"* ]]; then
      case "$import_path" in
        "$MODULE/internal/core"|"$MODULE/internal/core/"*|"$MODULE/internal/mocks"|"$MODULE/internal/mocks/"*) ;;
        *) fail "$pkg must not import application/runtime package $import_path" ;;
      esac
      continue
    fi

    if [[ "$import_path" == *"."*"/"* ]]; then
      fail "$pkg must stay provider-neutral; external import $import_path is not allowed"
    fi
  done <<< "$imports"
}

while IFS= read -r pkg; do
  case "$pkg" in
    "$MODULE/internal/core"|"$MODULE/internal/core/"*|"$MODULE/internal/mocks"|"$MODULE/internal/mocks/"*)
      check_pure_internal_package "$pkg"
      ;;
  esac
done < <(go list ./internal/core/... ./internal/mocks/...)

if rg -n "github.com/somoore/auto-bot/cmd/server" --glob '*.go' . >/tmp/auto-bot-boundary-server-imports.$$ 2>/dev/null; then
  cat /tmp/auto-bot-boundary-server-imports.$$ >&2
  fail "cmd/server is an application entrypoint and must not be imported"
fi
rm -f /tmp/auto-bot-boundary-server-imports.$$

if [ "$FAIL" -ne 0 ]; then
  exit 1
fi

printf 'import boundaries ok\n'
