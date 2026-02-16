# Comprehensive CI/CD Setup with Test Coverage and Benchmark Regression

## TL;DR

> **Quick Summary**: Implement GitHub Actions CI/CD with comprehensive test coverage (80%+), benchmark regression detection (5% threshold), and local git hooks to catch issues before push. Uses baseline approach for 51 pre-existing lint issues to unblock CI immediately.
> 
> **Deliverables**:
> - `.github/workflows/pr.yml` — PR validation (lint, test, coverage, race detector)
> - `.github/workflows/main.yml` — Main branch CI (full suite + benchmarks)
> - `.github/dependabot.yml` — Automated dependency updates
> - `.golangci.yml` — Linter configuration (8 comprehensive linters)
> - `scripts/hooks/pre-commit` — Format, lint, go.mod checks
> - `scripts/hooks/pre-push` — Tests, race detector, main branch protection
> - `scripts/install-hooks.sh` — Hook installation script
> - `.benchmarks/baseline.txt` — Benchmark performance baseline (standard Go benchmark text format)
> - `docs/branch-protection-setup.md` — GitHub branch protection instructions
> 
> **Estimated Effort**: Medium (6-8 hours implementation + testing)
> **Parallel Execution**: YES - 3 waves
> **Critical Path**: Config files (Task 1-2) → Workflows (Task 3-4) → Hooks (Task 5-6) → Integration (Task 7-8)

---

## Context

### Original Request
User wants comprehensive CI/CD with comprehensive test coverage and benchmark regression testing, plus git hooks to protect main branch from human errors.

### Interview Summary
**Key Discussions**:
- **CI Platform**: GitHub Actions (native integration, user preferred)
- **Test Coverage**: 80%+ enforced with fail-on-threshold
- **Benchmarks**: All benchmarks tracked with 5% regression threshold (strict)
- **Git Hooks**: Local + CI execution (fast feedback, CI as final gate)
- **Branch Protection**: 1 PR approval required, passing CI, prevent force-push
- **Linting**: Comprehensive (gofmt, govet, errcheck, staticcheck, ineffassign, unused, gosec, revive)
- **Dependencies**: Dependabot + govulncheck
- **Notifications**: GitHub only (PR comments, status checks)

**Research Findings**:
- Go 1.25 library with 4 packages (codecs, events, conversion, processing)
- 25 test files, 4 benchmark files
- Test patterns: table-driven, tb.Helper() utilities, round-trip integration, concurrency, resource leak detection
- Performance targets: 600+ Hz frame processing, <1ms event detection
- No existing CI/CD or active git hooks
- Key dependency: nevr-common/v4 (protobuf definitions)

### Metis Review
**Identified Blockers** (using baseline approach):
- **51 golangci-lint issues** (44 errcheck, 4 staticcheck, 2 ineffassign, 1 unused) → Use `--new-from-rev` baseline
- **Race condition** in `TestAsyncDetector_SensorIntegrationReceivesFrames` → Add skip annotation with TODO
- **pkg/conversion at 74.6% coverage** → Exception for conversion package, 80% for others
- **go.work references ../nevr-common** → Set `GOWORK=off` in CI environment

**Guardrails Applied**:
- Maximum 3 workflow files (no proliferation)
- Hook scripts location: `scripts/hooks/` (not `_local/`, which is test data)
- Benchmarks run on main branch only (not PRs, due to CI machine variance)
- Hooks use locally-installed tools only (no downloading binaries)
- No Makefile unless explicitly requested
- No modifications to Go source code EXCEPT: race test skip annotation for `TestAsyncDetector_SensorIntegrationReceivesFrames` (single t.Skip line with env guard)

---

## Work Objectives

### Core Objective
Set up production-grade CI/CD infrastructure that enforces code quality standards while accommodating pre-existing technical debt through baseline exceptions.

### Concrete Deliverables
- `.github/workflows/pr.yml` — PR validation workflow
- `.github/workflows/main.yml` — Main branch CI with benchmarks
- `.github/dependabot.yml` — Dependency management
- `.golangci.yml` — Linter configuration
- `scripts/hooks/pre-commit` — Pre-commit hook script
- `scripts/hooks/pre-push` — Pre-push hook script
- `scripts/install-hooks.sh` — Hook installer
- `.benchmarks/baseline.txt` — Initial benchmark baseline
- `docs/branch-protection-setup.md` — GitHub settings guide

### Definition of Done
- [ ] All GitHub Actions workflows parse and validate: `gh workflow list` shows 2 workflows
- [ ] golangci-lint config is valid: `golangci-lint config verify` exits 0
- [ ] Git hooks install correctly: `bash scripts/install-hooks.sh && ls -la .git/hooks/pre-commit .git/hooks/pre-push` shows executable files
- [ ] Pre-commit hook catches format issues: Test by adding unformatted code and attempting commit
- [ ] Pre-push hook catches failing tests: Test by breaking a test and attempting push
- [ ] CI builds without go.work: `GOWORK=off go build ./...` succeeds
- [ ] Benchmark baseline file exists: `test -f .benchmarks/baseline.txt && wc -l .benchmarks/baseline.txt` (should have benchmark results)

### Must Have
- GitHub Actions workflows for PR and main branch
- golangci-lint configuration with baseline for pre-existing issues
- Git hooks for pre-commit and pre-push validation
- Coverage enforcement (80% for codecs/events/processing, 74% for conversion)
- Benchmark regression detection (5% threshold)
- Dependabot configuration for automated updates
- govulncheck integration for vulnerability scanning

### Must NOT Have (Guardrails)
- ❌ No Makefile (use direct commands in workflows and hooks)
- ❌ No workflow proliferation (maximum 3 workflow files)
- ❌ No modifications to existing Go source code EXCEPT: single t.Skip() line with env guard for race test (minimal, documented exception)
- ❌ No custom benchmark framework (use `benchstat` standard tool)
- ❌ No binary downloads in git hooks (use locally-installed tools only)
- ❌ No golangci-lint config bloat (8 linters only, baseline for pre-existing)
- ❌ No fixing of 51 existing lint issues (out of scope, tracked separately)
- ❌ No fixing of race condition itself (out of scope, tracked separately — only adding skip annotation)
- ❌ No new tests to increase coverage (out of scope, tracked separately)

---

## Verification Strategy

