# Task 1: Create golangci-lint Configuration

## Context
You are implementing Task 1 of the CI/CD setup plan. This creates the linter configuration that will be used by both local git hooks and GitHub Actions workflows.

## Files to Reference
- **go.mod** — Go version (1.25), project module path
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 1 section, lines 208-293)

## What to Create
**File**: `.golangci.yml` (repository root)

## Requirements

### Enable These 8 Linters
1. `gofmt` — Code formatting
2. `govet` — Go vet checks
3. `errcheck` — Unchecked errors
4. `staticcheck` — Static analysis
5. `ineffassign` — Ineffectual assignments
6. `unused` — Unused code
7. `gosec` — Security issues
8. `revive` — Configurable linting rules

### Configuration Settings
- **Timeout**: 5 minutes
- **Baseline Mode**: Use `--new-from-rev` to ignore 51 pre-existing issues
- **Test Exclusions**: Exclude test files from `gosec` and `revive` linters
- **Go Version**: Match go.mod (1.25)

### Critical Constraint
- **MUST NOT** fix existing 51 lint issues (out of scope)
- **MUST NOT** enable more than 8 specified linters (avoid config bloat)
- **MUST** use golangci-lint v2 format (v2.8.0 is installed)

## Baseline Approach
The codebase has 51 pre-existing lint issues:
- 44 errcheck (unchecked errors)
- 4 staticcheck (static analysis issues)
- 2 ineffassign (ineffectual assignments)
- 1 unused (unused variable/function)

These will be ignored using `--new-from-rev=HEAD~1` baseline mode. New issues will still be caught.

## Implementation Steps

1. **Create `.golangci.yml` at repository root**
2. **Configure linters section**: Enable exactly 8 linters listed above
3. **Configure run section**:
   - Set `timeout: 5m`
   - Set `go: "1.25"` (from go.mod)
4. **Configure issues section**:
   - Use `new-from-rev: HEAD~1` for baseline
   - Exclude test files from gosec/revive if needed
5. **Validate syntax**: Run `python3 -c "import yaml; yaml.safe_load(open('.golangci.yml'))"`
6. **Verify config**: Run `golangci-lint config verify`

## Verification (Agent-Executed QA)

### Scenario 1: Config is valid YAML and golangci-lint accepts it
```bash
# Step 1: Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('.golangci.yml'))"
# Expected: Exit code 0

# Step 2: Verify golangci-lint accepts config
golangci-lint config verify
# Expected: Exit code 0

# Step 3: Verify all 8 linters are enabled
cat .golangci.yml
# Expected: Contains all 8 linter names
```

### Scenario 2: Config enables new-issues-only mode
```bash
# Step 1: Check baseline configuration
grep -A5 "new-from-rev" .golangci.yml
# Expected: Contains "new-from-rev" setting

# Step 2: Test baseline behavior
golangci-lint run --new-from-rev=HEAD ./... 2>&1 | tee lint-output.txt
# Expected: Exit code 0 or minimal issues (not 51 pre-existing)
```

## References
- **golangci-lint v2 docs**: https://golangci-lint.run/usage/configuration/
- **New issues mode**: https://golangci-lint.run/usage/linters/#new-from-rev
- **Example config**: https://github.com/golangci/golangci-lint/blob/master/.golangci.example.yml

## Success Criteria
✅ File `.golangci.yml` exists at repository root  
✅ Config is valid YAML (python parser succeeds)  
✅ golangci-lint validates config (exit code 0)  
✅ All 8 linters enabled in config  
✅ Baseline mode configured (`new-from-rev`)  
✅ Timeout set to 5m  
✅ Go version set to 1.25  

## Commit
**Message**: `chore(ci): add golangci-lint configuration with baseline`  
**Files**: `.golangci.yml`  
**Pre-commit check**: `golangci-lint config verify`

## Anti-Patterns (DO NOT DO)
❌ Enable more than 8 linters  
❌ Fix the 51 existing lint issues  
❌ Add custom exclude patterns beyond baseline  
❌ Use golangci-lint v1 format  
❌ Leave placeholders or TODOs in config  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 2 (Dependabot), Task 8 (Branch protection docs)
- **Blocks**: Tasks 3, 4, 5, 6 (workflows and hooks need lint config)
