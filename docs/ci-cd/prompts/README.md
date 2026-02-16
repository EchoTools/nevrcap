# CI/CD Task Prompts - Index

This directory contains task-specific prompts for implementing the nevrcap CI/CD system. Each prompt is self-contained with file references, requirements, verification steps, and anti-patterns.

## Quick Start

**For Coding Agents**: Pick a task from the parallel execution waves below. Each prompt is designed to be used independently without context overflow.

**For /start-work**: The orchestrator will dispatch these tasks automatically using the main plan (`.sisyphus/plans/ci-cd-setup.md`).

## Execution Order (Parallel Waves)

### Wave 1 - Configuration Foundation (Parallel)
Start with these three tasks in parallel:

- **[Task 1: golangci-lint Config](task-01-golangci-lint.md)** 
  - File: `.golangci.yml`
  - Duration: ~30 min
  - Enable 8 linters with baseline mode

- **[Task 2: Dependabot Config](task-02-dependabot.md)**
  - File: `.github/dependabot.yml`
  - Duration: ~20 min
  - Weekly Go dependency updates

- **[Task 8: Branch Protection Docs](task-08-branch-protection-docs.md)**
  - File: `docs/branch-protection-setup.md`
  - Duration: ~45 min
  - GitHub settings guide

### Wave 2 - GitHub Workflows (Parallel, after Wave 1)
After Wave 1 completes:

- **[Task 3: PR Validation Workflow](task-03-pr-workflow.md)**
  - Files: `.github/workflows/pr.yml`, `pkg/events/event_detector_test.go` (race skip)
  - Duration: ~1 hour
  - 5 jobs: lint, test, coverage, race, vuln

- **[Task 4: Main Branch CI Workflow](task-04-main-workflow.md)**
  - File: `.github/workflows/main.yml`
  - Duration: ~1.5 hours
  - Benchmark regression detection with benchstat

### Wave 3 - Git Hooks (Parallel, after Wave 1)
After Wave 1 completes:

- **[Task 5: Pre-Commit Hook](task-05-pre-commit-hook.md)**
  - File: `scripts/hooks/pre-commit`
  - Duration: ~45 min
  - Format, lint, go.mod checks

- **[Task 6: Pre-Push Hook](task-06-pre-push-hook.md)**
  - File: `scripts/hooks/pre-push`
  - Duration: ~1 hour
  - Tests, race, main protection

- **[Task 7: Hook Installer](task-07-hook-installer.md)**
  - File: `scripts/install-hooks.sh`
  - Duration: ~30 min
  - Install/uninstall script

### Wave 4 - Benchmarks (After Waves 2)
After workflows are ready:

- **[Task 9: Benchmark Baseline](task-09-benchmark-baseline.md)**
  - File: `.benchmarks/baseline.txt`
  - Duration: ~30 min
  - Generate initial performance baseline

### Wave 5 - Integration (After All)
Final verification:

- **[Task 10: Integration Testing](task-10-integration-testing.md)**
  - No files created
  - Duration: ~1 hour
  - End-to-end system verification

## Task Quick Reference

| Task | File(s) Created | Duration | Category |
|------|----------------|----------|----------|
| 1 | `.golangci.yml` | 30 min | quick |
| 2 | `.github/dependabot.yml` | 20 min | quick |
| 3 | `.github/workflows/pr.yml`, `pkg/events/event_detector_test.go` | 1 hour | unspecified-low |
| 4 | `.github/workflows/main.yml` | 1.5 hours | unspecified-low |
| 5 | `scripts/hooks/pre-commit` | 45 min | quick |
| 6 | `scripts/hooks/pre-push` | 1 hour | quick |
| 7 | `scripts/install-hooks.sh` | 30 min | quick |
| 8 | `docs/branch-protection-setup.md` | 45 min | quick |
| 9 | `.benchmarks/baseline.txt` | 30 min | quick |
| 10 | Integration tests only | 1 hour | unspecified-low |

**Total Estimated Time**: 6-8 hours

## Usage Patterns

### For Sequential Execution
```bash
# Wave 1
cat task-01-golangci-lint.md  # Implement
cat task-02-dependabot.md     # Implement
cat task-08-branch-protection-docs.md  # Implement

# Wave 2 (after Wave 1)
cat task-03-pr-workflow.md    # Implement
cat task-04-main-workflow.md  # Implement

# Wave 3 (after Wave 1)
cat task-05-pre-commit-hook.md   # Implement
cat task-06-pre-push-hook.md     # Implement
cat task-07-hook-installer.md    # Implement

# Wave 4 (after Wave 2)
cat task-09-benchmark-baseline.md  # Implement

# Wave 5 (after all)
cat task-10-integration-testing.md  # Verify
```

### For Task Framework Integration
Use with `task()` delegation:

```python
# Wave 1 - Parallel dispatch
task(category="quick", load_skills=[], prompt=open("task-01-golangci-lint.md").read(), ...)
task(category="quick", load_skills=[], prompt=open("task-02-dependabot.md").read(), ...)
task(category="quick", load_skills=[], prompt=open("task-08-branch-protection-docs.md").read(), ...)

# Collect results before Wave 2...
```

### For /start-work Orchestrator
The orchestrator reads the main plan (`.sisyphus/plans/ci-cd-setup.md`) and dispatches tasks automatically. These prompts serve as supplementary context.

## Prompt Structure

Each prompt follows this structure:

1. **Context** — What this task does, where it fits
2. **Files to Reference** — What to read before starting
3. **What to Create** — Output files
4. **Requirements** — Detailed specifications
5. **Implementation Steps** — How to build it
6. **Verification (Agent-Executed QA)** — Bash commands to test
7. **References** — External documentation
8. **Success Criteria** — Checklist
9. **Commit** — Git commit message and files
10. **Anti-Patterns** — What NOT to do
11. **Parallelization** — Dependencies

## Critical Constraints (All Tasks)

- **GOWORK=off** must be set in all CI jobs (go.work references ../nevr-common)
- **Baseline approach**: Ignore 51 pre-existing lint issues with `--new-from-rev`
- **Race test skip**: Add env-guarded `t.Skip()` to one test (3 lines total)
- **Coverage thresholds**: 80% default, 74.6% for conversion package
- **Benchmark format**: Text format (NOT JSON) for benchstat compatibility
- **No placeholders**: All implementations must be complete (no TODOs or "insert code here")

## File References

All prompts reference:
- **Plan**: `.sisyphus/plans/ci-cd-setup.md` (master plan with all details)
- **go.mod**: Go version (1.25), dependencies
- **Existing tests**: `pkg/events/event_detector_test.go` (race condition)
- **Existing benchmarks**: 4 benchmark files in pkg/events and pkg/codecs

## Verification

Every task includes "Agent-Executed QA" scenarios with bash commands that verify:
- File creation
- Format validity (YAML, JSON, shellcheck)
- Functional correctness (hooks block commits, workflows have required jobs)
- Integration (GOWORK=off builds, baseline ignores 51 issues)

**Zero human intervention required** for verification.

## Related Documentation

- **[Main Plan](../../.sisyphus/plans/ci-cd-setup.md)** — Complete 1,284-line implementation plan
- **[Coding Agent Guide](../CODING-AGENT-GUIDE.md)** — Instructions for using the plan
- **[CI/CD README](../README.md)** — Overview and navigation

## Support

- Questions about task requirements → See task prompt
- Questions about overall system → See main plan
- Questions about execution order → See this index (waves)
- Questions about anti-patterns → Each prompt has "Anti-Patterns" section
