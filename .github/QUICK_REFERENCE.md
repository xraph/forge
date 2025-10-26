# CI/CD Quick Reference Card

## 🎯 TL;DR
**Status**: ⚠️ Needs fixes (~4 hours work)  
**New Files Created**: 4 (review and commit them)  
**Critical Issues**: 3 (all have solutions ready)

---

## 📁 Files Created for You

```
.github/
├── workflows/
│   ├── cli-release.yml ← NEW: Production-ready CLI release workflow
│   └── go-improved.yml ← NEW: Enhanced Go CI with security scans
├── CI_CD_REVIEW.md     ← NEW: Full technical analysis (12K+ words)
├── ACTION_PLAN.md      ← NEW: Step-by-step implementation guide  
├── EXECUTIVE_SUMMARY.md ← NEW: Executive overview
└── QUICK_REFERENCE.md  ← YOU ARE HERE
```

---

## 🚨 3 Critical Issues

| Issue | Severity | Fix Time | Solution |
|-------|----------|----------|----------|
| Missing CLI release workflow | ❌ CRITICAL | 0 min | Already created `cli-release.yml` |
| Wrong Go versions in CI | ⚠️ HIGH | 30 min | Use `go-version-file: go.mod` |
| Test inconsistencies | ⚠️ HIGH | 60 min | Exclude `bk/` everywhere |

---

## ⚡ Quick Start (5 minutes)

```bash
# 1. Commit new files
git add .github/
git commit -m "feat(ci): add comprehensive CI/CD workflows and documentation"

# 2. Test with pre-release
git tag -a v0.0.1-beta.1 -m "Test CI/CD"
git push origin v0.0.1-beta.1

# 3. Watch it work
open "https://github.com/xraph/forge/actions"

# 4. Verify release
go install github.com/xraph/forge/cmd/forge@v0.0.1-beta.1
forge --version
```

---

## 🔧 Critical Fixes Needed

### Fix #1: Update Go Versions (30 min)
```yaml
# In ALL workflow files, change:
go-version: "1.24"        # ❌ OLD
go-version: ['1.21', ...] # ❌ OLD

# To:
go-version-file: go.mod   # ✅ NEW
check-latest: true        # ✅ NEW
```

**Files to update**:
- `.github/workflows/go.yml`
- `.github/workflows/package-release.yml`

### Fix #2: Standardize Tests (60 min)
```bash
# Use this command EVERYWHERE:
go test -v -race -timeout=5m $(go list ./... | grep -v '/bk/')

# NOT this:
go test -v -race ./...  # ❌ Includes bk/
```

**Files to update**:
- `.github/workflows/go.yml`
- `.github/workflows/package-release.yml`
- `Makefile` (already correct ✅)

### Fix #3: Add Caching (15 min)
```yaml
# Add to all workflow files after setup-go:
- name: Cache Go modules
  uses: actions/cache@v4
  with:
    path: |
      ~/go/pkg/mod
      ~/.cache/go-build
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
```

---

## 📋 Pre-Flight Checklist

Before merging to production:

- [ ] New `cli-release.yml` committed
- [ ] Go versions fixed
- [ ] Test commands standardized
- [ ] Caching added
- [ ] Tested with pre-release tag
- [ ] Verified release works: `go install github.com/xraph/forge/cmd/forge@TAG`
- [ ] Branch protection enabled
- [ ] Team notified of changes

---

## 🎯 What Each Workflow Does

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `go.yml` | Push/PR | Basic build and test |
| `go-improved.yml` | Push/PR | Enhanced with security scans |
| `pr-conventional-commits.yml` | PR | Validates commit format |
| `conventional-commits.yml` | Push to main | Creates release PRs |
| `cli-release.yml` | Version tag | Releases CLI binaries |
| `package-release.yml` | Version tag | Releases Go module |

---

## 🔄 Release Flow

```
1. Developer writes code with conventional commit
   ↓
2. Push to main (tests run)
   ↓
3. Conventional commits workflow analyzes commits
   ↓
4. Release PR created/updated (if needed)
   ↓
5. Review and merge release PR
   ↓
6. Tag automatically created (v1.2.3)
   ↓
7. CLI release workflow triggers
   ↓
8. Tests run on all platforms
   ↓
9. GoReleaser builds binaries
   ↓
10. GitHub release created
    ↓
11. Docker images published
    ↓
12. Package managers updated
    ↓
13. ✨ Done! (5-10 minutes total)
```

---

## 📝 Conventional Commit Format

```
type(scope): description

[optional body]

[optional footer]
```