> **UNIVERSAL RULE: ZERO HUMAN INTERVENTION**
>
> ALL tasks in this plan MUST be verifiable WITHOUT any human action.
> This is NOT conditional — it applies to EVERY task, regardless of test strategy.
>
> **FORBIDDEN** — acceptance criteria that require:
> - "User manually tests..." / "Manual browser verification"
> - "User visually confirms..." / "Check GitHub UI"
> - "Ask user to verify..." / "Human approval needed"
> - ANY step where a human must perform an action
>
> **ALL verification is executed by the agent** using tools (Bash, interactive_bash, etc.). No exceptions.

### Test Decision
- **Infrastructure exists**: YES (25 test files, 4 benchmark files)
- **Automated tests**: Tests-after (verify CI/hooks work correctly)
- **Framework**: Go's built-in testing (`go test`)

### Agent-Executed QA Scenarios (MANDATORY — ALL tasks)

> Whether TDD is enabled or not, EVERY task MUST include Agent-Executed QA Scenarios.
> These describe how the executing agent DIRECTLY verifies the deliverable by running it.

**Verification Tool by Deliverable Type:**

| Type | Tool | How Agent Verifies |
|------|------|-------------------|
| **YAML Config** | Bash (python/yq) | Parse YAML, validate structure, check required fields |
| **Shell Script** | Bash (shellcheck) | Run shellcheck, execute with --help flag, test with dry-run |
| **Git Hook** | Bash | Install hook, create test commit/push, verify blocking behavior |
| **GitHub Workflow** | Bash (gh CLI + act) | Validate with `gh workflow list`, test with `act` locally if possible |

---

## Execution Strategy

### Parallel Execution Waves

> Maximize throughput by grouping independent tasks into parallel waves.

```
Wave 1 (Config Foundation - Start Immediately):
├── Task 1: golangci-lint config (.golangci.yml)
├── Task 2: Dependabot config (.github/dependabot.yml)
└── Task 8: Branch protection documentation (docs/branch-protection-setup.md)

Wave 2 (CI Workflows - After Wave 1):
├── Task 3: PR validation workflow (.github/workflows/pr.yml)
└── Task 4: Main branch CI workflow (.github/workflows/main.yml)

Wave 3 (Git Hooks - After Wave 1):
├── Task 5: Pre-commit hook (scripts/hooks/pre-commit)
├── Task 6: Pre-push hook (scripts/hooks/pre-push)
└── Task 7: Hook installer (scripts/install-hooks.sh)

Wave 4 (Benchmarks - After Wave 2):
└── Task 9: Generate benchmark baseline (.benchmarks/baseline.txt)

Wave 5 (Integration Testing - After All):
└── Task 10: End-to-end verification of CI/CD system

Critical Path: Task 1 → Task 3 → Task 9 → Task 10
Parallel Speedup: ~60% faster than sequential
```

### Dependency Matrix

| Task | Depends On | Blocks | Can Parallelize With |
|------|------------|--------|---------------------|
| 1 | None | 3, 5 | 2, 8 |
| 2 | None | 3 | 1, 8 |
| 3 | 1, 2 | 9, 10 | 4 |
| 4 | 1, 2 | 9, 10 | 3 |
| 5 | 1 | 10 | 6, 7 |
| 6 | 1 | 10 | 5, 7 |
| 7 | None | 10 | 5, 6 |
| 8 | None | None | 1, 2 |
| 9 | 3, 4 | 10 | None |
| 10 | All | None | None (final) |

### Agent Dispatch Summary

| Wave | Tasks | Recommended Category | Skills |
|------|-------|---------------------|--------|
| 1 | 1, 2, 8 | quick | [] |
| 2 | 3, 4 | unspecified-low | [] |
| 3 | 5, 6, 7 | quick | [] |
| 4 | 9 | quick | [] |
| 5 | 10 | unspecified-low | [] |

---

## TODOs

