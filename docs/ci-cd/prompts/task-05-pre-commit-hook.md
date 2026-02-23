# Task 5: Create Pre-Commit Git Hook

## Context
You are implementing Task 5 of the CI/CD setup plan. This local git hook runs before each commit to catch formatting and linting issues early.

## Files to Reference
- **.golangci.yml** — Linter configuration (from Task 1)
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 5 section, lines 597-699)

## What to Create
**File**: `scripts/hooks/pre-commit` (executable bash script)

## Hook Requirements

### Checks (Run in Order)

#### Check 1: Format
- **Run**: `go fmt ./...`
- **Check**: If any files changed
- **If fail**: Block commit with message "Code not formatted. Run 'go fmt ./...' and re-stage"
- **Exit code**: 1

#### Check 2: Lint
- **Run**: `golangci-lint run --new-from-rev=HEAD~1 ./...`
- **Check**: If any issues reported
- **If fail**: Block commit with error list
- **Exit code**: 1

#### Check 3: go.mod
- **Run**: `go mod tidy`
- **Check**: If go.mod or go.sum changed
- **If fail**: Block commit with message "go.mod out of sync. Run 'go mod tidy' and re-stage"
- **Exit code**: 1

### Performance
- **Target**: <10 seconds for typical changes
- **Optimization**: Only check changed packages/files if possible

### User Experience
- **Clear errors**: Tell user exactly what to fix
- **Exit codes**: 0 = pass, 1 = fail (blocks commit)
- **Informative**: Show which check failed and how to fix

## Implementation Steps

1. **Create `scripts/hooks/pre-commit`**
2. **Add shebang**: `#!/usr/bin/env bash` or `#!/bin/bash`
3. **Set error handling**: `set -e` (exit on error)
4. **Implement check 1** (go fmt)
   - Store git status before/after
   - Compare to detect changes
5. **Implement check 2** (golangci-lint)
   - Run with baseline flag
   - Capture output
6. **Implement check 3** (go mod tidy)
   - Store git status before/after
   - Compare to detect changes
7. **Add user-friendly error messages**
8. **Make executable**: `chmod +x scripts/hooks/pre-commit`
9. **Validate script**: Run `shellcheck scripts/hooks/pre-commit`

## Verification (Agent-Executed QA)

### Scenario 1: Hook script is executable with correct shebang
```bash
# Step 1: Check file permissions
ls -la scripts/hooks/pre-commit
# Expected: Permissions include execute bit (x)

# Step 2: Check shebang
head -1 scripts/hooks/pre-commit
# Expected: "#!/usr/bin/env bash" or "#!/bin/bash"

# Step 3: Validate shell script
shellcheck scripts/hooks/pre-commit
# Expected: Exit code 0 (no errors)
```

### Scenario 2: Hook catches unformatted code
```bash
# Step 1: Install hook
cp scripts/hooks/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit

# Step 2: Create unformatted test file
echo 'package test
func test(){println(1)}' > pkg/codecs/temp_test.go

# Step 3: Try to commit
git add pkg/codecs/temp_test.go
git commit -m "test" 2>&1 | tee hook-output.txt
# Expected: Exit code 1 (commit blocked)

# Step 4: Verify error message
grep -i "fmt\|format" hook-output.txt
# Expected: Output mentions formatting issue

# Step 5: Cleanup
git reset HEAD && rm pkg/codecs/temp_test.go
```

### Scenario 3: Hook catches go.mod inconsistencies
```bash
# Step 1: Modify go.mod
echo "require github.com/fake/dep v1.0.0" >> go.mod

# Step 2: Try to commit
git add go.mod
git commit -m "test" 2>&1 | tee hook-output.txt
# Expected: Exit code 1 (commit blocked)

# Step 3: Verify error message
grep -i "mod tidy" hook-output.txt
# Expected: Output mentions "go mod tidy"

# Step 4: Cleanup
git checkout -- go.mod
```

## Script Template Structure
```bash
#!/usr/bin/env bash
set -e

echo "Running pre-commit checks..."

# Check 1: Formatting
echo "Checking code formatting..."
# [Your implementation here]

# Check 2: Linting
echo "Running golangci-lint..."
# [Your implementation here]

# Check 3: go.mod
echo "Checking go.mod..."
# [Your implementation here]

echo "✓ All pre-commit checks passed"
exit 0
```

## References
- **Git hooks docs**: https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks
- **Pre-commit examples**: https://github.com/aitemr/awesome-git-hooks
- **go fmt**: https://pkg.go.dev/cmd/gofmt
- **golangci-lint**: https://golangci-lint.run/usage/quick-start/

## Success Criteria
✅ File `scripts/hooks/pre-commit` created  
✅ Script is executable (chmod +x)  
✅ Correct shebang (#!/usr/bin/env bash)  
✅ Passes shellcheck validation  
✅ Implements all 3 checks (fmt, lint, go.mod)  
✅ Catches unformatted code (tested)  
✅ Catches go.mod inconsistencies (tested)  
✅ Clear, actionable error messages  
✅ Exit code 0 on pass, 1 on fail  

## Commit
**Message**: `feat(hooks): add pre-commit hook for format/lint/go.mod checks`  
**Files**: `scripts/hooks/pre-commit`  
**Pre-commit check**: `shellcheck scripts/hooks/pre-commit`

## Anti-Patterns (DO NOT DO)
❌ Run tests (too slow for pre-commit, that's pre-push)  
❌ Download/install tools (use locally-installed only)  
❌ Run on all files (only staged or changed packages)  
❌ Silent failures (always show clear error messages)  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 6 (pre-push), Task 7 (installer)
- **Blocks**: Task 10 (integration testing)
- **Blocked by**: Task 1 (needs golangci-lint config)
