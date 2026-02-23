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