- [ ] 1. Create golangci-lint configuration with baseline

  **What to do**:
  - Create `.golangci.yml` at repository root
  - Enable 8 comprehensive linters: gofmt, govet, errcheck, staticcheck, ineffassign, unused, gosec, revive
  - Configure `--new-from-rev` baseline to ignore 51 pre-existing issues
  - Set timeout to 5 minutes
  - Exclude test files from certain linters (gosec, revive)
  - Add issue exclusions for known false positives if necessary

  **Must NOT do**:
  - Don't enable more than the 8 specified linters (no config bloat)
  - Don't add custom exclude patterns beyond baseline (keep it simple)
  - Don't fix existing lint issues (out of scope)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Single config file creation with well-defined structure
  - **Skills**: []
    - No specialized skills needed (standard YAML config)
  - **Skills Evaluated but Omitted**:
    - None (straightforward config task)

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 2, 8)
  - **Blocks**: Tasks 3, 4, 5, 6 (workflows and hooks need lint config reference)
  - **Blocked By**: None (can start immediately)

  **References**:

  **Pattern References**:
  - None (creating new file, no existing pattern)

  **API/Type References**:
  - golangci-lint v2 config docs: https://golangci-lint.run/usage/configuration/
  - New issues mode: https://golangci-lint.run/usage/linters/#new-from-rev

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — User's requirements (8 linters, baseline approach)

  **External References**:
  - golangci-lint v2.8.0 config schema: https://golangci-lint.run/usage/configuration/#config-file
  - Example config: https://github.com/golangci/golangci-lint/blob/master/.golangci.example.yml

  **WHY Each Reference Matters**:
  - golangci-lint docs: Understand v2 YAML schema (different from v1)
  - New issues mode: Configure `--new-from-rev` to ignore 51 pre-existing issues
  - Draft file: Contains Metis findings about specific issue counts

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Config file is valid YAML and golangci-lint accepts it
    Tool: Bash
    Preconditions: golangci-lint v2.8.0 installed, .golangci.yml created
    Steps:
      1. python3 -c "import yaml; yaml.safe_load(open('.golangci.yml'))"
      2. Assert: Exit code 0 (valid YAML)
      3. golangci-lint config verify
      4. Assert: Exit code 0 (valid config)
      5. cat .golangci.yml
      6. Assert: Contains "gofmt", "govet", "errcheck", "staticcheck", "ineffassign", "unused", "gosec", "revive"
    Expected Result: Config parses and golangci-lint validates it
    Evidence: Command output showing successful validation

  Scenario: Config enables new-issues-only mode
    Tool: Bash
    Preconditions: .golangci.yml created
    Steps:
      1. grep -A5 "new-from-rev" .golangci.yml
      2. Assert: Output contains "new-from-rev" configuration
      3. golangci-lint run --new-from-rev=HEAD ./... 2>&1 | tee lint-output.txt
      4. Assert: Exit code 0 or specific issues only (not 51 pre-existing)
    Expected Result: Only new issues are reported, not pre-existing 51
    Evidence: lint-output.txt showing limited/zero issues
  ```

  **Commit**: YES
  - Message: `chore(ci): add golangci-lint configuration with baseline`
  - Files: `.golangci.yml`
  - Pre-commit: `golangci-lint config verify`

---

- [ ] 2. Create Dependabot configuration

  **What to do**:
  - Create `.github/dependabot.yml`
  - Enable `gomod` ecosystem updates
  - Schedule: weekly updates
  - Target branch: main
  - Reviewers: none (optional, user can add via GitHub UI later)
  - Assignees: none (optional)
  - Labels: dependencies, automated
  - Commit message prefix: `chore(deps):`
  - Open PR limit: 5 (prevent flood)
  - Enable govulncheck integration (GitHub's native Go vulnerability scanning)

  **Must NOT do**:
  - Don't enable auto-merge (user preference not confirmed)
  - Don't configure reviewers/assignees (can be added later via GitHub UI)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Single YAML config file with well-defined Dependabot schema
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 8)
  - **Blocks**: Tasks 3 (PR workflow references dependabot)
  - **Blocked By**: None (can start immediately)

  **References**:

  **Pattern References**:
  - None (creating new file)

  **Documentation References**:
  - GitHub Dependabot docs: https://docs.github.com/en/code-security/dependabot/dependabot-version-updates/configuration-options-for-the-dependabot.yml-file

  **External References**:
  - Dependabot gomod config: https://docs.github.com/en/code-security/dependabot/dependabot-version-updates/configuration-options-for-the-dependabot.yml-file#package-ecosystem
  - govulncheck integration: https://go.dev/security/vuln/

  **WHY Each Reference Matters**:
  - Dependabot docs: Understand YAML schema for gomod ecosystem
  - govulncheck: Native Go vulnerability scanning (user requested Dependabot + govulncheck)

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Dependabot config is valid YAML
    Tool: Bash
    Preconditions: .github/dependabot.yml created
    Steps:
      1. python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
      2. Assert: Exit code 0 (valid YAML)
      3. cat .github/dependabot.yml
      4. Assert: Contains "package-ecosystem: gomod"
      5. Assert: Contains "schedule:" with "interval:"
      6. Assert: Contains "open-pull-requests-limit: 5"
    Expected Result: Valid YAML with gomod ecosystem configured
    Evidence: Command output showing valid YAML structure

  Scenario: Config follows GitHub Dependabot schema
    Tool: Bash
    Preconditions: .github/dependabot.yml created
    Steps:
      1. grep "version:" .github/dependabot.yml
      2. Assert: Contains "version: 2" (Dependabot v2 schema)
      3. grep "directory:" .github/dependabot.yml
      4. Assert: Contains "directory: /" (root directory for go.mod)
    Expected Result: Schema version and directory correctly specified
    Evidence: grep output showing required fields
  ```

  **Commit**: YES
  - Message: `chore(ci): add Dependabot configuration for Go dependencies`
  - Files: `.github/dependabot.yml`
  - Pre-commit: `python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"`

---

- [ ] 3. Create PR validation workflow

  **What to do**:
  - **FIRST**: Add race test skip to `pkg/events/event_detector_test.go` in `TestAsyncDetector_SensorIntegrationReceivesFrames`:
    - At start of test function body, add: `if os.Getenv("SKIP_RACE_FLAKY") == "1" { t.Skip("Skipping flaky race test") }`
    - Add import: `"os"` if not already present
    - This allows CI to skip the known flaky race condition
  - **THEN**: Create `.github/workflows/pr.yml`
  - Trigger: on pull_request (opened, synchronize, reopened)
  - Jobs:
    1. **lint**: Run golangci-lint with `--new-from-rev=origin/main` (ignore pre-existing issues)
    2. **test**: Run `go test ./...` with coverage profile
    3. **coverage**: Check coverage thresholds (80% for codecs/events/processing, 74.6% for conversion)
    4. **race**: Run `go test -race ./...` (first add skip annotation to test: wrap test body with `if os.Getenv("SKIP_RACE_FLAKY") == "1" { t.Skip() }` and set env `SKIP_RACE_FLAKY=1` in workflow)
    5. **vuln**: Run `govulncheck ./...`
  - Environment: Set `GOWORK=off` (go.work references local sibling directory)
  - Go version: 1.25
  - Cache: go modules cache enabled
  - PR comment: Post coverage report as PR comment
  - Status checks: All jobs must pass for PR to be mergeable

  **Must NOT do**:
  - Don't run benchmarks on PR (too slow, CI machine variance causes false positives)
  - Don't fail on pre-existing 51 lint issues (use baseline)
  - Don't enforce 80% on conversion package (use 74.6% exception)

  **Recommended Agent Profile**:
  - **Category**: `unspecified-low`
    - Reason: Multi-job workflow with moderate complexity, not trivial but not deep
  - **Skills**: []
    - No specialized skills needed (standard GitHub Actions)
  - **Skills Evaluated but Omitted**:
    - git-master: Not needed (no git history manipulation)

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Task 4)
  - **Blocks**: Tasks 9, 10 (benchmark baseline and integration testing)
  - **Blocked By**: Tasks 1, 2 (needs lint config and dependabot config)

  **References**:

  **Pattern References**:
  - None (no existing workflows)

  **API/Type References**:
  - GitHub Actions workflow syntax: https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Coverage thresholds, lint baseline, race condition skip
  - `go.mod` — Go version (1.25), dependencies

  **External References**:
  - golangci-lint-action: https://github.com/golangci/golangci-lint-action (use v7 for golangci-lint v2)
  - actions/setup-go: https://github.com/actions/setup-go
  - govulncheck: https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck

  **WHY Each Reference Matters**:
  - golangci-lint-action v7: Supports golangci-lint v2.8.0 (current version)
  - Draft file: Contains Metis findings (coverage exceptions, race skip, GOWORK=off requirement)
  - go.mod: Specifies Go 1.25 requirement

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Workflow file is valid YAML
    Tool: Bash
    Preconditions: .github/workflows/pr.yml created
    Steps:
      1. python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"
      2. Assert: Exit code 0 (valid YAML)
      3. cat .github/workflows/pr.yml | grep "GOWORK"
      4. Assert: Contains "GOWORK=off" or "GOWORK: off"
    Expected Result: Valid workflow YAML with GOWORK=off
    Evidence: YAML parse success and grep output showing GOWORK

  Scenario: Workflow has all required jobs
    Tool: Bash
    Preconditions: .github/workflows/pr.yml created
    Steps:
      1. cat .github/workflows/pr.yml
      2. Assert: Contains "jobs:" section
      3. grep -E "^\s+(lint|test|coverage|race|vuln):" .github/workflows/pr.yml | wc -l
      4. Assert: Output is 5 (all 5 jobs defined)
      5. grep "golangci-lint-action" .github/workflows/pr.yml
      6. Assert: Uses golangci-lint-action (version v7 or later)
    Expected Result: All 5 jobs present with correct tools
    Evidence: grep output showing job definitions

  Scenario: Coverage job has package-specific thresholds
    Tool: Bash
    Preconditions: .github/workflows/pr.yml created
    Steps:
      1. grep -A10 "coverage:" .github/workflows/pr.yml
      2. Assert: Contains logic for per-package coverage checking
      3. grep "74.6" .github/workflows/pr.yml
      4. Assert: Contains exception for conversion package at 74.6%
      5. grep "80" .github/workflows/pr.yml
      6. Assert: Contains 80% threshold for other packages
    Expected Result: Coverage thresholds match Metis recommendations
    Evidence: grep output showing threshold configurations
  ```

  **Commit**: YES
  - Message: `feat(ci): add PR validation workflow with lint/test/coverage/race`
  - Files: `.github/workflows/pr.yml`
  - Pre-commit: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"`

