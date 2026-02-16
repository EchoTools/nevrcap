# CI/CD Implementation Documentation

This directory contains comprehensive documentation for implementing the CI/CD system for nevrcap.

## Quick Navigation

### For Humans
- **[plan.md](./plan.md)** — Complete implementation plan with 10 tasks (Prometheus-generated)
- **[START HERE]** Read this if you want to understand the complete system design

### For Coding Agents
- **[CODING-AGENT-GUIDE.md](./CODING-AGENT-GUIDE.md)** — Execution guide with explicit implementation instructions
- **[START HERE]** if you're an autonomous agent implementing this system
- Contains anti-placeholder rules, common pitfalls, exact code examples

## What's Being Built

A comprehensive CI/CD system with:
- **GitHub Actions Workflows**: PR validation + main branch CI
- **Git Hooks**: Local pre-commit and pre-push checks
- **Test Coverage**: 80%+ enforcement with package-specific thresholds
- **Benchmark Regression**: 5% performance threshold tracking
- **Branch Protection**: 1 approval required, all checks must pass
- **Security**: Dependabot + govulncheck vulnerability scanning

## Key Technical Decisions

| Decision | Rationale |
|----------|-----------|
| **Baseline Approach** | Ship CI immediately with `--new-from-rev` to ignore 51 pre-existing lint issues |
| **GOWORK=off** | CI doesn't have `../nevr-common` sibling, must disable workspace |
| **Text Format Baseline** | Use standard Go benchmark text format for benchstat compatibility (not JSON) |
| **No Auto-Commit** | Workflows upload benchmarks as artifacts (branch protection blocks auto-commit) |
| **Race Test Skip** | Add env-guarded skip for known flaky test `TestAsyncDetector_SensorIntegrationReceivesFrames` |
| **golangci-lint v2** | Use v2.8.0 format (current installed version) |
| **Benchmarks on Main Only** | CI machine variance makes 5% threshold unreliable on PRs |

## Implementation Waves

### Wave 1: Config Foundation (Parallel)
- Task 1: `.golangci.yml`
- Task 2: `.github/dependabot.yml`
- Task 8: `docs/branch-protection-setup.md`

### Wave 2: Workflows (After Wave 1)
- Task 3: `.github/workflows/pr.yml` (with race test skip)
- Task 4: `.github/workflows/main.yml`

### Wave 3: Hooks (After Wave 1)
- Task 5: `scripts/hooks/pre-commit`
- Task 6: `scripts/hooks/pre-push`
- Task 7: `scripts/install-hooks.sh`

### Wave 4: Benchmarks (After Wave 2)
- Task 9: `.benchmarks/baseline.txt`

### Wave 5: Integration (After All)
- Task 10: End-to-end verification

## Success Criteria

Implementation is complete when:

✅ All GitHub Actions workflows parse and validate  
✅ golangci-lint config is valid  
✅ Git hooks install correctly and catch issues  
✅ `GOWORK=off go build ./...` succeeds  
✅ Benchmark baseline exists in text format  
✅ Branch protection documentation complete  
✅ NO placeholders or TODOs in any file  
✅ All Agent-Executed QA scenarios pass

## Files to be Created

```
.github/
├── workflows/
│   ├── pr.yml           (PR validation: lint, test, coverage, race, vuln)
│   └── main.yml         (Main CI: benchmarks, full test suite)
└── dependabot.yml       (Dependency updates)

.golangci.yml            (Linter config with baseline)

.benchmarks/
└── baseline.txt         (Performance baseline - Go text format)

scripts/
├── hooks/
│   ├── pre-commit       (Format, lint, go.mod checks)
│   └── pre-push         (Tests, race detector, main protection)
└── install-hooks.sh     (Hook installer with uninstall)

docs/
└── branch-protection-setup.md  (GitHub settings guide)

pkg/events/event_detector_test.go  (MODIFIED: add race test skip)
```

## Usage

### For Humans Planning Work
1. Read `plan.md` for complete context
2. Review Metis findings (blockers, guardrails)
3. Understand parallel execution waves
4. Use task acceptance criteria for verification

### For Autonomous Agents
1. Read `CODING-AGENT-GUIDE.md` first
2. Follow wave-by-wave execution
3. Implement exact code examples (no placeholders!)
4. Run Agent-Executed QA scenarios after each task
5. Verify against final checklist

### After Implementation
```bash
# Install hooks locally
bash scripts/install-hooks.sh

# Verify system
GOWORK=off go build ./...
golangci-lint config verify

# Set up branch protection (see docs)
# Follow docs/branch-protection-setup.md
```

## Questions?

- **Plan unclear?** Check `plan.md` for task-specific "References" sections
- **Implementation stuck?** Check `CODING-AGENT-GUIDE.md` "Common Pitfalls"
- **Verification failing?** Each task has "Agent-Executed QA Scenarios" with exact commands

## Approval History

- **Metis Review**: Identified 51 lint issues, race condition, coverage gap → Baseline approach recommended
- **Momus Review**: OKAY (3 iterations, all blockers resolved)
  - Iteration 1: Fixed benchmark format, auto-commit conflict, gh verification
  - Iteration 2: Fixed race test scope, baseline contradiction, invalid benchmark reference
  - Iteration 3: Approved with one minor non-blocking reference mismatch

## Links

- **Main Repository**: github.com/echotools/nevr-capture
- **Go Version**: 1.25 (see `go.mod`)
- **golangci-lint**: v2.8.0
- **Dependencies**: nevr-common/v4, klauspost/compress, gofrs/uuid
