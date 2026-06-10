#!/usr/bin/env bash
set -euo pipefail

if [ ! -d .github/workflows ]; then
  printf 'no GitHub Actions workflows found\n'
  exit 0
fi

fail=0
while IFS= read -r line; do
  ref="${line#*@}"
  ref="${ref%%[[:space:]#]*}"
  if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
    printf 'workflow action is not pinned to a 40-character commit SHA: %s\n' "$line" >&2
    fail=1
  fi
done < <(rg -n 'uses:\s*[^#[:space:]]+@[^#[:space:]]+' .github/workflows || true)

if [ "$fail" -ne 0 ]; then
  exit 1
fi

printf 'GitHub Actions are pinned to commit SHAs\n'