---

- [ ] 4. Create main branch CI workflow

  **What to do**:
  - Create `.github/workflows/main.yml`
  - Trigger: on push to main branch
  - Jobs:
    1. **test**: Run full test suite with coverage
    2. **race**: Run race detector
    3. **benchmark**: Run benchmarks with `benchstat` comparison
    4. **vuln**: Run govulncheck
  - Benchmark comparison:
    - Run: `go test -bench=. -benchmem -count=5 ./... | tee current-bench.txt`
    - Compare: `benchstat .benchmarks/baseline.txt current-bench.txt`
    - Fail: If any benchmark shows >5% regression (check benchstat output for "~" or "+" delta indicators)
    - Update: If no regression, upload updated baseline as workflow artifact (manual PR creation for baseline updates)
  - Environment: Set `GOWORK=off`
  - Go version: 1.25
  - Cache: go modules cache enabled
  - Notifications: Post results as commit status

  **Must NOT do**:
  - Don't run benchmarks on every PR (only on main, per Metis)
  - Don't fail fast (run all jobs even if one fails, for complete visibility)

  **Recommended Agent Profile**:
  - **Category**: `unspecified-low`
    - Reason: Similar to PR workflow but adds benchmark logic (moderate complexity)
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Task 3)
  - **Blocks**: Tasks 9, 10 (benchmark baseline and integration)
  - **Blocked By**: Tasks 1, 2 (needs lint config)

  **References**:

  **Pattern References**:
  - `.github/workflows/pr.yml` — Similar structure, adapt for main branch

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — 5% regression threshold, benchstat tool, baseline storage

  **External References**:
  - benchstat tool: https://pkg.go.dev/golang.org/x/perf/cmd/benchstat
  - benchstat usage: https://go.dev/blog/benchstat
  - Go benchmark docs: https://pkg.go.dev/testing#hdr-Benchmarks

  **WHY Each Reference Matters**:
  - benchstat: Standard Go tool for statistically sound benchmark comparison
  - Draft file: Contains 5% regression threshold requirement
  - PR workflow: Reuse job structure for consistency

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Workflow triggers only on push to main
    Tool: Bash
    Preconditions: .github/workflows/main.yml created
    Steps:
      1. cat .github/workflows/main.yml
      2. grep -A3 "on:" .github/workflows/main.yml
      3. Assert: Contains "push:" with "branches: [main]" or equivalent
      4. grep "pull_request" .github/workflows/main.yml
      5. Assert: Exit code 1 (no pull_request trigger)
    Expected Result: Workflow only runs on main branch pushes
    Evidence: grep output showing trigger configuration

  Scenario: Benchmark job uses benchstat with 5% threshold
    Tool: Bash
    Preconditions: .github/workflows/main.yml created
    Steps:
      1. grep -A20 "benchmark:" .github/workflows/main.yml
      2. Assert: Contains "benchstat" command
      3. Assert: Contains ".benchmarks/baseline.txt" reference
      4. Assert: Contains logic for 5% regression detection via benchstat output parsing
      5. grep "count=5" .github/workflows/main.yml
      6. Assert: Benchmarks run with count=5 for statistical significance
    Expected Result: Benchmark comparison configured correctly
    Evidence: grep output showing benchstat usage

  Scenario: Workflow uploads baseline as artifact (no auto-commit)
    Tool: Bash
    Preconditions: .github/workflows/main.yml created
    Steps:
      1. grep -A30 "benchmark:" .github/workflows/main.yml
      2. Assert: Contains "actions/upload-artifact" for baseline
      3. grep "git commit\|git push" .github/workflows/main.yml || echo "No git commands found"
      4. Assert: Exit code 1 (no auto-commit to avoid branch protection conflict)
    Expected Result: Baseline uploaded as artifact, not auto-committed
    Evidence: grep output showing artifact upload, no git commands
  ```

  **Commit**: YES
  - Message: `feat(ci): add main branch workflow with benchmarks and regression detection`
  - Files: `.github/workflows/main.yml`
  - Pre-commit: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/main.yml'))"`

---

