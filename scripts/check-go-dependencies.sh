#!/usr/bin/env bash
set -euo pipefail

tmpdir="$(mktemp -d)"
cleanup() {
  if [ -f "$tmpdir/go.mod" ]; then cp "$tmpdir/go.mod" go.mod; fi
  if [ -f "$tmpdir/go.sum" ]; then cp "$tmpdir/go.sum" go.sum; fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

cp go.mod "$tmpdir/go.mod"
cp go.sum "$tmpdir/go.sum"

printf 'checking package graph...\n'
go list ./... >/dev/null
go list -deps ./... >/dev/null

printf 'checking module graph...\n'
go list -m all >/dev/null
go mod verify >/dev/null

printf 'checking go mod tidy has no diff...\n'
go mod tidy >/dev/null
if ! cmp -s go.mod "$tmpdir/go.mod" || ! cmp -s go.sum "$tmpdir/go.sum"; then
  printf 'go.mod or go.sum changed after go mod tidy; run go mod tidy and commit the result\n' >&2
  diff -u "$tmpdir/go.mod" go.mod || true
  diff -u "$tmpdir/go.sum" go.sum || true
  exit 1
fi

printf 'go dependency graph ok\n'
