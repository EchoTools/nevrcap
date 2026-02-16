# Task 7: Create Hook Installation Script

## Context
You are implementing Task 7 of the CI/CD setup plan. This script installs (and uninstalls) the git hooks for local development.

## Files to Reference
- **scripts/hooks/pre-commit** — Hook to install (from Task 5)
- **scripts/hooks/pre-push** — Hook to install (from Task 6)
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 7 section, lines 806-904)

## What to Create
**File**: `scripts/install-hooks.sh` (executable bash script)

## Script Requirements

### Install Mode (Default)
Actions:
1. **Check git repo**: Verify `.git/` directory exists → Fail if not with "Not a git repository"
2. **Copy pre-commit**: `cp scripts/hooks/pre-commit .git/hooks/pre-commit`
3. **Copy pre-push**: `cp scripts/hooks/pre-push .git/hooks/pre-push`
4. **Set permissions**: `chmod +x .git/hooks/pre-commit .git/hooks/pre-push`
5. **Verify**: Check both files exist and are executable
6. **Success message**: Print instructions on what was installed

### Uninstall Mode (--uninstall flag)
Actions:
1. **Check git repo**: Same as install mode
2. **Remove hooks**:
   - `rm -f .git/hooks/pre-commit`
   - `rm -f .git/hooks/pre-push`
3. **Verify**: Check both files are gone
4. **Success message**: Confirm hooks removed

### Features
- **Idempotent**: Running multiple times is safe (overwrites existing)
- **No sudo**: Should not require root permissions
- **Clear output**: Show what's being done
- **Error handling**: Fail gracefully with clear messages

## Implementation Steps

1. **Create `scripts/install-hooks.sh`**
2. **Add shebang**: `#!/usr/bin/env bash`
3. **Parse arguments**: Check for `--uninstall` flag
4. **Implement install logic**:
   - Check `.git/` exists
   - Copy both hooks
   - Set executable permissions
   - Verify installation
5. **Implement uninstall logic**:
   - Check `.git/` exists
   - Remove both hooks
   - Verify removal
6. **Add success messages** with usage instructions
7. **Make executable**: `chmod +x scripts/install-hooks.sh`
8. **Validate script**: Run `shellcheck scripts/install-hooks.sh`

## Verification (Agent-Executed QA)

### Scenario 1: Script installs hooks successfully
```bash
# Step 1: Remove existing hooks
rm -f .git/hooks/pre-commit .git/hooks/pre-push

# Step 2: Run installer
bash scripts/install-hooks.sh 2>&1 | tee install-output.txt
# Expected: Exit code 0

# Step 3: Verify hooks installed
ls -la .git/hooks/pre-commit .git/hooks/pre-push
# Expected: Both files exist with execute bit

# Step 4: Verify success message
grep -i "success\|installed" install-output.txt
# Expected: Success message printed
```

### Scenario 2: Script is idempotent (can run multiple times)
```bash
# Step 1: Run installer again
bash scripts/install-hooks.sh 2>&1 | tee install-output-2.txt
# Expected: Exit code 0

# Step 2: Verify hooks still work
ls -la .git/hooks/pre-commit .git/hooks/pre-push
# Expected: Both files still exist and executable
```

### Scenario 3: Uninstall flag removes hooks
```bash
# Step 1: Ensure hooks are installed
bash scripts/install-hooks.sh

# Step 2: Run uninstaller
bash scripts/install-hooks.sh --uninstall 2>&1 | tee uninstall-output.txt
# Expected: Exit code 0

# Step 3: Verify hooks removed
ls .git/hooks/pre-commit 2>&1 || echo "pre-commit not found"
ls .git/hooks/pre-push 2>&1 || echo "pre-push not found"
# Expected: Both hooks removed (ls fails)

# Step 4: Verify uninstall message
grep -i "removed\|uninstall" uninstall-output.txt
# Expected: Uninstall confirmation message
```

### Scenario 4: Script fails gracefully outside git repo
```bash
# Step 1: Create temp directory (not a git repo)
mkdir -p /tmp/test-not-git && cd /tmp/test-not-git

# Step 2: Try to run installer
bash /path/to/scripts/install-hooks.sh 2>&1 | tee error-output.txt
# Expected: Exit code 1 (failure)

# Step 3: Verify error message
grep -i "not.*git.*repo" error-output.txt
# Expected: Clear error about not being a git repo

# Step 4: Cleanup
cd - && rm -rf /tmp/test-not-git
```

## Script Template Structure
```bash
#!/usr/bin/env bash
set -e

# Parse arguments
if [[ "$1" == "--uninstall" ]]; then
    # Uninstall mode
    # [Your implementation here]
    exit 0
fi

# Install mode
echo "Installing git hooks..."

# Check .git directory exists
if [[ ! -d ".git" ]]; then
    echo "Error: Not a git repository"
    exit 1
fi

# Copy hooks
# [Your implementation here]

# Set permissions
# [Your implementation here]

# Verify installation
# [Your implementation here]

echo "✓ Git hooks installed successfully"
echo ""
echo "Installed hooks:"
echo "  - pre-commit: format, lint, go.mod checks"
echo "  - pre-push: tests, race detector, main branch protection"
echo ""
echo "To uninstall: bash scripts/install-hooks.sh --uninstall"
```

## References
- **Git hooks location**: `.git/hooks/` directory
- **Git hooks docs**: https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks
- **Bash best practices**: https://google.github.io/styleguide/shellguide.html

## Success Criteria
✅ File `scripts/install-hooks.sh` created  
✅ Script is executable (chmod +x)  
✅ Correct shebang (#!/usr/bin/env bash)  
✅ Passes shellcheck validation  
✅ Installs both hooks successfully (tested)  
✅ Sets execute permissions on hooks  
✅ Verifies installation  
✅ Prints success message with instructions  
✅ Idempotent (tested - can run multiple times)  
✅ --uninstall flag removes hooks (tested)  
✅ Fails gracefully outside git repo (tested)  

## Commit
**Message**: `feat(hooks): add hook installation script with uninstall support`  
**Files**: `scripts/install-hooks.sh`  
**Pre-commit check**: `shellcheck scripts/install-hooks.sh`

## Anti-Patterns (DO NOT DO)
❌ Install hooks globally (only current repo)  
❌ Modify existing hook files beyond copying  
❌ Require root/sudo permissions  
❌ Silent operation (show what's being done)  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 5 (pre-commit), Task 6 (pre-push)
- **Blocks**: Task 10 (integration testing)
- **Blocked by**: None (can start immediately, but logically after hooks are written)