- [ ] 5. Create pre-commit git hook

  **What to do**:
  - Create `scripts/hooks/pre-commit` (executable bash script)
  - Checks (in order):
    1. **Format**: Run `go fmt ./...` and check for changes → If changes, fail with message "Run 'go fmt ./...' and re-stage"
    2. **Lint**: Run `golangci-lint run --new-from-rev=HEAD~1 ./...` → If issues, fail with error list
    3. **go.mod**: Run `go mod tidy` and check for changes → If changes, fail with message "Run 'go mod tidy' and re-stage"
  - Exit codes: 0 = pass, 1 = fail (blocks commit)
  - Fast execution: Should complete in <10 seconds for typical changes
  - Informative errors: Tell user exactly what to fix

  **Must NOT do**:
  - Don't run tests (too slow for pre-commit, that's pre-push)
  - Don't download/install tools (use locally-installed tools only)
  - Don't run on all files (only staged files or changed packages)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Single bash script with well-defined logic
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 6, 7)
  - **Blocks**: Task 10 (integration testing)
  - **Blocked By**: Task 1 (needs golangci-lint config)

  **References**:

  **Pattern References**:
  - None (creating new hook)

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Hook requirements (format, lint, go.mod)
  - Git hooks docs: https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks

  **External References**:
  - Pre-commit hook examples: https://github.com/aitemr/awesome-git-hooks
  - go fmt: https://pkg.go.dev/cmd/gofmt
  - golangci-lint: https://golangci-lint.run/usage/quick-start/

  **WHY Each Reference Matters**:
  - Draft file: Specifies exact checks required (format, lint, go.mod)
  - Git hooks docs: Understand hook execution environment and exit codes

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Hook script is executable and has correct shebang
    Tool: Bash
    Preconditions: scripts/hooks/pre-commit created
    Steps:
      1. ls -la scripts/hooks/pre-commit
      2. Assert: Permissions include execute bit (x)
      3. head -1 scripts/hooks/pre-commit
      4. Assert: First line is "#!/usr/bin/env bash" or "#!/bin/bash"
      5. shellcheck scripts/hooks/pre-commit
      6. Assert: Exit code 0 (no shell script errors)
    Expected Result: Valid executable bash script
    Evidence: ls output and shellcheck validation

  Scenario: Hook catches unformatted code
    Tool: Bash
    Preconditions: scripts/hooks/pre-commit created, installed to .git/hooks/
    Steps:
      1. echo "package test
func test(){println(1)}" > /tmp/test_unformatted.go
      2. cp /tmp/test_unformatted.go pkg/codecs/temp_test.go
      3. git add pkg/codecs/temp_test.go
      4. git commit -m "test" 2>&1 | tee hook-output.txt
      5. Assert: Exit code 1 (commit blocked)
      6. grep -i "fmt" hook-output.txt
      7. Assert: Output mentions formatting issue
      8. git reset HEAD && rm pkg/codecs/temp_test.go
    Expected Result: Commit blocked with format error
    Evidence: hook-output.txt showing format failure

  Scenario: Hook catches go.mod inconsistencies
    Tool: Bash
    Preconditions: scripts/hooks/pre-commit created, installed
    Steps:
      1. echo "require github.com/fake/dep v1.0.0" >> go.mod
      2. git add go.mod
      3. git commit -m "test" 2>&1 | tee hook-output.txt
      4. Assert: Exit code 1 (commit blocked)
      5. grep -i "mod tidy" hook-output.txt
      6. Assert: Output mentions go mod tidy
      7. git checkout -- go.mod
    Expected Result: Commit blocked with go.mod error
    Evidence: hook-output.txt showing mod tidy requirement
  ```

  **Commit**: YES
  - Message: `feat(hooks): add pre-commit hook for format/lint/go.mod checks`
  - Files: `scripts/hooks/pre-commit`
  - Pre-commit: `shellcheck scripts/hooks/pre-commit`

---

- [ ] 6. Create pre-push git hook

  **What to do**:
  - Create `scripts/hooks/pre-push` (executable bash script)
  - Checks (in order):
    1. **Tests**: Run `go test ./...` → If fail, block push with error details
    2. **Race**: Run `go test -race ./...` with env `SKIP_RACE_FLAKY=1` (skips the flaky `TestAsyncDetector_SensorIntegrationReceivesFrames` via environment check in test code) → If fail, block push
    3. **Main branch protection**: Check if pushing to main directly → If yes and current branch is not main, fail with "Direct pushes to main not allowed. Create a PR."
  - Parse git hook arguments: `$1` is remote name, `$2` is remote URL, stdin has refs being pushed
  - Smarter execution: Only run tests for packages with staged changes (optional optimization)
  - Timeout: Set reasonable timeout (2 minutes) to prevent hanging

  **Must NOT do**:
  - Don't run benchmarks (too slow, that's in CI)
  - Don't run coverage (that's in CI)
  - Don't download tools

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Bash script similar to pre-commit but with git ref parsing
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - git-master: Not needed (simple ref checking, not history manipulation)

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 5, 7)
  - **Blocks**: Task 10 (integration testing)
  - **Blocked By**: Task 1 (needs test suite to be runnable)

  **References**:

  **Pattern References**:
  - None (creating new hook)

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Hook requirements (tests, race, main protection)
  - Git pre-push hook docs: https://git-scm.com/docs/githooks#_pre_push

  **External References**:
  - Pre-push hook examples: https://github.com/git-hooks/git-hooks/blob/master/pre-push.sample
  - go test flags: https://pkg.go.dev/cmd/go#hdr-Testing_flags

  **WHY Each Reference Matters**:
  - Draft file: Specifies main branch protection and race detector requirement
  - Git pre-push docs: Understand hook arguments (remote name/URL) and stdin format (refs)

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Hook blocks push with failing tests
    Tool: Bash
    Preconditions: scripts/hooks/pre-push created, installed
    Steps:
      1. echo "package test
import \"testing\"
func TestFail(t *testing.T) { t.Fatal(\"fail\") }" > pkg/codecs/temp_fail_test.go
      2. git add pkg/codecs/temp_fail_test.go && git commit -m "temp test"
      3. git push origin HEAD 2>&1 | tee hook-output.txt
      4. Assert: Exit code 1 (push blocked)
      5. grep -i "test.*fail" hook-output.txt
      6. Assert: Output shows test failure
      7. git reset --hard HEAD~1 && rm pkg/codecs/temp_fail_test.go
    Expected Result: Push blocked with test failure message
    Evidence: hook-output.txt showing test failure

  Scenario: Hook blocks direct push to main
    Tool: Bash
    Preconditions: scripts/hooks/pre-push created, installed, not on main branch
    Steps:
      1. git checkout -b temp-feature-branch
      2. echo "# test" >> README.md && git add README.md && git commit -m "test"
      3. git push origin main 2>&1 | tee hook-output.txt
      4. Assert: Exit code 1 (push blocked)
      5. grep -i "direct push.*main" hook-output.txt
      6. Assert: Output mentions main branch protection
      7. git checkout main && git branch -D temp-feature-branch
    Expected Result: Push to main blocked from feature branch
    Evidence: hook-output.txt showing main protection message

  Scenario: Hook allows push with passing tests
    Tool: Bash
    Preconditions: scripts/hooks/pre-push created, installed
    Steps:
      1. echo "# test" >> README.md && git add README.md && git commit -m "test"
      2. git push --dry-run origin HEAD 2>&1 | tee hook-output.txt
      3. Assert: Exit code 0 (push allowed in dry-run)
      4. grep -v "error\|fail" hook-output.txt || true
      5. Assert: No error messages in output
      6. git reset --hard HEAD~1
    Expected Result: Push allowed when tests pass
    Evidence: hook-output.txt showing successful execution
  ```

  **Commit**: YES
  - Message: `feat(hooks): add pre-push hook for tests/race/main-protection`
  - Files: `scripts/hooks/pre-push`
  - Pre-commit: `shellcheck scripts/hooks/pre-push`

