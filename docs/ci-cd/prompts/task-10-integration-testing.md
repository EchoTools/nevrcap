# Task 10: End-to-End Integration Testing

## Context
You are implementing Task 10 of the CI/CD setup plan. This is the final verification that all CI/CD components work together correctly.

## Files to Reference
- All previous tasks (1-9)
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 10 section, lines 1114-1220)

## What to Do
Verify the entire CI/CD system works together by running comprehensive integration tests.

## Tests to Run

### Test 1: Hook Installation
```bash
bash scripts/install-hooks.sh
# Expected: Exit code 0, both hooks installed

ls -la .git/hooks/pre-commit .git/hooks/pre-push
# Expected: Both files exist with execute permissions
```

### Test 2: Pre-Commit Hook Integration
```bash
# Create unformatted test file
echo 'package test
func test(){println(1)}' > pkg/codecs/temp_integration_test.go

# Try to commit
git add pkg/codecs/temp_integration_test.go
git commit -m "integration test" 2>&1 | tee integration-output.txt

# Expected: Commit blocked (exit code 1) or passes after format
# Cleanup
git reset HEAD && rm -f pkg/codecs/temp_integration_test.go
```

### Test 3: Pre-Push Hook Integration
```bash
# Create failing test
echo 'package test
import "testing"
func TestFail(t *testing.T) { t.Fatal("fail") }' > pkg/codecs/temp_fail_test.go

# Commit
git add pkg/codecs/temp_fail_test.go && git commit -m "temp"

# Try to push (use --dry-run to avoid actually pushing)
git push --dry-run origin HEAD 2>&1 | tee push-output.txt

# Expected: Push blocked (exit code 1)
# Cleanup
git reset --hard HEAD~1 && rm -f pkg/codecs/temp_fail_test.go
```

### Test 4: Workflow YAML Validation
```bash
# Validate all workflow files
python3 -c "import yaml; [yaml.safe_load(open(f)) for f in ['.github/workflows/pr.yml', '.github/workflows/main.yml']]"
# Expected: Exit code 0 (valid YAML)
```

### Test 5: GOWORK=off Build
```bash
# Build without go.work
GOWORK=off go build ./...
# Expected: Exit code 0 (builds successfully)
```

### Test 6: golangci-lint Config Validation
```bash
golangci-lint config verify
# Expected: Exit code 0 (config valid)
```

### Test 7: Dependabot Config Validation
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
# Expected: Exit code 0 (valid YAML)
```

### Test 8: Benchmark Baseline Verification
```bash
ls .benchmarks/baseline.txt
# Expected: File exists

head -1 .benchmarks/baseline.txt | grep -E "^Benchmark"
# Expected: First line starts with "Benchmark"
```

### Test 9: Branch Protection Documentation
```bash
wc -l docs/branch-protection-setup.md
# Expected: >20 lines
```

### Test 10: Baseline Ignores Pre-existing Issues
```bash
golangci-lint run --new-from-rev=HEAD ./... 2>&1 | tee lint-new-issues.txt
wc -l lint-new-issues.txt
# Expected: Minimal output (not 51 pre-existing issues)
```

## Verification (Agent-Executed QA)

### Scenario 1: Complete system integration check
```bash
# Step 1: Install hooks
bash scripts/install-hooks.sh
# Expected: Exit code 0

# Step 2: Build without workspace
GOWORK=off go build ./...
# Expected: Exit code 0

# Step 3: Verify lint config
golangci-lint config verify
# Expected: Exit code 0

# Step 4: Validate all workflows
python3 -c "import yaml; [yaml.safe_load(open(f)) for f in ['.github/workflows/pr.yml', '.github/workflows/main.yml', '.github/dependabot.yml']]"
# Expected: Exit code 0

# Step 5: Verify benchmark baseline
ls .benchmarks/baseline.txt && head -1 .benchmarks/baseline.txt | grep -E "^Benchmark"
# Expected: File exists with correct format

# Step 6: Verify documentation
wc -l docs/branch-protection-setup.md
# Expected: >20 lines
```

### Scenario 2: Baseline handles pre-existing issues
```bash
# Run lint with baseline
golangci-lint run --new-from-rev=HEAD ./... 2>&1 | tee lint-new-issues.txt

# Check issue count
wc -l lint-new-issues.txt
# Expected: Minimal output (not 51 issues)
```

## Summary Report Template

After running all tests, create a summary:

```
CI/CD Integration Test Results
==============================

✓ Hook Installation: PASS
✓ Pre-Commit Hook: PASS
✓ Pre-Push Hook: PASS
✓ Workflow YAML Validation: PASS
✓ GOWORK=off Build: PASS
✓ golangci-lint Config: PASS
✓ Dependabot Config: PASS
✓ Benchmark Baseline: PASS
✓ Branch Protection Docs: PASS
✓ Baseline Pre-existing Issues: PASS

All 10 integration tests passed.

Files Created:
- .github/workflows/pr.yml
- .github/workflows/main.yml
- .github/dependabot.yml
- .golangci.yml
- scripts/hooks/pre-commit
- scripts/hooks/pre-push
- scripts/install-hooks.sh
- .benchmarks/baseline.txt
- docs/branch-protection-setup.md

Files Modified:
- pkg/events/event_detector_test.go (race test skip added)

Next Steps:
1. Run: bash scripts/install-hooks.sh
2. Set up branch protection: See docs/branch-protection-setup.md
3. Push to trigger first CI run
```

## Implementation Steps

1. **Run all 10 tests** sequentially
2. **Document results** for each test (pass/fail)
3. **Fix any failures immediately**:
   - If config invalid → fix config
   - If hook doesn't work → fix hook script
   - If build fails → investigate GOWORK issue
4. **Generate summary report** with results
5. **Clean up test artifacts** (temp files created during testing)
6. **Verify no permanent changes** to source files (except intended ones)

## References
- Tasks 1-9 acceptance criteria (reuse as integration tests)
- `.sisyphus/plans/ci-cd-setup.md` — Complete system requirements

## Success Criteria
✅ All 10 integration tests pass  
✅ Hooks install correctly  
✅ Pre-commit catches format issues  
✅ Pre-push catches test failures  
✅ All workflows are valid YAML  
✅ GOWORK=off build succeeds  
✅ golangci-lint config valid  
✅ Dependabot config valid  
✅ Benchmark baseline exists in correct format  
✅ Branch protection docs complete (>20 lines)  
✅ Baseline ignores 51 pre-existing issues  
✅ Summary report generated  

## Commit
**Message**: `test(ci): verify complete CI/CD system integration`  
**Files**: None (testing only, could add integration test script)  
**Pre-commit check**: None (final verification task)

## Anti-Patterns (DO NOT DO)
❌ Actually push to remote (use --dry-run)  
❌ Trigger actual GitHub Actions runs (local validation only)  
❌ Leave test artifacts (clean up temp files)  
❌ Modify source files permanently during tests  
❌ Skip any test (all 10 must pass)  

## Parallelization
- **Can run in parallel**: NO (final integration, must be sequential)
- **Parallel group**: Wave 5 (alone, after everything)
- **Blocks**: None (final task)
- **Blocked by**: All tasks (1-9)
