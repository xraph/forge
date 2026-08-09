# Contributing to Forge

Thanks for taking the time. This covers the mechanics; open a
[discussion](https://github.com/xraph/forge/discussions) if you want to talk
through an idea before writing code.

## Setting up

You need Go 1.24 or later. Make is optional, but every command below assumes it.

```bash
git clone https://github.com/your-username/forge.git
cd forge
make install-tools
```

## Making a change

Work on a branch, not on `main`.

```bash
make test           # all tests
make test-coverage  # with coverage
go test ./extensions/graphql/...
```

Before you push, run what CI runs:

```bash
make ci
```

That covers formatting, linting and tests together. The individual targets are
there when you want to iterate faster:

```bash
make fmt            # format
make lint           # lint
make lint-fix       # lint and fix what can be fixed
make security-scan  # security scan
make vuln-check     # check dependencies for known vulnerabilities
```

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/).
The prefix is not cosmetic: Release Please reads it to decide the next version
and to build the changelog, so a mislabelled commit produces a wrong release.

```
feat:     a new feature, bumps the minor version
fix:      a bug fix, bumps the patch version
docs:     documentation only
style:    formatting, no behaviour change
refactor: neither a fix nor a feature
perf:     a performance change
test:     adding or correcting tests
chore:    build, tooling, dependencies
```

Add a scope when it narrows things usefully, for example `fix(router):` or
`feat(client):`.

## Pull requests

Open the PR against `main`. Describe what changes and why; if the reason is a
bug, say what the bug did rather than only that it existed.

A PR is ready to review when `make ci` passes and the change is covered by
tests. If part of the work is deliberately left out, say so in the description
rather than leaving a reviewer to notice.

## Releases

You do not need to do anything to release your change. Merging to `main` with a
conventional commit message is enough: Release Please opens a PR with the
version bump and changelog, and merging that PR tags and publishes.

See the [README](README.md#releases) for the full pipeline, including how to cut
a release by hand.

## Licence

Forge is MIT licensed. By contributing you agree that your contribution is
licensed under the same terms. See [LICENSE](LICENSE).