---

- [ ] 7. Create hook installation script

  **What to do**:
  - Create `scripts/install-hooks.sh` (executable bash script)
  - Actions:
    1. Check if `.git/` directory exists → If not, fail with "Not a git repository"
    2. Copy `scripts/hooks/pre-commit` to `.git/hooks/pre-commit`
    3. Copy `scripts/hooks/pre-push` to `.git/hooks/pre-push`
    4. Set executable permissions: `chmod +x .git/hooks/pre-commit .git/hooks/pre-push`
    5. Verify installation: Check both files exist and are executable
    6. Print success message with instructions
  - Idempotent: Running multiple times should be safe (overwrites existing hooks)
  - Uninstall option: Add `--uninstall` flag to remove hooks

  **Must NOT do**:
  - Don't install hooks globally (only for current repo)
  - Don't modify existing hook files beyond copying
  - Don't require root/sudo

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Simple bash script with file copying logic
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 5, 6)
  - **Blocks**: Task 10 (integration testing)
  - **Blocked By**: None (can start immediately, but logically after hooks are written)

  **References**:

  **Pattern References**:
  - None (creating new script)

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Hook installation requirements
  - Git hooks location: `.git/hooks/` directory

  **External References**:
  - Git hooks: https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks
  - Bash best practices: https://google.github.io/styleguide/shellguide.html

  **WHY Each Reference Matters**:
  - Draft file: Specifies local + CI execution (installer enables local)
  - Git hooks docs: Understand `.git/hooks/` directory structure

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Script installs hooks successfully
    Tool: Bash
    Preconditions: scripts/install-hooks.sh created, hooks exist
    Steps:
      1. rm -f .git/hooks/pre-commit .git/hooks/pre-push
      2. bash scripts/install-hooks.sh 2>&1 | tee install-output.txt
      3. Assert: Exit code 0 (successful installation)
      4. ls -la .git/hooks/pre-commit .git/hooks/pre-push
      5. Assert: Both files exist and have execute bit
      6. grep -i "success\|installed" install-output.txt
      7. Assert: Success message printed
    Expected Result: Hooks installed and executable
    Evidence: install-output.txt and ls showing installed hooks

  Scenario: Script is idempotent (can run multiple times)
    Tool: Bash
    Preconditions: Hooks already installed
    Steps:
      1. bash scripts/install-hooks.sh 2>&1 | tee install-output-2.txt
      2. Assert: Exit code 0
      3. ls -la .git/hooks/pre-commit .git/hooks/pre-push
      4. Assert: Both files still exist and executable
    Expected Result: Re-running installer doesn't break anything
    Evidence: install-output-2.txt and ls showing hooks intact

  Scenario: Uninstall flag removes hooks
    Tool: Bash
    Preconditions: Hooks installed
    Steps:
      1. bash scripts/install-hooks.sh --uninstall 2>&1 | tee uninstall-output.txt
      2. Assert: Exit code 0
      3. ls .git/hooks/pre-commit 2>&1 || echo "pre-commit not found"
      4. Assert: pre-commit hook removed or disabled
      5. ls .git/hooks/pre-push 2>&1 || echo "pre-push not found"
      6. Assert: pre-push hook removed or disabled
    Expected Result: Hooks removed or disabled
    Evidence: uninstall-output.txt and ls showing missing hooks
  ```

  **Commit**: YES
  - Message: `feat(hooks): add hook installation script with uninstall support`
  - Files: `scripts/install-hooks.sh`
  - Pre-commit: `shellcheck scripts/install-hooks.sh`

---

- [ ] 8. Create branch protection documentation

  **What to do**:
  - Create `docs/branch-protection-setup.md`
  - Document GitHub branch protection settings for main branch:
    1. Require pull request before merging: YES
    2. Required approvals: 1
    3. Require status checks to pass: YES
    4. Required status checks: All jobs from `.github/workflows/pr.yml` (lint, test, coverage, race, vuln)
    5. Require branches to be up to date: YES
    6. Restrict who can push: NO (hooks + CI are sufficient)
    7. Allow force pushes: NO
    8. Allow deletions: NO
  - Include step-by-step instructions with screenshots placeholders
  - Include GitHub CLI commands as alternative: `gh api repos/{owner}/{repo}/branches/main/protection --method PUT --input protection.json`
  - Include `protection.json` template with all settings

  **Must NOT do**:
  - Don't automate branch protection setup (requires admin token, out of scope)
  - Don't include actual screenshots (placeholders only)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Markdown documentation with well-defined structure
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 2)
  - **Blocks**: None (documentation only)
  - **Blocked By**: None (can start immediately)

  **References**:

  **Pattern References**:
  - None (creating new documentation)

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Branch protection requirements (1 approval, passing CI)

  **External References**:
  - GitHub branch protection: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches
  - GitHub API: https://docs.github.com/en/rest/branches/branch-protection

  **WHY Each Reference Matters**:
  - Draft file: Specifies exact protection requirements (1 approval, all checks pass)
  - GitHub docs: Understand available protection options and API schema

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Documentation file exists and is valid markdown
    Tool: Bash
    Preconditions: docs/branch-protection-setup.md created
    Steps:
      1. ls docs/branch-protection-setup.md
      2. Assert: File exists
      3. cat docs/branch-protection-setup.md | wc -l
      4. Assert: File has >20 lines (substantive content)
      5. grep -i "pull request" docs/branch-protection-setup.md
      6. Assert: Mentions pull request requirement
      7. grep -i "status check" docs/branch-protection-setup.md
      8. Assert: Mentions required status checks
    Expected Result: Complete documentation with key settings
    Evidence: File content with required sections

  Scenario: Documentation includes GitHub CLI commands
    Tool: Bash
    Preconditions: docs/branch-protection-setup.md created
    Steps:
      1. grep "gh api" docs/branch-protection-setup.md
      2. Assert: Contains gh CLI commands
      3. grep "protection.json" docs/branch-protection-setup.md
      4. Assert: References protection.json template
    Expected Result: Alternative automation approach documented
    Evidence: grep output showing CLI commands

  Scenario: protection.json template is valid JSON
    Tool: Bash
    Preconditions: docs/branch-protection-setup.md created with embedded JSON or separate file
    Steps:
      1. grep -Pzo '(?s)\{.*\}' docs/branch-protection-setup.md > /tmp/protection-test.json || echo '{"test": true}' > /tmp/protection-test.json
      2. python3 -c "import json; json.load(open('/tmp/protection-test.json'))"
      3. Assert: Exit code 0 (valid JSON if embedded)
    Expected Result: JSON template is syntactically valid
    Evidence: Python validation success
  ```

  **Commit**: YES
  - Message: `docs(ci): add GitHub branch protection setup guide`
  - Files: `docs/branch-protection-setup.md`
  - Pre-commit: None (documentation only)

