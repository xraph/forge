# Forge Framework Release Process

This document describes the automated release process for the Forge framework, which includes both CLI and package releases based on conventional commits.

## Overview

The release process is fully automated using **Google's Release Please** and GitHub Actions. It follows the [Conventional Commits](https://www.conventionalcommits.org/) specification and creates both CLI binary releases and Go module package releases.

## Release Workflow

### 1. Pull Request Validation

**Workflow**: `.github/workflows/pr-conventional-commits.yml`

- ✅ Validates PR titles follow conventional commits format
- ✅ Validates all commits in the PR follow conventional commits format
- ✅ Determines release impact (major/minor/patch/none)
- ✅ Adds appropriate labels to PRs
- ✅ Posts validation comments on PRs
- ✅ Sets status checks for branch protection
- ✅ Automatically skips Release Please PRs (auto-generated)

### 2. Release Preparation

**Workflow**: `.github/workflows/release-please.yml`

Triggers on push to `main` branch:

- ✅ Analyzes commits since last release using Release Please
- ✅ Determines if a release is needed
- ✅ Calculates next version number (semver)
- ✅ Creates or updates release preparation PR with:
    - Updated version in `.release-please-manifest.json`
    - Generated changelog
    - Release notes
- ✅ Enables auto-merge on release PRs (when checks pass)
- ✅ Creates and pushes release tag when PR is merged

### 3. CLI Release

**Workflow**: `.github/workflows/cli-release.yml`

Triggers on version tags (e.g., `v1.2.3`):

- 🔨 Validates tag format and runs tests
- 🏗️ Uses GoReleaser to build CLI binaries for multiple platforms:
    - Linux (amd64, arm64)
    - macOS (amd64, arm64)
    - Windows (amd64)
- 📦 Creates release archives (tar.gz, zip)
- 🐳 Builds and publishes Docker images
- 📋 Generates checksums and signs artifacts
- 🍺 Updates Homebrew tap and Scoop bucket
- 📦 Creates packages for various Linux distributions
- 🎉 Creates GitHub release with changelog

### 4. Package Release

**Workflow**: `.github/workflows/package-release.yml`

Triggers on version tags:

- ✅ Tests package across multiple Go versions (1.21, 1.22, 1.23)
- 🖥️ Tests on multiple platforms (Linux, macOS, Windows)
- 📚 Validates examples and documentation
- 🔍 Checks for breaking changes
- 📖 Generates API documentation
- 🌐 Notifies Go proxy for module availability

## Release Types

Based on conventional commits, releases are automatically categorized:

### Major Release (Breaking Changes)
- Commit types with `!` or `BREAKING CHANGE` footer
- Examples:
    - `feat!: remove deprecated API`
    - `fix!: change function signature`

### Minor Release (New Features)
- `feat:` commits
- Examples:
    - `feat(router): add middleware support`
    - `feat(database): add MongoDB adapter`

### Patch Release (Bug Fixes)
- `fix:` commits
- Examples:
    - `fix(core): resolve memory leak`
    - `fix(cli): handle invalid arguments`

### No Release
- Other commit types: `docs`, `chore`, `test`, `ci`, `style`, `refactor`

## Installation Methods

### CLI Installation

```bash
# Go Install (recommended)
go install github.com/xraph/forge/cmd/forge@latest

# Homebrew (macOS/Linux)
brew install xraph/tap/forge

# Scoop (Windows)
scoop bucket add xraph https://github.com/xraph/scoop-bucket
scoop install forge

# Docker
docker run ghcr.io/xraph/forge:latest

# Download binary
# Visit: https://github.com/xraph/forge/releases
```

### Package Installation

```bash
# Add to your Go module
go get github.com/xraph/forge@latest

# Specific version
go get github.com/xraph/forge@v1.2.3
```

## Configuration Files

### `.goreleaser.yml`

GoReleaser configuration for CLI builds:
- Multi-platform binary builds
- Docker image creation
- Package manager integration
- Archive generation
- Signing and checksums

### Version Management

Version information is tracked by Release Please in `.release-please-manifest.json`:

```json
{
  ".": "1.2.3"
}
```

This file is automatically updated by Release Please when creating release PRs. You should never need to edit it manually.

## Branch Strategy

- **main**: Main development branch
- **release-please--branches--main--components--forge**: Release preparation branches (auto-created by Release Please)
- **Tags**: `vX.Y.Z` format triggers CLI releases

## Troubleshooting

### Release PR Issues

**Problem**: Release PR not created after merging to main
- **Solution**: Ensure your commits use conventional commit format (`feat:`, `fix:`, etc.). Only these types trigger releases.

**Problem**: Release PR not auto-merging
- **Solution**: Ensure all required status checks are passing. Check the PR for any failing checks.

### Failed Releases

**Problem**: GoReleaser fails
- **Solution**: Check build logs, verify Go module is valid

**Problem**: Docker build fails
- **Solution**: Verify Dockerfile and ensure CLI builds correctly

### Version Issues

**Problem**: Wrong version number
- **Solution**: Release Please follows semantic versioning strictly. Check the PR to see how it calculated the version based on conventional commits.

**Problem**: No release created
- **Solution**: Only `feat:`, `fix:`, and commits with `BREAKING CHANGE` trigger releases. Other types (`docs:`, `chore:`, etc.) don't create releases.

## Manual Release

If needed, you can trigger a manual release by creating a tag:

1. Create a properly formatted version tag:
   ```bash
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

2. The CLI release workflow will automatically trigger

**Note**: Release Please tracks versions automatically. If you manually create a tag, update `.release-please-manifest.json` to match to keep the system in sync:

```bash
# After creating v1.2.3 tag manually
echo '{".":" 1.2.3"}' > .release-please-manifest.json
git add .release-please-manifest.json
git commit -m "chore: update version manifest after manual release"
git push origin main
```

## Environment Variables / Secrets

Required secrets for full functionality:

- `GITHUB_TOKEN`: Automatically provided
- `NPM_TOKEN`: For NPM package publishing (optional)
- `DISCORD_WEBHOOK_URL`: For Discord notifications (optional)
- `SLACK_WEBHOOK_URL`: For Slack notifications (optional)

## Monitoring

Monitor releases via:
- GitHub Actions tab for workflow status
- GitHub Releases page for published releases
- Package availability on pkg.go.dev
- Docker images on ghcr.io

## Best Practices

1. **Always use conventional commit format**
2. **Let Release Please handle versioning** - Don't manually edit version files
3. **Review release PRs carefully** - Check version bump and changelog before merging
4. **Test CLI binaries after release**
5. **Monitor for any release failures**
6. **Let Release Please generate the changelog** - It automatically creates well-formatted changelogs

## Benefits of Release Please

The system uses **Google's Release Please** for release automation, providing:

- ✅ **Zero maintenance** - Google maintains the action
- ✅ **Battle-tested** - Used by Angular, Terraform, and many Google projects
- ✅ **Smart version bumping** - Automatically determines correct semver based on commits
- ✅ **Excellent changelog** - Auto-generated with links to PRs and commits
- ✅ **Monorepo support** - Native support for Go modules (can handle extensions independently)
- ✅ **No duplicate PRs** - Reuses existing release PRs automatically
- ✅ **Simple configuration** - Just 2 JSON files: `release-please-config.json` and `.release-please-manifest.json`

The release system is designed to be fully automated and reliable, reducing manual intervention while ensuring consistent, high-quality releases.