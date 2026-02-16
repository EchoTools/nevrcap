#!/usr/bin/env bash
set -e

# Check for uninstall flag
if [[ "$1" == "--uninstall" ]]; then
  echo "Uninstalling git hooks..."
  rm -f .git/hooks/pre-commit .git/hooks/pre-push
  echo "✓ Git hooks uninstalled"
  exit 0
fi

# Check if this is a git repository
if [ ! -d .git ]; then
  echo "ERROR: Not a git repository"
  echo "Run this script from the repository root"
  exit 1
fi

echo "Installing git hooks..."

# Copy hooks
cp scripts/hooks/pre-commit .git/hooks/pre-commit
cp scripts/hooks/pre-push .git/hooks/pre-push

# Make executable
chmod +x .git/hooks/pre-commit
chmod +x .git/hooks/pre-push

# Verify installation
if [[ -x .git/hooks/pre-commit && -x .git/hooks/pre-push ]]; then
  echo "✓ Git hooks installed successfully"
  echo ""
  echo "Installed hooks:"
  echo "  - pre-commit: format, lint, go.mod checks"
  echo "  - pre-push: tests, race detector, main branch protection"
  echo ""
  echo "To bypass hooks: git commit --no-verify"
  echo "To uninstall: bash scripts/install-hooks.sh --uninstall"
else
  echo "ERROR: Hook installation failed"
  exit 1
fi