---

- [ ] 9. Generate initial benchmark baseline

  **What to do**:
  - Create `.benchmarks/` directory
  - Run benchmarks: `go test -bench=. -benchmem -count=5 ./... | tee .benchmarks/baseline.txt`
  - Verify baseline is in standard Go benchmark format (benchstat-compatible text format)
  - Verify baseline contains all benchmarks:
    - `BenchmarkAsyncDetector_ProcessFrame`
    - `BenchmarkReadFrameTo`
    - `BenchmarkOptimizedWriteFrame`
    - Event detection micro-benchmarks
  - Add `.benchmarks/baseline.txt` to git (committed as baseline)
  - Document in README or docs: How to update baseline (run benchmarks, create PR with updated baseline.txt)

  **Must NOT do**:
  - Don't convert to JSON (benchstat works with text format directly)
  - Don't run benchmarks with -benchtime too long (default is fine)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Single command execution + directory setup
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after workflows)
  - **Parallel Group**: Wave 4 (alone)
  - **Blocks**: Task 10 (integration testing)
  - **Blocked By**: Tasks 3, 4 (workflows reference baseline)

  **References**:

  **Pattern References**:
  - None (creating new baseline)

  **Test References**:
  - `pkg/events/events_bench_test.go` — BenchmarkAsyncDetector_ProcessFrame
  - `pkg/codecs/codec_echoreplay_bench_test.go` — BenchmarkReadFrameTo, BenchmarkOptimizedWriteFrame
  - `pkg/events/event_detection_bench_test.go` — Event detection micro-benchmarks

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Benchmark baseline storage strategy

  **External References**:
  - benchstat: https://pkg.go.dev/golang.org/x/perf/cmd/benchstat
  - Go benchmarks: https://pkg.go.dev/testing#hdr-Benchmarks

  **WHY Each Reference Matters**:
  - Test files: Verify all benchmarks are captured in baseline
  - benchstat: Understand output format for baseline storage

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Baseline file is created and valid
    Tool: Bash
    Preconditions: Benchmarks run, .benchmarks/baseline.txt created
    Steps:
      1. ls .benchmarks/baseline.txt
      2. Assert: File exists
      3. wc -l .benchmarks/baseline.txt
      4. Assert: File has >10 lines (multiple benchmarks)
      5. head -5 .benchmarks/baseline.txt
      6. Assert: Lines start with "Benchmark" (Go benchmark text format)
      7. grep -E "BenchmarkAsyncDetector_ProcessFrame.*ns/op" .benchmarks/baseline.txt
      8. Assert: Contains standard benchmark fields (ns/op, B/op, allocs/op)
    Expected Result: Baseline file in Go benchmark text format
    Evidence: File content showing standard benchmark output

  Scenario: Baseline contains all critical benchmarks
    Tool: Bash
    Preconditions: .benchmarks/baseline.txt created
    Steps:
      1. grep "BenchmarkAsyncDetector_ProcessFrame" .benchmarks/baseline.txt
      2. Assert: Exit code 0 (benchmark present)
      3. grep "BenchmarkReadFrameTo" .benchmarks/baseline.txt
      4. Assert: Exit code 0
      5. grep "BenchmarkOptimizedWriteFrame" .benchmarks/baseline.txt
      6. Assert: Exit code 0
    Expected Result: All key benchmarks in baseline
    Evidence: grep output showing benchmark names

  Scenario: Baseline is tracked in git
    Tool: Bash
    Preconditions: .benchmarks/ directory created with baseline.txt
    Steps:
      1. git check-ignore .benchmarks/baseline.txt 2>&1
      2. Assert: Exit code 1 (baseline.txt is NOT ignored - should be tracked)
      3. ls -la .benchmarks/baseline.txt
      4. Assert: File exists and is readable
      5. git ls-files .benchmarks/baseline.txt
      6. Assert: Exit code 0 (file is tracked in git)
    Expected Result: baseline.txt tracked in git for version control
    Evidence: git ls-files output showing baseline.txt
  ```

  **Commit**: YES
  - Message: `chore(ci): add initial benchmark baseline for regression tracking`
  - Files: `.benchmarks/baseline.txt`, `.benchmarks/.gitkeep`
  - Pre-commit: `ls .benchmarks/baseline.txt && head -5 .benchmarks/baseline.txt`

---

- [ ] 10. End-to-end integration testing

  **What to do**:
  - Verify entire CI/CD system works together
  - Tests:
    1. **Hook installation**: Run `bash scripts/install-hooks.sh` → Verify both hooks installed
    2. **Pre-commit hook**: Create unformatted file, attempt commit → Verify blocked
    3. **Pre-push hook**: Create failing test, attempt push → Verify blocked  
    4. **Workflow YAML validation**: Parse all workflow files with YAML parser → Verify valid syntax
    5. **GOWORK=off build**: Run `GOWORK=off go build ./...` → Verify builds without workspace file
    6. **golangci-lint config**: Run `golangci-lint config verify` → Verify config valid
    7. **Dependabot config**: Parse `.github/dependabot.yml` with YAML parser → Verify valid
    8. **Benchmark baseline**: Verify `.benchmarks/baseline.txt` exists and has standard Go benchmark format
    9. **Branch protection docs**: Verify `docs/branch-protection-setup.md` exists and has >20 lines
  - Document any failures with exact error messages
  - Create summary report of all checks

  **Must NOT do**:
  - Don't actually push to remote (use --dry-run or local testing)
  - Don't trigger actual GitHub Actions runs (local YAML validation only)
  - Don't modify any source files permanently (clean up test changes)

  **Recommended Agent Profile**:
  - **Category**: `unspecified-low`
    - Reason: Multi-step integration testing with moderate complexity
  - **Skills**: []
    - No specialized skills needed
  - **Skills Evaluated but Omitted**:
    - None

  **Parallelization**:
  - **Can Run In Parallel**: NO (final integration, sequential)
  - **Parallel Group**: Wave 5 (alone, after everything)
  - **Blocks**: None (final task)
  - **Blocked By**: All previous tasks (1-9)

  **References**:

  **Pattern References**:
  - Tasks 1-9 acceptance criteria — Reuse verification commands

  **Documentation References**:
  - `.sisyphus/drafts/ci-cd-setup.md` — Complete system requirements

  **External References**:
  - None (integration of all previous work)

  **WHY Each Reference Matters**:
  - Previous tasks: Reuse their acceptance criteria as integration tests
  - Draft file: Verify all original requirements are met

  **Acceptance Criteria**:

  **Agent-Executed QA Scenarios**:

  ```
  Scenario: Complete CI/CD system integration check
    Tool: Bash
    Preconditions: All previous tasks completed
    Steps:
      1. bash scripts/install-hooks.sh
      2. Assert: Exit code 0 (hooks install)
      3. GOWORK=off go build ./...
      4. Assert: Exit code 0 (builds without go.work)
      5. golangci-lint config verify
      6. Assert: Exit code 0 (config valid)
      7. python3 -c "import yaml; [yaml.safe_load(open(f)) for f in ['.github/workflows/pr.yml', '.github/workflows/main.yml']]"
      8. Assert: Exit code 0 (all workflows valid YAML)
      9. python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
      10. Assert: Exit code 0 (valid YAML)
      11. ls .benchmarks/baseline.txt && head -1 .benchmarks/baseline.txt | grep -E "^Benchmark"
      12. Assert: Baseline exists and is Go benchmark format
      13. wc -l docs/branch-protection-setup.md
      14. Assert: >20 lines (substantive documentation)
    Expected Result: All components integrate successfully
    Evidence: Command outputs showing all checks pass

  Scenario: Pre-commit hook integration with golangci-lint
    Tool: Bash
    Preconditions: Hooks installed, golangci-lint config exists
    Steps:
      1. echo "package test
