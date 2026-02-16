# Task 4: Create Main Branch CI Workflow

## Context
You are implementing Task 4 of the CI/CD setup plan. This workflow runs the full test suite and benchmarks when code is merged to main.

## Files to Reference
- **go.mod** — Go version (1.25)
- **.github/workflows/pr.yml** — Similar structure for consistency (from Task 3)
- **.benchmarks/baseline.txt** — Performance baseline (will be created in Task 9)
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 4 section, lines 492-594)

## What to Create
**File**: `.github/workflows/main.yml`

## Workflow Requirements

### Trigger
- **Event**: `push`
- **Branches**: `main` only
- **NO pull_request trigger** (this is main branch CI only)

### Environment
- **Go Version**: 1.25 (from go.mod)
- **GOWORK**: Set to `off` (CRITICAL - go.work references ../nevr-common)
- **Cache**: Enable go modules cache

### Jobs (4 Total)

#### Job 1: test
- **Run**: `go test -v -coverprofile=coverage.out ./...`
- **Purpose**: Full test suite with coverage

#### Job 2: race
- **Run**: `SKIP_RACE_FLAKY=1 go test -race ./...`
- **Purpose**: Race detection on main branch

#### Job 3: benchmark
This is the most complex job - handles benchmark regression detection.

**Steps**:
1. **Run benchmarks**: `go test -bench=. -benchmem -count=5 ./... | tee current-bench.txt`
   - `-count=5` for statistical significance
   - Save output to `current-bench.txt`

2. **Download baseline**: Fetch `.benchmarks/baseline.txt`

3. **Compare with benchstat**:
   ```bash
   go install golang.org/x/perf/cmd/benchstat@latest
   benchstat .benchmarks/baseline.txt current-bench.txt > comparison.txt
   ```

4. **Check for regressions**:
   - Parse `comparison.txt` for delta indicators
   - Look for `~` (likely regression) or `+` (definite regression) with >5% change
   - Example line: `BenchmarkProcessFrame  100ns ± 2%  110ns ± 2%  +10.00%  (p=0.000 n=5+5)`
   - Fail if any benchmark shows >5% regression

5. **Upload results**:
   - If no regression: Upload `current-bench.txt` as workflow artifact
   - Name: `benchmark-results-{sha}`
   - **DO NOT auto-commit** (conflicts with branch protection)
   - User can manually create PR to update baseline

#### Job 4: vuln
- **Run**: `govulncheck ./...`
- **Purpose**: Security vulnerability scanning

### Critical Constraints
- **NO auto-commit** of benchmark updates (branch protection blocks it)
- **NO fail-fast** (run all jobs even if one fails for visibility)
- Benchmarks ONLY run on main (not PRs) due to CI machine variance

## Implementation Steps

1. **Create `.github/workflows/main.yml`**
2. **Set workflow name**: "Main Branch CI"
3. **Configure trigger**: push to main only
4. **Define 4 jobs**: test, race, benchmark, vuln
5. **Set environment**: `GOWORK=off` in all jobs
6. **Implement benchmark logic**:
   - Install benchstat
   - Run benchmarks with count=5
   - Compare against baseline
   - Parse results for >5% regressions
   - Upload artifact (not auto-commit)
7. **Validate YAML**: Run `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/main.yml'))"`

## Verification (Agent-Executed QA)

### Scenario 1: Workflow triggers only on main branch
```bash
# Step 1: Check trigger configuration
cat .github/workflows/main.yml
grep -A3 "on:" .github/workflows/main.yml
# Expected: Contains "push:" with "branches: [main]"

# Step 2: Verify no pull_request trigger
grep "pull_request" .github/workflows/main.yml
# Expected: Exit code 1 (not found)
```

### Scenario 2: Benchmark job uses benchstat with 5% threshold
```bash
# Step 1: Check benchmark configuration
grep -A20 "benchmark:" .github/workflows/main.yml
# Expected: Contains "benchstat" command

# Step 2: Verify baseline reference
grep ".benchmarks/baseline.txt" .github/workflows/main.yml
# Expected: Found

# Step 3: Verify count=5 for statistical significance
grep "count=5" .github/workflows/main.yml
# Expected: Found

# Step 4: Verify 5% threshold logic
grep -E "5%|0.05" .github/workflows/main.yml
# Expected: Contains 5% regression detection logic
```

### Scenario 3: No auto-commit (artifact upload only)
```bash
# Step 1: Check for artifact upload
grep -A30 "benchmark:" .github/workflows/main.yml
# Expected: Contains "actions/upload-artifact"

# Step 2: Verify NO git commands
grep "git commit\|git push" .github/workflows/main.yml || echo "No git commands found"
# Expected: "No git commands found" (exit code 1 from grep)
```

## References
- **benchstat tool**: https://pkg.go.dev/golang.org/x/perf/cmd/benchstat
- **benchstat usage**: https://go.dev/blog/benchstat
- **Go benchmark docs**: https://pkg.go.dev/testing#hdr-Benchmarks
- **GitHub Actions artifacts**: https://docs.github.com/en/actions/using-workflows/storing-workflow-data-as-artifacts

## Success Criteria
✅ File `.github/workflows/main.yml` created  
✅ Workflow is valid YAML  
✅ Triggers only on push to main (not pull_request)  
✅ All 4 jobs defined (test, race, benchmark, vuln)  
✅ `GOWORK=off` set in environment  
✅ Go version set to 1.25  
✅ Benchmarks run with `-count=5`  
✅ benchstat compares against `.benchmarks/baseline.txt`  
✅ 5% regression threshold implemented  
✅ Results uploaded as artifact (no auto-commit)  
✅ NO git commit/push commands in workflow  

## Commit
**Message**: `feat(ci): add main branch workflow with benchmarks and regression detection`  
**Files**: `.github/workflows/main.yml`  
**Pre-commit check**: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/main.yml'))"`

## Anti-Patterns (DO NOT DO)
❌ Run benchmarks on every PR (main branch only per plan)  
❌ Auto-commit baseline updates (conflicts with branch protection)  
❌ Use fail-fast (run all jobs for complete visibility)  
❌ Use JSON format for benchmarks (benchstat needs text format)  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 3 (PR workflow)
- **Blocks**: Tasks 9, 10 (benchmark baseline, integration)
- **Blocked by**: Tasks 1, 2 (needs lint config)
