#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HOOK_TARGET="$REPO_ROOT/.git/hooks/pre-commit"
HOOK_SOURCE="$REPO_ROOT/scripts/pre-commit"

chmod +x "$HOOK_SOURCE"

cat > "$HOOK_TARGET" << 'HOOK'
#!/usr/bin/env bash
exec "$(git rev-parse --show-toplevel)/scripts/pre-commit"
HOOK

chmod +x "$HOOK_TARGET"

echo "✓ Pre-commit hook installed → .git/hooks/pre-commit"
echo "  Hook script: scripts/pre-commit"
echo ""
echo "Optional tools for deeper checks:"
echo "  go install golang.org/x/tools/cmd/goimports@latest"
echo "  go install golang.org/x/vuln/cmd/govulncheck@latest"
