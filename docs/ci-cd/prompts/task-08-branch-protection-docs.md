# Task 8: Create Branch Protection Documentation

## Context
You are implementing Task 8 of the CI/CD setup plan. This documentation guides users through setting up GitHub branch protection for the main branch.

## Files to Reference
- **.github/workflows/pr.yml** — Lists required status checks (from Task 3)
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 8 section, lines 907-1004)

## What to Create
**File**: `docs/branch-protection-setup.md`

## Documentation Requirements

### GitHub Branch Protection Settings

Document these settings for the main branch:

1. **Require pull request before merging**: YES
2. **Required approvals**: 1
3. **Require status checks to pass**: YES
4. **Required status checks**: All jobs from `.github/workflows/pr.yml`
   - lint
   - test
   - coverage
   - race
   - vuln
5. **Require branches to be up to date**: YES
6. **Restrict who can push**: NO (hooks + CI are sufficient)
7. **Allow force pushes**: NO
8. **Allow deletions**: NO

### Documentation Sections

#### 1. Overview
- Purpose of branch protection
- What it prevents (direct pushes, force push, unreviewed changes)
- Benefits (code quality, peer review)

#### 2. Prerequisites
- Admin access to repository
- GitHub workflows must be set up first (Task 3, Task 4)

#### 3. Manual Setup (GitHub UI)
- Step-by-step instructions with screenshot placeholders
- Navigate to: Settings → Branches → Add rule
- Configure each setting
- Save rule

#### 4. Automated Setup (GitHub CLI)
- Alternative approach using `gh` CLI
- Include complete `gh api` command
- Include `protection.json` template with all settings

#### 5. Verification
- How to test branch protection is working
- Try to push directly to main (should fail)
- Verify PR status checks appear

### JSON Template (`protection.json`)

Must include valid JSON with:
- `required_pull_request_reviews` configuration
- `required_status_checks` list
- `enforce_admins` setting
- `allow_force_pushes` disabled
- `allow_deletions` disabled

## Implementation Steps

1. **Create `docs/branch-protection-setup.md`**
2. **Write overview section** explaining purpose
3. **List prerequisites** (admin access, workflows)
4. **Document UI setup steps**:
   - Navigation path
   - Each setting with explanation
   - Screenshot placeholders: `[Screenshot: Branch protection settings]`
5. **Document CLI alternative**:
   - Install gh CLI
   - Create protection.json
   - Run gh api command
6. **Add verification section**
7. **Create protection.json template** (embed in doc or separate file)
8. **Validate JSON**: `python3 -c "import json; json.loads(...)"`

## Verification (Agent-Executed QA)

### Scenario 1: Documentation exists and is complete
```bash
# Step 1: Verify file exists
ls docs/branch-protection-setup.md
# Expected: File exists

# Step 2: Check file length
wc -l docs/branch-protection-setup.md
# Expected: >20 lines (substantive content)

# Step 3: Check for key sections
grep -i "pull request" docs/branch-protection-setup.md
# Expected: Found

grep -i "status check" docs/branch-protection-setup.md
# Expected: Found

grep -i "approval" docs/branch-protection-setup.md
# Expected: Found
```

### Scenario 2: GitHub CLI commands are included
```bash
# Step 1: Check for gh CLI usage
grep "gh api" docs/branch-protection-setup.md
# Expected: Contains gh CLI commands

# Step 2: Check for JSON reference
grep "protection.json" docs/branch-protection-setup.md
# Expected: References protection.json template
```

### Scenario 3: JSON template is valid (if embedded)
```bash
# Step 1: Extract JSON from markdown (if embedded)
grep -Pzo '(?s)\{.*\}' docs/branch-protection-setup.md > /tmp/protection-test.json || echo '{"test": true}' > /tmp/protection-test.json

# Step 2: Validate JSON syntax
python3 -c "import json; json.load(open('/tmp/protection-test.json'))"
# Expected: Exit code 0 (valid JSON)
```

## Document Template Structure

```markdown
# GitHub Branch Protection Setup

## Overview
[Explain purpose and benefits]

## Prerequisites
- [ ] Admin access to repository
- [ ] GitHub Actions workflows configured (.github/workflows/pr.yml)
- [ ] At least one successful workflow run

## Manual Setup (GitHub UI)

### Step 1: Navigate to Settings
1. Go to your repository on GitHub
2. Click **Settings** → **Branches**
3. Click **Add rule** under "Branch protection rules"

[Screenshot: Branch protection rules page]

### Step 2: Configure Rule
...

## Automated Setup (GitHub CLI)

### Install gh CLI
```bash
# macOS
brew install gh

# Linux
# See: https://github.com/cli/cli/blob/trunk/docs/install_linux.md
```

### Create protection.json
```json
{
  "required_pull_request_reviews": {
    "required_approving_review_count": 1
  },
  ...
}
```

### Apply Protection
```bash
gh api repos/{owner}/{repo}/branches/main/protection \
  --method PUT \
  --input protection.json
```

## Verification
[How to test it works]

## Troubleshooting
[Common issues and solutions]
```

## References
- **GitHub branch protection docs**: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches
- **GitHub API**: https://docs.github.com/en/rest/branches/branch-protection
- **gh CLI**: https://cli.github.com/manual/gh_api

## Success Criteria
✅ File `docs/branch-protection-setup.md` exists  
✅ File has >20 lines (substantive content)  
✅ Overview section explains purpose  
✅ Prerequisites listed  
✅ UI setup steps documented with screenshot placeholders  
✅ GitHub CLI alternative documented  
✅ protection.json template included  
✅ JSON is valid syntax  
✅ Verification section included  
✅ All 8 required settings documented  
✅ Required status checks listed (lint, test, coverage, race, vuln)  

## Commit
**Message**: `docs(ci): add GitHub branch protection setup guide`  
**Files**: `docs/branch-protection-setup.md`  
**Pre-commit check**: None (documentation only)

## Anti-Patterns (DO NOT DO)
❌ Automate branch protection setup (requires admin token, out of scope)  
❌ Include actual screenshots (placeholders only)  
❌ Leave incomplete JSON template  
❌ Forget to document required status checks  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 1 (golangci-lint), Task 2 (Dependabot)
- **Blocks**: None (documentation only)
- **Blocked by**: None (can start immediately)
