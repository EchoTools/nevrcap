# Task 2: Create Dependabot Configuration

## Context
You are implementing Task 2 of the CI/CD setup plan. This enables automated dependency updates for Go modules via GitHub Dependabot.

## Files to Reference
- **go.mod** — Current dependencies to be managed
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 2 section, lines 295-377)

## What to Create
**File**: `.github/dependabot.yml`

## Requirements

### Dependabot Settings
- **Ecosystem**: `gomod` (Go modules)
- **Directory**: `/` (root, where go.mod is located)
- **Schedule**: Weekly updates
- **Target Branch**: `main`
- **Open PR Limit**: 5 (prevent PR flood)
- **Commit Message Prefix**: `chore(deps):`
- **Labels**: `dependencies`, `automated`
- **Version**: Dependabot v2 schema

### Optional Settings (User Can Add Later via UI)
- **Reviewers**: None (optional, can be added later)
- **Assignees**: None (optional)
- **Auto-merge**: Not enabled (user preference not confirmed)

### Integration
- Enable govulncheck integration (GitHub's native Go vulnerability scanning)

## Implementation Steps

1. **Create `.github/dependabot.yml`**
2. **Set version**: `version: 2` (Dependabot v2 schema)
3. **Configure gomod updates**:
   - Package ecosystem: `gomod`
   - Directory: `/`
   - Schedule interval: `weekly`
   - Open PR limit: 5
4. **Set commit message**: Prefix `chore(deps):`
5. **Add labels**: `dependencies`, `automated`
6. **Validate YAML**: Run `python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"`

## Verification (Agent-Executed QA)

### Scenario 1: Config is valid YAML
```bash
# Step 1: Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"
# Expected: Exit code 0

# Step 2: Verify gomod ecosystem
cat .github/dependabot.yml
# Expected: Contains "package-ecosystem: gomod"

# Step 3: Verify schedule and PR limit
cat .github/dependabot.yml
# Expected: Contains "schedule:" with "interval:" and "open-pull-requests-limit: 5"
```

### Scenario 2: Config follows GitHub Dependabot schema
```bash
# Step 1: Check schema version
grep "version:" .github/dependabot.yml
# Expected: Contains "version: 2"

# Step 2: Check directory setting
grep "directory:" .github/dependabot.yml
# Expected: Contains 'directory: "/"' (root for go.mod)
```

## References
- **Dependabot docs**: https://docs.github.com/en/code-security/dependabot/dependabot-version-updates/configuration-options-for-the-dependabot.yml-file
- **gomod ecosystem**: https://docs.github.com/en/code-security/dependabot/dependabot-version-updates/configuration-options-for-the-dependabot.yml-file#package-ecosystem
- **govulncheck**: https://go.dev/security/vuln/

## Success Criteria
✅ File `.github/dependabot.yml` exists  
✅ Config is valid YAML (python parser succeeds)  
✅ Schema version is 2  
✅ Package ecosystem is `gomod`  
✅ Directory is `/` (root)  
✅ Schedule interval configured (weekly)  
✅ Open PR limit set to 5  
✅ Commit message prefix includes `chore(deps):`  
✅ Labels include `dependencies` and `automated`  

## Commit
**Message**: `chore(ci): add Dependabot configuration for Go dependencies`  
**Files**: `.github/dependabot.yml`  
**Pre-commit check**: `python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"`

## Anti-Patterns (DO NOT DO)
❌ Enable auto-merge (not confirmed by user)  
❌ Configure reviewers/assignees (can be added later via UI)  
❌ Leave placeholders or TODOs  
❌ Use Dependabot v1 format (deprecated)  

## Parallelization
- **Can run in parallel**: YES
- **Parallel with**: Task 1 (golangci-lint), Task 8 (Branch protection docs)
- **Blocks**: Task 3 (PR workflow references dependabot)
