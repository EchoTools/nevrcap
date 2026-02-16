# Task 6: Create Pre-Push Git Hook

## Context
You are implementing Task 6 of the CI/CD setup plan. This local git hook runs before each push to catch test failures and protect the main branch.

## Files to Reference
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 6 section, lines 702-803)

## What to Create
**File**: `scripts/hooks/pre-push` (executable bash script)

## Hook Requirements

### Hook Arguments
Pre-push hooks receive special arguments:
- `$1` = remote name (e.g., "origin")
- `$2` = remote URL
- **stdin** = list of refs being pushed (format: `<local ref> <local sha> <remote ref> <remote sha>`)

### Checks (Run in Order)

#### Check 1: Tests
- **Run**: `go test ./...`
- **Timeout**: 2 minutes (prevent hanging)
- **If fail**: Block push with error details
- **Exit code**: 1

#### Check 2: Race Detector
- **Run**: `SKIP_RACE_FLAKY=1 go test -race ./...`
- **Environment**: Set `SKIP_RACE_FLAKY=1` to skip known flaky test
- **If fail**: Block push with race condition details
- **Exit code**: 1

#### Check 3: Main Branch Protection
- **Parse**: Read refs from stdin
- **Check**: If pushing to `refs/heads/main` or `main`
- **Check**: If current branch is NOT main
- **If fail**: Block with message "Direct pushes to main not allowed. Create a PR."
- **Exit code**: 1
- **Allow**: Pushes to main from main branch (after PR merge)

### Performance
- **Optimization**: Consider only running tests for changed packages
- **Timeout**: 2-minute limit to prevent infinite hangs

### User Experience
- **Clear errors**: Show exactly which tests failed
- **Progress**: Show what's being checked
- **Quick**: Fail fast if tests fail (don't run race if tests fail)

## Implementation Steps

1. **Create `scripts/hooks/pre-push`**
2. **Add shebang**: `#!/usr/bin/env bash` or `#!/bin/bash`
3. **Parse hook arguments**: Capture `$1` (remote), `$2` (URL)
4. **Read refs from stdin**: Parse line by line
5. **Implement check 1** (tests)
   - Run with timeout
   - Capture output
   - Fail fast on error
6. **Implement check 2** (race detector)
   - Set SKIP_RACE_FLAKY=1
   - Run with timeout
7. **Implement check 3** (main protection)
   - Parse refs for main branch
   - Check current branch
   - Block if pushing to main from feature branch
8. **Make executable**: `chmod +x scripts/hooks/pre-push`
9. **Validate script**: Run `shellcheck scripts/hooks/pre-push`

## Verification (Agent-Executed QA)

### Scenario 1: Hook blocks push with failing tests
```bash
# Step 1: Install hook
cp scripts/hooks/pre-push .git/hooks/pre-push
chmod +x .git/hooks/pre-push

# Step 2: Create failing test
echo 'package test
import "testing"
func TestFail(t *testing.T) { t.Fatal("fail") }' > pkg/codecs/temp_fail_test.go

# Step 3: Commit and try to push
git add pkg/codecs/temp_fail_test.go && git commit -m "temp test"
git push origin HEAD 2>&1 | tee hook-output.txt
# Expected: Exit code 1 (push blocked)

# Step 4: Verify error message
grep -i "test.*fail" hook-output.txt
# Expected: Output shows test failure

# Step 5: Cleanup
git reset --hard HEAD~1 && rm -f pkg/codecs/temp_fail_test.go
```

### Scenario 2: Hook blocks direct push to main from feature branch
```bash
# Step 1: Create feature branch
git checkout -b temp-feature-branch

# Step 2: Make change and commit
echo "# test" >> README.md && git add README.md && git commit -m "test"

# Step 3: Try to push to main
git push origin temp-feature-branch:main 2>&1 | tee hook-output.txt
# Expected: Exit code 1 (push blocked)

# Step 4: Verify protection message
grep -i "direct push.*main\|main.*not allowed" hook-output.txt
# Expected: Output mentions main branch protection

# Step 5: Cleanup
git checkout main && git branch -D temp-feature-branch
git reset --hard HEAD~1  # if committed on main
```

### Scenario 3: Hook allows push with passing tests
```bash
# Step 1: Make safe change
echo "# test" >> README.md && git add README.md && git commit -m "test"

# Step 2: Try push with dry-run
git push --dry-run origin HEAD 2>&1 | tee hook-output.txt
# Expected: Exit code 0 (push would be allowed)

# Step 3: Verify no errors
grep -v "error\|fail" hook-output.txt || true
# Expected: No error messages

# Step 4: Cleanup
git reset --hard HEAD~1
```

## Script Template Structure
```bash
#!/usr/bin/env bash
set -e

# Parse arguments
remote="$1"
url="$2"

echo "Running pre-push checks..."

# Read refs from stdin
while read local_ref local_sha remote_ref remote_sha; do
    # Check if pushing to main
    # [Your implementation here]
done

# Check 1: Run tests
echo "Running tests..."
timeout 120s go test ./...
# [Error handling here]

# Check 2: Run race detector
echo "Running race detector..."
SKIP_RACE_FLAKY=1 timeout 120s go test -race ./...
# [Error handling here]

echo "✓ All pre-push checks passed"
exit 0
```

## References
- **Git pre-push docs**: https://git-scm.com/docs/githooks#_pre_push
- **Pre-push examples**: https://github.com/git-hooks/git-hooks/blob/master/pre-push.sample
- **go test flags**: https://pkg.go.dev/cmd/go#hdr-Testing_flags

## Success Criteria
✅ File `scripts/hooks/pre-push` created  
✅ Script is executable (chmod +x)  
✅ Correct shebang (#!/usr/bin/env bash)  
✅ Passes shellcheck validation  
✅ Blocks push with failing tests (tested)  
✅ Blocks direct push to main from feature branch (tested)  
✅ Allows push with passing tests (tested)  
✅ Runs race detector with SKIP_RACE_FLAKY=1  
✅ Parses hook arguments correctly  
✅ Timeout protection (2 minutes)  

## Commit
**Message**: `feat(hooks): add pre-push hook for tests/race/main-protection`  
**Files**: `scripts/hooks/pre-push`  
**Pre-commit check**: `shellcheck scripts/hooks/pre-push`

## Anti-Patterns (DO NOT DO)
❌ Run benchmarks (too slow, that's in CI)  
❌ Run coverage (that's in CI)  
❌ Download/install tools  
❌ Block all pushes to main (allow from main branch after PR merge)  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 5 (pre-commit), Task 7 (installer)
- **Blocks**: Task 10 (integration testing)
- **Blocked by**: Task 1 (needs test suite to be runnable)
