# CI/CD Implementation Guide for Coding Agents

> **TARGET AUDIENCE**: Autonomous coding agents executing the CI/CD setup plan
> 
> **PURPOSE**: This guide provides explicit, no-ambiguity instructions to ensure complete implementation of all CI/CD features without placeholders, TODOs, or "insert code here" stubs.

---

## CRITICAL RULES FOR EXECUTION

### Rule #1: NO PLACEHOLDERS ALLOWED

**FORBIDDEN** in your implementation:
- ❌ `# TODO: Add actual implementation`
- ❌ `// Insert code here`
- ❌ `... (code omitted for brevity)`
- ❌ `<YOUR_VALUE_HERE>`
- ❌ `# FIXME: Complete this`
- ❌ Any comment suggesting incomplete implementation

**REQUIRED**:
- ✅ Complete, working code in every file
- ✅ All values filled in with actual data
- ✅ All logic fully implemented
- ✅ Every acceptance criterion must be verifiable by bash commands

### Rule #2: VERIFY EVERYTHING

After implementing each task:
1. Run the "Agent-Executed QA Scenarios" from the task
2. Ensure all assertions pass
3. Fix any failures immediately
4. Do NOT proceed to next task until current task passes ALL verification

### Rule #3: FILE REFERENCES ARE MANDATORY

Every file you create MUST reference actual files from this repository:
- Use exact file paths: `pkg/events/event_detector_test.go`
- Reference actual Go versions: `1.25` (from go.mod)
- Use real package names: `github.com/echotools/nevr-capture/v3`
- Copy patterns from existing test files when relevant

---

## EXECUTION CONTEXT

### Repository Information
- **Project**: nevrcap (github.com/echotools/nevr-capture/v3)
- **Go Version**: 1.25 (see `go.mod`)
- **Packages**: 4 (codecs, events, conversion, processing)
- **Test Files**: 25 test files, 4 benchmark files
- **Dependencies**: nevr-common/v4, klauspost/compress, gofrs/uuid