func uncheckedError() { _ = fmt.Println(\"test\") }" > pkg/codecs/temp_integration_test.go
      2. git add pkg/codecs/temp_integration_test.go
      3. git commit -m "integration test" 2>&1 | tee integration-output.txt
      4. Assert: Exit code 1 or 0 (depending on --new-from-rev behavior)
      5. rm pkg/codecs/temp_integration_test.go && git reset HEAD
    Expected Result: Hook runs golangci-lint with baseline config
    Evidence: integration-output.txt showing lint execution

  Scenario: System handles pre-existing issues gracefully
    Tool: Bash
    Preconditions: golangci-lint config with --new-from-rev
    Steps:
      1. golangci-lint run --new-from-rev=HEAD ./... 2>&1 | tee lint-new-issues.txt
      2. Assert: Exit code 0 (no NEW issues, ignores 51 pre-existing)
      3. wc -l lint-new-issues.txt
      4. Assert: Output is minimal (not 51 issues)
    Expected Result: Baseline ignores pre-existing issues
    Evidence: lint-new-issues.txt showing limited output
  ```

  **Commit**: YES
  - Message: `test(ci): verify complete CI/CD system integration`
  - Files: None (testing only, but could add integration test script)
  - Pre-commit: None (final verification task)

---

## Commit Strategy

| After Task | Message | Files | Verification |
|------------|---------|-------|--------------|
| 1 | `chore(ci): add golangci-lint configuration with baseline` | `.golangci.yml` | `golangci-lint config verify` |
| 2 | `chore(ci): add Dependabot configuration for Go dependencies` | `.github/dependabot.yml` | `python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"` |
| 3 | `feat(ci): add PR validation workflow with lint/test/coverage/race` | `.github/workflows/pr.yml` | `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pr.yml'))"` |
| 4 | `feat(ci): add main branch workflow with benchmarks and regression detection` | `.github/workflows/main.yml` | `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/main.yml'))"` |
| 5 | `feat(hooks): add pre-commit hook for format/lint/go.mod checks` | `scripts/hooks/pre-commit` | `shellcheck scripts/hooks/pre-commit` |
| 6 | `feat(hooks): add pre-push hook for tests/race/main-protection` | `scripts/hooks/pre-push` | `shellcheck scripts/hooks/pre-push` |
| 7 | `feat(hooks): add hook installation script with uninstall support` | `scripts/install-hooks.sh` | `shellcheck scripts/install-hooks.sh` |
| 8 | `docs(ci): add GitHub branch protection setup guide` | `docs/branch-protection-setup.md` | None |
| 9 | `chore(ci): add initial benchmark baseline for regression tracking` | `.benchmarks/baseline.txt`, `.gitignore` | `ls .benchmarks/baseline.txt` |
| 10 | `test(ci): verify complete CI/CD system integration` | None | None |

---

## Success Criteria

### Verification Commands
```bash
# All workflows recognized by GitHub
gh workflow list | grep -E "pr|main"  # Expected: 2 workflows

# golangci-lint config valid
golangci-lint config verify  # Expected: Exit code 0

# Git hooks installed
ls -la .git/hooks/pre-commit .git/hooks/pre-push  # Expected: Both executable

# GOWORK=off build succeeds
GOWORK=off go build ./...  # Expected: Exit code 0

# Dependabot config valid
python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"  # Expected: Exit code 0

# Benchmark baseline exists
cat .benchmarks/baseline.txt | head -5  # Expected: Standard Go benchmark text format (BenchmarkXxx ... ns/op)

# Branch protection docs exist
wc -l docs/branch-protection-setup.md  # Expected: >20 lines
```

### Final Checklist
- [ ] All "Must Have" present:
  - [ ] GitHub Actions workflows (PR + main)
  - [ ] golangci-lint config with baseline
  - [ ] Git hooks (pre-commit + pre-push)
  - [ ] Coverage enforcement (80%/74.6%)
  - [ ] Benchmark regression (5% threshold)
  - [ ] Dependabot config
  - [ ] govulncheck integration
- [ ] All "Must NOT Have" absent:
  - [ ] No Makefile
  - [ ] No more than 3 workflow files
  - [ ] No modifications to Go source
  - [ ] No custom benchmark framework
  - [ ] No binary downloads in hooks
- [ ] All acceptance criteria pass:
  - [ ] `gh workflow list` shows 2 workflows
  - [ ] `golangci-lint config verify` exits 0
  - [ ] Hooks install and execute correctly
  - [ ] `GOWORK=off go build ./...` succeeds
  - [ ] Pre-commit catches format issues (tested)
  - [ ] Pre-push catches failing tests (tested)
- [ ] Documentation complete:
  - [ ] Branch protection setup guide exists
  - [ ] Hook installation instructions clear
  - [ ] CI workflow triggers documented
