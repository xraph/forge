# Forge Framework Release Process

This document describes the automated release process for the Forge framework, which includes both CLI and package releases based on conventional commits.

## Overview

The release process is fully automated using GitHub Actions and follows the [Conventional Commits](https://www.conventionalcommits.org/) specification. The system creates both CLI binary releases and Go module package releases.

## Release Workflow

### 1. Pull Request Validation

**Workflow**: `.github/workflows/pr-conventional-commits.yml`

- ✅ Validates PR titles follow conventional commits format
- ✅ Validates all commits in the PR follow conventional commits format
- ✅ Determines release impact (major/minor/patch/none)
- ✅ Adds appropriate labels to PRs
- ✅ Posts validation comments on PRs
- ✅ Sets status checks for branch protection

### 2. Release Preparation

**Workflow**: `.github/workflows/conventional-commits.yml`

Triggers on push to `main` branch:

- ✅ Analyzes commits since last release using conventional commits
- ✅ Determines if a release is needed
- ✅ Calculates next version number (semver)
- ✅ **Checks for existing release PRs to avoid duplicates**
- ✅ Creates or updates release preparation PR with:
    - Updated version file
    - Generated changelog
    - Release checklist
- ✅ Enables auto-merge on release PRs
- ✅ Creates and pushes release tag when PR is merged

### 3. CLI Release

**Workflow**: `.github/workflows/release.yml`

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

Version information is stored in `.github/version.json`:

```json
{
  "version": "1.2.3"
}
```

## Branch Strategy

- **main**: Main development branch
- **release/vX.Y.Z**: Release preparation branches (auto-created)
- **Tags**: `vX.Y.Z` format triggers releases

## Troubleshooting

### Release PR Issues

**Problem**: Multiple release PRs created
- **Solution**: The workflow now checks for existing release PRs and updates them instead of creating duplicates

**Problem**: Release PR not auto-merging
- **Solution**: Ensure all required status checks are passing

### Failed Releases

**Problem**: GoReleaser fails
- **Solution**: Check build logs, verify Go module is valid

**Problem**: Docker build fails
- **Solution**: Verify Dockerfile and ensure CLI builds correctly

### Version Issues

**Problem**: Wrong version number
- **Solution**: Ensure commits follow conventional commits format exactly

**Problem**: No release created
- **Solution**: Check if commits are release-worthy (feat/fix/breaking changes)

## Manual Release

If needed, you can trigger a manual release:

1. Create a properly formatted version tag:
   ```bash
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

2. The release workflows will automatically trigger

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
2. **Let the automated system handle releases**
3. **Review release PRs before merging**
4. **Test CLI binaries after release**
5. **Monitor for any release failures**
6. **Keep changelog and documentation updated**

The release system is designed to be fully automated and reliable, reducing manual intervention while ensuring consistent, high-quality releases.