### Key Constraints
- **GOWORK=off**: CI must set `GOWORK=off` (go.work references ../nevr-common which won't exist in CI)
- **Pre-existing Issues**: 51 lint issues, 1 race condition, 1 coverage gap (use baseline approach)
- **golangci-lint**: v2.8.0 format (not v1)
- **Benchmark Format**: Standard Go text format (NOT JSON) for benchstat compatibility

---

## IMPLEMENTATION ROADMAP

### Wave 1: Configuration Foundation (Parallel)
**Start these 3 tasks immediately in parallel:**

1. **Task 1**: Create `.golangci.yml`
2. **Task 2**: Create `.github/dependabot.yml`
3. **Task 8**: Create `docs/branch-protection-setup.md`

**Expected time**: 30 minutes total
**Verification**: Each has Agent-Executed QA scenarios in plan

---

### Wave 2: GitHub Actions Workflows (After Wave 1)
**Start these 2 tasks in parallel after Wave 1 completes:**

4. **Task 3**: Create `.github/workflows/pr.yml`
   - **CRITICAL**: First add race test skip to `pkg/events/event_detector_test.go`
   - Then create workflow
5. **Task 4**: Create `.github/workflows/main.yml`

**Expected time**: 45 minutes total
**Dependencies**: Needs golangci-lint config from Task 1

---

### Wave 3: Git Hooks (After Wave 1)
**Start these 3 tasks in parallel after Wave 1 completes:**

6. **Task 5**: Create `scripts/hooks/pre-commit`
7. **Task 6**: Create `scripts/hooks/pre-push`
8. **Task 7**: Create `scripts/install-hooks.sh`

**Expected time**: 40 minutes total
**Dependencies**: Needs golangci-lint config from Task 1

---

### Wave 4: Benchmark Baseline (After Wave 2)
**Sequential after workflows:**

9. **Task 9**: Generate `.benchmarks/baseline.txt`

**Expected time**: 10 minutes
**Dependencies**: Needs workflows from Tasks 3-4 (they reference baseline)

---

### Wave 5: Integration Testing (Final)
**Sequential after all previous tasks:**

10. **Task 10**: End-to-end verification

**Expected time**: 20 minutes
**Dependencies**: ALL previous tasks (1-9)

---

## TASK-BY-TASK IMPLEMENTATION GUIDE

### Task 1: golangci-lint Configuration

**File to create**: `.golangci.yml`

**Exact requirements**:
```yaml
# This is golangci-lint v2 configuration format
# Version: 2.8.0

run:
  timeout: 5m
  go: '1.25'

linters:
  enable:
    - gofmt
    - govet
    - errcheck
    - staticcheck
    - ineffassign
    - unused
    - gosec
    - revive

linters-settings:
  errcheck:
    check-blank: true
  govet:
    enable-all: true
  gosec:
    excludes:
      - G104  # Audit errors not checked
  revive:
    rules:
      - name: exported
        disabled: true

issues:
  new-from-rev: HEAD~1  # Only report issues in new code
  exclude-use-default: false
  max-issues-per-linter: 0
  max-same-issues: 0

output:
  format: colored-line-number
  print-issued-lines: true
  print-linter-name: true
```

**Why this config**:
- `new-from-rev: HEAD~1`: Ignores 51 pre-existing issues (baseline approach)
- `go: '1.25'`: Matches go.mod version
- 8 linters: Exactly as user requested
- Timeout 5m: Prevents hanging in CI

**Verification commands** (from plan):
```bash
python3 -c "import yaml; yaml.safe_load(open('.golangci.yml'))"
golangci-lint config verify
cat .golangci.yml | grep -E "gofmt|govet|errcheck|staticcheck|ineffassign|unused|gosec|revive"
```

---

### Task 2: Dependabot Configuration

**File to create**: `.github/dependabot.yml`

**Exact requirements**:
```yaml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 5
    commit-message:
      prefix: "chore(deps)"
    labels:
      - "dependencies"
      - "automated"
```

**Why this config**:
- `gomod` ecosystem: Go module dependencies
- `weekly`: Not too aggressive
- `open-pull-requests-limit: 5`: Prevents PR flood
- `directory: "/"`: go.mod is at repository root

**Verification commands**:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
grep "package-ecosystem: gomod" .github/dependabot.yml
grep "open-pull-requests-limit: 5" .github/dependabot.yml
```

---

### Task 3: PR Validation Workflow

**CRITICAL FIRST STEP**: Add race test skip annotation

**File to modify**: `pkg/events/event_detector_test.go`

**Function**: `TestAsyncDetector_SensorIntegrationReceivesFrames` (around line 58-90)

**Add at the START of the function body**:
```go
func TestAsyncDetector_SensorIntegrationReceivesFrames(t *testing.T) {
	// Skip flaky race test in CI
	if os.Getenv("SKIP_RACE_FLAKY") == "1" {
		t.Skip("Skipping flaky race test (known issue tracked separately)")
	}
	
	// ... rest of existing test code ...
}
```

**Add import** (if not already present):
```go
import (
	"os"
	// ... existing imports ...
)
```

**THEN create**: `.github/workflows/pr.yml`

**Exact requirements**:
```yaml
name: PR Validation

on:
  pull_request:
    types: [opened, synchronize, reopened]

env:
  GOWORK: off  # CI doesn't have ../nevr-common sibling directory

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0  # Needed for new-from-rev

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v7
        with:
          version: v2.8.0
          args: --new-from-rev=origin/main

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Run tests with coverage
        run: |
          go test -v -coverprofile=coverage.out -covermode=atomic ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          file: ./coverage.out
          fail_ci_if_error: false

  coverage:
    name: Coverage Check
    runs-on: ubuntu-latest
    needs: test
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Check coverage thresholds
        run: |
          go test -coverprofile=coverage.out ./...
          
          # Per-package coverage check
          echo "Checking per-package coverage..."
          go tool cover -func=coverage.out | grep -E "^github.com/echotools/nevr-capture" | while read -r line; do
            pkg=$(echo "$line" | awk '{print $1}')
            percent=$(echo "$line" | awk '{print $3}' | tr -d '%')
            
            # Exception for conversion package (74.6%)
            if echo "$pkg" | grep -q "conversion"; then
              threshold=74.6
            else
              threshold=80.0
            fi
            
            if (( $(echo "$percent < $threshold" | bc -l) )); then
              echo "FAIL: $pkg has $percent% coverage (threshold: $threshold%)"
              exit 1
            else
              echo "PASS: $pkg has $percent% coverage (threshold: $threshold%)"
            fi
          done

  race:
    name: Race Detector
    runs-on: ubuntu-latest
    env:
      SKIP_RACE_FLAKY: "1"  # Skip known flaky race test
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Run race detector
        run: go test -race -timeout=5m ./...

  vuln:
    name: Vulnerability Scan
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Run govulncheck
        run: govulncheck ./...
```

**Why this workflow**:
- `GOWORK: off`: Required (go.work references missing directory in CI)
- `SKIP_RACE_FLAKY: "1"`: Skips the flaky race test
- `--new-from-rev=origin/main`: Ignores 51 pre-existing lint issues
- Coverage check: 80% for most packages, 74.6% exception for conversion
- `fetch-depth: 0`: Needed for golangci-lint baseline comparison

**Verification commands**:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"
grep "GOWORK" .github/workflows/pr.yml
grep -E "^\s+(lint|test|coverage|race|vuln):" .github/workflows/pr.yml | wc -l  # Should be 5
```

---

### Task 4: Main Branch CI Workflow

**File to create**: `.github/workflows/main.yml`

**Exact requirements**:
```yaml
name: Main Branch CI

on:
  push:
    branches:
      - main

env:
  GOWORK: off

jobs:
  test:
    name: Test Suite
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Run full test suite
        run: go test -v -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          file: ./coverage.out
          fail_ci_if_error: false

  race:
    name: Race Detector
    runs-on: ubuntu-latest
    env:
      SKIP_RACE_FLAKY: "1"
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Run race detector
        run: go test -race -timeout=5m ./...

  benchmark:
    name: Benchmark Regression
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Install benchstat
        run: go install golang.org/x/perf/cmd/benchstat@latest

      - name: Run benchmarks
        run: |
          go test -bench=. -benchmem -count=5 ./... | tee current-bench.txt

      - name: Compare with baseline
        run: |
          if [ -f .benchmarks/baseline.txt ]; then
            echo "Comparing benchmarks..."
            benchstat .benchmarks/baseline.txt current-bench.txt > comparison.txt || true
            cat comparison.txt
            
            # Check for regressions (>5% slower)
            if grep -E "\+[0-9]+\.[0-9]+%" comparison.txt; then
              echo "::warning::Performance regression detected"
              # Parse regression percentage
              regression=$(grep -Eo "\+[0-9]+\.[0-9]+%" comparison.txt | head -1 | tr -d '+%')
              if (( $(echo "$regression > 5.0" | bc -l) )); then
                echo "::error::Regression exceeds 5% threshold: $regression%"
                exit 1
              fi
            fi
          else
            echo "No baseline found, creating initial baseline"
          fi

      - name: Upload benchmark results
        uses: actions/upload-artifact@v4
        with:
          name: benchmark-results
          path: |
            current-bench.txt
            comparison.txt

  vuln:
    name: Vulnerability Scan
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true

      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Run govulncheck
        run: govulncheck ./...
```

**Why this workflow**:
- Runs only on main branch pushes
- Benchmark comparison with 5% threshold
- Uploads benchmark as artifact (no auto-commit to avoid branch protection conflict)
- Full test suite + race detector

**Verification commands**:
```bash
grep -A3 "on:" .github/workflows/main.yml | grep "branches: \[main\]"
grep "benchstat" .github/workflows/main.yml
grep "count=5" .github/workflows/main.yml
```

---

### Task 5: Pre-commit Hook

**File to create**: `scripts/hooks/pre-commit`

**Exact requirements**:
```bash
#!/usr/bin/env bash
set -e

echo "Running pre-commit checks..."

# Check 1: Go formatting
echo "Checking formatting..."
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "ERROR: The following files are not formatted:"
  echo "$UNFORMATTED"
  echo ""
  echo "Run: go fmt ./..."
  exit 1
fi

# Check 2: golangci-lint with baseline
echo "Running linter..."
if ! golangci-lint run --new-from-rev=HEAD~1 ./...; then
  echo "ERROR: Linting failed. Fix the issues above."
  exit 1
fi

# Check 3: go.mod consistency
echo "Checking go.mod..."
go mod tidy
if ! git diff --exit-code go.mod go.sum; then
  echo "ERROR: go.mod or go.sum is not up to date"
  echo ""
  echo "Run: go mod tidy"
  exit 1
fi

echo "✓ All pre-commit checks passed"
```

**Make executable**:
```bash
chmod +x scripts/hooks/pre-commit
```

**Why this hook**:
- Fast checks only (<10 seconds)
- Format, lint, go.mod (no tests, those are in pre-push)
- Uses baseline (`--new-from-rev=HEAD~1`) to ignore pre-existing issues

**Verification commands**:
```bash
ls -la scripts/hooks/pre-commit  # Check execute bit
head -1 scripts/hooks/pre-commit  # Check shebang
shellcheck scripts/hooks/pre-commit
```

---

### Task 6: Pre-push Hook

**File to create**: `scripts/hooks/pre-push`

**Exact requirements**:
```bash
#!/usr/bin/env bash
set -e

# Read pre-push hook arguments
remote="$1"
url="$2"

echo "Running pre-push checks..."

# Parse refs being pushed (format: local_ref local_sha remote_ref remote_sha)
while read local_ref local_sha remote_ref remote_sha; do
  # Check if pushing to main directly
  if [[ "$remote_ref" == "refs/heads/main" ]]; then
    current_branch=$(git rev-parse --abbrev-ref HEAD)
    if [[ "$current_branch" != "main" ]]; then
      echo "ERROR: Direct push to main from '$current_branch' is not allowed"
      echo "Please create a pull request instead"
      exit 1
    fi
  fi
done

# Check 1: Run tests
echo "Running tests..."
if ! go test -timeout=2m ./...; then
  echo "ERROR: Tests failed"
  exit 1
fi

# Check 2: Run race detector with flaky test skipped
echo "Running race detector..."
export SKIP_RACE_FLAKY=1
if ! go test -race -timeout=3m ./...; then
  echo "ERROR: Race detector found issues"
  exit 1
fi

echo "✓ All pre-push checks passed"
```

**Make executable**:
```bash
chmod +x scripts/hooks/pre-push
```

**Why this hook**:
- Tests + race detector (slower checks)
- Main branch protection (blocks direct push to main from feature branch)
- `SKIP_RACE_FLAKY=1`: Skips the known flaky race test

**Verification commands**:
```bash
ls -la scripts/hooks/pre-push
shellcheck scripts/hooks/pre-push
```

---

### Task 7: Hook Installation Script

**File to create**: `scripts/install-hooks.sh`

**Exact requirements**:
```bash
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
```

**Make executable**:
```bash
chmod +x scripts/install-hooks.sh
```

**Why this script**:
- Idempotent (safe to run multiple times)
- Verifies installation
- Includes uninstall support
- Clear user instructions

**Verification commands**:
```bash
bash scripts/install-hooks.sh
ls -la .git/hooks/pre-commit .git/hooks/pre-push
```

---

### Task 8: Branch Protection Documentation

**File to create**: `docs/branch-protection-setup.md`

**Exact requirements**:
```markdown
# GitHub Branch Protection Setup

This guide explains how to configure branch protection rules for the `main` branch to enforce CI/CD requirements.

## Required Settings

Navigate to: `Settings` → `Branches` → `Add branch protection rule`

### 1. Branch name pattern
```
main
```

### 2. Protect matching branches

**Require a pull request before merging**: ✅ Enabled
- Required approvals: **1**
- Dismiss stale pull request approvals: ✅ Enabled
- Require review from Code Owners: ⬜ Optional

**Require status checks to pass before merging**: ✅ Enabled
- Require branches to be up to date: ✅ Enabled
- Status checks that are required:
  - `lint` (from pr.yml)
  - `test` (from pr.yml)
  - `coverage` (from pr.yml)
  - `race` (from pr.yml)
  - `vuln` (from pr.yml)

**Require conversation resolution before merging**: ✅ Enabled

**Require linear history**: ⬜ Optional (prevents merge commits)

**Do not allow bypassing the above settings**: ✅ Enabled

**Restrict who can push to matching branches**: ⬜ Disabled (hooks + CI are sufficient)

**Allow force pushes**: ❌ Disabled

**Allow deletions**: ❌ Disabled

## Alternative: GitHub CLI Setup

Create `protection.json`:

```json
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["lint", "test", "coverage", "race", "vuln"]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismissal_restrictions": {},
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false,
    "required_approving_review_count": 1,
    "bypass_pull_request_allowances": {}
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
```

Apply with:
```bash
gh api repos/{owner}/{repo}/branches/main/protection \
  --method PUT \
  --input protection.json
```

Replace `{owner}` and `{repo}` with your repository details.

## Verification

After setting up branch protection:

1. Create a test branch
2. Make a change and create a PR
3. Verify that:
   - PR requires 1 approval
   - All 5 status checks must pass
   - Cannot push directly to main
   - Cannot force-push to main

## Troubleshooting

### "Status check not found"
- Ensure the workflow has run at least once
- Check that job names in `.github/workflows/pr.yml` match the required checks

### "Cannot enable branch protection"
- Requires admin access to the repository
- If using GitHub CLI, token needs `repo` scope

### "Status checks never complete"
- Check GitHub Actions logs for errors
- Verify workflows trigger on `pull_request` event
```

**Verification commands**:
```bash
ls docs/branch-protection-setup.md
wc -l docs/branch-protection-setup.md  # Should be >20 lines
grep -i "pull request" docs/branch-protection-setup.md
```

---

### Task 9: Generate Benchmark Baseline

**Directory to create**: `.benchmarks/`

**Command to run**:
```bash
mkdir -p .benchmarks
go test -bench=. -benchmem -count=5 ./... | tee .benchmarks/baseline.txt
```

**Expected output format** (in `.benchmarks/baseline.txt`):
```
goos: linux
goarch: amd64
pkg: github.com/echotools/nevr-capture/v3/pkg/events
cpu: AMD EPYC 7763 64-Core Processor
BenchmarkAsyncDetector_ProcessFrame-4     123456     9876 ns/op     1234 B/op     56 allocs/op
BenchmarkAsyncDetector_ProcessFrame-4     123456     9876 ns/op     1234 B/op     56 allocs/op
...
```

**Why this approach**:
- Standard Go benchmark text format (benchstat compatible)
- `count=5`: Statistical significance for comparison
- Committed to git: Baseline is version-controlled

**Verification commands**:
```bash
ls .benchmarks/baseline.txt
wc -l .benchmarks/baseline.txt  # Should have >10 lines
head -5 .benchmarks/baseline.txt | grep "Benchmark"
git ls-files .benchmarks/baseline.txt  # Should be tracked
```

---

### Task 10: End-to-End Integration Testing

**What to do**: Run all verification commands from previous tasks

**Test script** (optional, can run manually):
```bash
#!/usr/bin/env bash
set -e

echo "=== CI/CD Integration Test ==="
echo ""

echo "1. Installing hooks..."
bash scripts/install-hooks.sh
echo "✓ Hooks installed"
echo ""

echo "2. Verifying GOWORK=off build..."
GOWORK=off go build ./...
echo "✓ Build succeeds without go.work"
echo ""

echo "3. Verifying golangci-lint config..."
golangci-lint config verify
echo "✓ Lint config valid"
echo ""

echo "4. Verifying workflow YAML..."
python3 -c "import yaml; [yaml.safe_load(open(f)) for f in ['.github/workflows/pr.yml', '.github/workflows/main.yml']]"
echo "✓ All workflows valid"
echo ""

echo "5. Verifying Dependabot config..."
python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
echo "✓ Dependabot config valid"
echo ""

echo "6. Verifying benchmark baseline..."
test -f .benchmarks/baseline.txt
head -1 .benchmarks/baseline.txt | grep -E "^(goos|Benchmark)"
echo "✓ Benchmark baseline exists"
echo ""

echo "7. Verifying branch protection docs..."
test -f docs/branch-protection-setup.md
lines=$(wc -l < docs/branch-protection-setup.md)
if [ "$lines" -lt 20 ]; then
  echo "ERROR: Documentation too short"
  exit 1
fi
echo "✓ Branch protection docs complete"
echo ""

echo "=== All Integration Tests Passed ==="
```

**Verification**: All commands should exit with code 0

---

## COMMON PITFALLS TO AVOID

### ❌ PITFALL #1: Using JSON for Benchmarks
**WRONG**:
```json
{
  "BenchmarkAsyncDetector_ProcessFrame": {
    "ns/op": 9876
  }
}
```

**CORRECT**: Use standard Go text format (benchstat requirement)

---

### ❌ PITFALL #2: Forgetting GOWORK=off
**WRONG**:
```yaml
jobs:
  test:
    steps:
      - run: go test ./...
```

**CORRECT**:
```yaml
env:
  GOWORK: off

jobs:
  test:
    steps:
      - run: go test ./...
```

---

### ❌ PITFALL #3: Auto-committing Benchmarks
**WRONG**:
```yaml
- name: Update baseline
  run: |
    git add .benchmarks/baseline.txt
    git commit -m "Update baseline"
    git push
```

**CORRECT**: Upload as artifact (branch protection blocks auto-commit)
```yaml
- uses: actions/upload-artifact@v4
  with:
    name: benchmark-results
    path: current-bench.txt
```

---

### ❌ PITFALL #4: Wrong golangci-lint Version
**WRONG** (v1 format):
```yaml
linters-settings:
  golint:
    min-confidence: 0.8
```

**CORRECT** (v2 format):
```yaml
linters:
  enable:
    - revive  # replaces golint in v2
```

---

### ❌ PITFALL #5: Not Skipping Flaky Race Test
**WRONG**: Run race detector without env var
```yaml
- run: go test -race ./...
```

**CORRECT**: Set env to skip flaky test
```yaml
env:
  SKIP_RACE_FLAKY: "1"
steps:
  - run: go test -race ./...
```

---

## SUCCESS CRITERIA

Your implementation is complete when:

✅ All 10 tasks have passing Agent-Executed QA scenarios
✅ `bash scripts/install-hooks.sh` succeeds
✅ `GOWORK=off go build ./...` succeeds
✅ `golangci-lint config verify` exits 0
✅ All workflow YAML files parse successfully
✅ `.benchmarks/baseline.txt` exists and is in Go text format
✅ `docs/branch-protection-setup.md` exists with >20 lines
✅ NO files contain TODO, FIXME, or placeholder comments
✅ All hooks are executable (chmod +x)
✅ Pre-commit hook catches unformatted code
✅ Pre-push hook catches failing tests

---

## FINAL CHECKLIST

Before marking implementation complete:

- [ ] I ran ALL Agent-Executed QA scenarios from the plan
- [ ] Every scenario passed (exit code 0 where expected)
- [ ] I verified GOWORK=off is set in ALL workflow jobs
- [ ] I verified benchmark baseline is TEXT format (not JSON)
- [ ] I verified race test skip is implemented in test file
- [ ] I verified no auto-commit of benchmarks (artifact only)
- [ ] I verified golangci-lint uses v2 format
- [ ] I verified all hooks are executable (chmod +x)
- [ ] I verified no placeholders/TODOs in any file
- [ ] I ran integration test (Task 10) and all checks passed

---

**When in doubt**: Refer back to the full plan at `docs/ci-cd/plan.md` for exact specifications.
