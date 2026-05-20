#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/keychain-get-secret.sh <service> [account]

Reads one secret from the macOS login Keychain and prints it to stdout.
Use from launcher scripts; avoid running directly in a shared terminal.
USAGE
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  usage
  exit 2
fi

if [ "$(uname -s)" != "Darwin" ]; then
  echo "macOS Keychain is only available on Darwin/macOS." >&2
  exit 1
fi

SERVICE="$1"
ACCOUNT="${2:-$USER}"

/usr/bin/security find-generic-password -s "$SERVICE" -a "$ACCOUNT" -w
