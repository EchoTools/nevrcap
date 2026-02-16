# Task 3: Create PR Validation Workflow

## Context
You are implementing Task 3 of the CI/CD setup plan. This GitHub Actions workflow validates all pull requests before they can be merged.

## Files to Reference
- **go.mod** — Go version (1.25)
- **pkg/events/event_detector_test.go** — Contains flaky race test that needs skip annotation
- **.golangci.yml** — Linter config (from Task 1)
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 3 section, lines 380-489)

## What to Create
**File**: `.github/workflows/pr.yml`

## CRITICAL: Two-Step Implementation

### Step 1: Add Race Test Skip (FIRST)
**File to modify**: `pkg/events/event_detector_test.go`

**Location**: In function `TestAsyncDetector_SensorIntegrationReceivesFrames` (around line 58-90)

**Add at start of test function body**:
```go
if os.Getenv("SKIP_RACE_FLAKY") == "1" {
    t.Skip("Skipping flaky race test in CI")
}
```

**Add import** (if not present):
```go
"os"
```

This allows CI to skip the known flaky race condition without modifying the test itself.

### Step 2: Create Workflow (THEN)
After adding the skip annotation, create the PR validation workflow.

## Workflow Requirements

### Trigger
- **Event**: `pull_request`
- **Types**: `opened`, `synchronize`, `reopened`

### Environment
- **Go Version**: 1.25 (from go.mod)
- **GOWORK**: Set to `off` (CRITICAL - go.work references ../nevr-common which won't exist in CI)
- **Cache**: Enable go modules cache for faster builds

### Jobs (5 Total)

#### Job 1: lint
- **Run**: `golangci-lint run --new-from-rev=origin/main ./...`
- **Use**: golangci-lint-action v7 (supports golangci-lint v2.8.0)
- **Baseline**: Ignore 51 pre-existing issues with `--new-from-rev`

#### Job 2: test
- **Run**: `go test -v -coverprofile=coverage.out ./...`
- **Generate**: Coverage profile for next job

#### Job 3: coverage
- **Input**: coverage.out from test job
- **Check**: Package-specific thresholds
  - **codecs, events, processing**: 80%+ required
  - **conversion**: 74.6%+ required (exception due to protobuf code)
- **Fail**: If any package below threshold
- **Comment**: Post coverage report as PR comment

#### Job 4: race
- **Run**: `SKIP_RACE_FLAKY=1 go test -race ./...`
- **Environment**: Set `SKIP_RACE_FLAKY=1` to skip flaky test
- **Purpose**: Detect race conditions in remaining tests

#### Job 5: vuln
- **Run**: `govulncheck ./...`
- **Purpose**: Scan for known vulnerabilities in dependencies

### Status Checks
All 5 jobs must pass for PR to be mergeable.

## Implementation Steps

1. **Add race test skip** to `pkg/events/event_detector_test.go` (Step 1)
2. **Create `.github/workflows/pr.yml`**
3. **Set workflow name**: "PR Validation"
4. **Configure trigger**: pull_request events
5. **Define 5 jobs** with dependencies (test → coverage)
6. **Set environment**: `GOWORK=off` in all jobs
7. **Configure caching**: Use actions/cache for go modules
8. **Add PR comment**: Use actions/github-script to post coverage
9. **Validate YAML**: Run `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"`

## Verification (Agent-Executed QA)

### Scenario 1: Workflow is valid YAML with GOWORK=off
```bash
# Step 1: Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"
# Expected: Exit code 0

# Step 2: Verify GOWORK=off is set
cat .github/workflows/pr.yml | grep "GOWORK"
# Expected: Contains "GOWORK=off" or "GOWORK: off"
```

### Scenario 2: All 5 required jobs are defined
```bash
# Step 1: Count jobs
grep -E "^\s+(lint|test|coverage|race|vuln):" .github/workflows/pr.yml | wc -l
# Expected: Output is 5

# Step 2: Verify golangci-lint-action version
grep "golangci-lint-action" .github/workflows/pr.yml
# Expected: Uses v7 or later (supports golangci-lint v2)
```

### Scenario 3: Coverage has package-specific thresholds
```bash
# Step 1: Check coverage threshold logic
grep -A10 "coverage:" .github/workflows/pr.yml
# Expected: Contains per-package coverage checking logic

# Step 2: Verify conversion exception
grep "74.6" .github/workflows/pr.yml
# Expected: Contains exception for conversion package at 74.6%

# Step 3: Verify default threshold
grep "80" .github/workflows/pr.yml
# Expected: Contains 80% threshold for other packages
```

### Scenario 4: Race test skip is in place
```bash
# Verify skip annotation added
grep -A3 "SKIP_RACE_FLAKY" pkg/events/event_detector_test.go
# Expected: Contains skip logic with env check
```

## References
- **GitHub Actions workflow syntax**: https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions
- **golangci-lint-action**: https://github.com/golangci/golangci-lint-action (use v7)
- **actions/setup-go**: https://github.com/actions/setup-go
- **govulncheck**: https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck

## Success Criteria
✅ Race test skip added to `pkg/events/event_detector_test.go`  
✅ File `.github/workflows/pr.yml` created  
✅ Workflow is valid YAML  
✅ All 5 jobs defined (lint, test, coverage, race, vuln)  
✅ `GOWORK=off` set in environment  
✅ Go version set to 1.25  
✅ golangci-lint uses `--new-from-rev=origin/main`  
✅ Coverage thresholds: 80% default, 74.6% for conversion  
✅ Race detector runs with `SKIP_RACE_FLAKY=1`  
✅ govulncheck included  

## Commit
**Message**: `feat(ci): add PR validation workflow with lint/test/coverage/race`  
**Files**: `.github/workflows/pr.yml`, `pkg/events/event_detector_test.go`  
**Pre-commit check**: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"`

## Anti-Patterns (DO NOT DO)
❌ Run benchmarks on PR (too slow, CI variance causes false positives)  
❌ Fail on pre-existing 51 lint issues (use baseline)  
❌ Enforce 80% on conversion package (use 74.6% exception)  
❌ Skip all race tests (only skip the one flaky test)  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 4 (Main workflow)
- **Blocks**: Tasks 9, 10 (benchmark baseline, integration testing)
- **Blocked by**: Tasks 1, 2 (needs lint config, dependabot config)
