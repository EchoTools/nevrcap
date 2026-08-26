#!/usr/bin/env bash
# Install git hooks from scripts/hooks/ to .git/hooks/.
# Usage: ./scripts/hooks/install-hooks.sh [--uninstall]
set -euo pipefail

HOOKS_DIR="$(dirname "$0")"
GIT_HOOKS="$(git rev-parse --git-dir)/hooks"

if [[ "${1:-}" == "--uninstall" ]]; then
  for hook in pre-commit commit-msg pre-push; do
    target="$GIT_HOOKS/$hook"
    if [[ -f "$target" && "$(readlink "$target" 2>/dev/null || echo "$target")" == "$HOOKS_DIR/$hook" ]]; then
      rm "$target"
      echo "removed: $hook"
    fi
  done
  exit 0
fi

for hook in pre-commit commit-msg pre-push; do
  src="$HOOKS_DIR/$hook"
  if [[ ! -f "$src" ]]; then
    echo "WARNING: $src not found, skipping $hook"
    continue
  fi
  cp "$src" "$GIT_HOOKS/$hook"
  chmod +x "$GIT_HOOKS/$hook"
  echo "installed: $hook"
done
echo "✓ hooks installed"