**Types that trigger releases**:
- `feat:` → Minor version bump (1.0.0 → 1.1.0)
- `fix:` → Patch version bump (1.0.0 → 1.0.1)
- `feat!:` or `BREAKING CHANGE:` → Major version bump (1.0.0 → 2.0.0)

**Types that DON'T trigger releases**:
- `docs:`, `chore:`, `ci:`, `test:`, `style:`, `refactor:`

---

## 🧪 Testing Commands

```bash
# Test GoReleaser config
goreleaser check

# Dry-run release (no publishing)
goreleaser release --snapshot --clean

# Test with pre-release tag
git tag -a v0.0.1-beta.1 -m "Test"
git push origin v0.0.1-beta.1

# Verify installation works
go install github.com/xraph/forge/cmd/forge@v0.0.1-beta.1
forge --version

# Delete test tag if needed
git tag -d v0.0.1-beta.1
git push origin :refs/tags/v0.0.1-beta.1
```

---

## 🆘 Emergency Rollback

```bash
# 1. Delete GitHub release
gh release delete v1.2.3 --yes

# 2. Delete tag remotely
git push origin :refs/tags/v1.2.3

# 3. Delete tag locally
git tag -d v1.2.3

# 4. Fix issues, then re-tag
git tag -a v1.2.3 -m "Fixed release"
git push origin v1.2.3
```

---

## 🔑 Required Secrets

| Secret | Required? | Purpose |
|--------|-----------|---------|
| `GITHUB_TOKEN` | ✅ Yes | Automatically provided |
| `FURY_TOKEN` | ❌ No | Package distribution |
| `AUR_KEY` | ❌ No | Arch Linux packages |
| `NPM_TOKEN` | ❌ No | NPM packages |

---

## 📊 Success Indicators

**It's working when you see**:
- ✅ Tests pass on all platforms
- ✅ Release PR auto-created
- ✅ Tag auto-created on merge
- ✅ Binaries built (Linux, macOS, Windows)
- ✅ Docker images published
- ✅ `go install` works immediately
- ✅ GitHub release has changelog

---

## 🎓 Learn More

| Document | When to Read | Time |
|----------|-------------|------|
| `EXECUTIVE_SUMMARY.md` | First! High-level overview | 5 min |
| `QUICK_REFERENCE.md` | Quick lookup | 2 min |
| `ACTION_PLAN.md` | Ready to implement | 10 min |
| `CI_CD_REVIEW.md` | Deep technical dive | 30 min |

---

## 🔗 Useful Commands

```bash
# View all workflows
ls -la .github/workflows/

# Check workflow syntax
actionlint .github/workflows/*.yml

# View recent workflow runs
gh run list --limit 10

# View specific run details
gh run view <run-id>

# Re-run failed workflow
gh run rerun <run-id>

# List all tags
git tag -l

# List releases
gh release list
```

---

## 📈 Performance Tips

1. **Always use caching** - Saves 2-3 minutes per run
2. **Use matrix parallelization** - Runs tests concurrently
3. **Cache Go modules** - Avoid downloading every time
4. **Use fail-fast: false** - See all failures, not just first

---

## 🎯 Next Actions

| Priority | Action | Time |
|----------|--------|------|
| P0 | Review new files | 15 min |
| P0 | Commit new workflows | 5 min |
| P0 | Fix Go versions | 30 min |
| P0 | Test with pre-release | 30 min |
| P1 | Fix test commands | 60 min |
| P1 | Add caching | 15 min |
| P2 | Update documentation | 120 min |

**Total**: ~4-5 hours to production-ready

---

## ✅ Final Checklist

```
Before Production:
□ All files reviewed
□ Critical fixes applied
□ Tested with pre-release tag
□ Verified release works
□ Team trained
□ Documentation updated
□ Branch protection enabled
□ Rollback procedure understood

After First Real Release:
□ Verified binaries work
□ Checked Docker images
□ Tested go install
□ Verified pkg.go.dev listing
□ Announced release
□ Monitored for issues
```

---

## 💡 Pro Tips

1. **Always test with pre-release tags first** (`v1.0.0-beta.1`)
2. **Don't skip CI** - It catches real issues
3. **Review release PRs carefully** - Version bumps are permanent
4. **Keep commits atomic** - One feature/fix per commit
5. **Write good commit messages** - They become your changelog
6. **Monitor first few releases closely** - Catch issues early

---

## 📞 Support

If something goes wrong:
1. Check GitHub Actions logs
2. Review `CI_CD_REVIEW.md` troubleshooting section
3. Test GoReleaser locally: `goreleaser release --snapshot --clean`
4. Rollback using emergency procedure above

---

**Keep this card handy!** Bookmark for quick reference.

---

*Last Updated: October 26, 2025*

