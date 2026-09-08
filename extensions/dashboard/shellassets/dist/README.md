# Dashboard shell artifact

`index.html` and this README are the placeholder, and they are the only two
files that belong in here. Anything else you see is an unpacked artifact.
**Do not commit a built shell here.** An earlier design committed an 11MB `dist`
and embedded it with `all:dist`, pulling every byte into every binary that
imported the dashboard extension, 6MB of which was sourcemaps.

`embed.go` uses a plain `//go:embed dist` today, and dropping the `all:` is
load-bearing rather than tidying: the default rules skip dotfiles, which is
what keeps the fetch script's own `.fetched.sha256` marker out of the binary.

The real shell is built and published by
[`xraph/forge-dashboard`](https://github.com/xraph/forge-dashboard) as a
release tarball. On release, this repo's CI downloads that tarball and unpacks
it over this directory before it builds. The placeholder is here only so that
`//go:embed` finds files at build time, which keeps `go build ./...` working
offline and in every consumer's CI.

`shellassets.IsPlaceholder()` reports whether what is embedded is this
placeholder, and the dashboard extension logs a warning at startup when it is.

## Which shell a release ships

Two files at the repo root decide that, and they move together:

| File | Holds |
| --- | --- |
| `.dashboard-shell-version` | a bare semver, `1.4.0`, no leading `v` |
| `.dashboard-shell-version.sha256` | the SHA-256 of that release's tarball |

Both currently read `none`. That is the sentinel for "no shell release is
pinned yet", and it is a real state rather than a broken one: the shell ships
from its own repo on its own cadence, and until the first tag exists there is
nothing to point at. `scripts/fetch-dashboard-shell.sh` sees the sentinel,
skips the download, leaves this placeholder in place, and exits 0 even in CI,
warning loudly that a release built this way serves a placeholder page.

The digest is not belt-and-braces. A GitHub release asset can be deleted and
re-uploaded under an existing tag with no commit and no trace in git history,
so a version pin fixes the artifact's *name* and nothing about its contents.
Without the digest, write access to that repo's releases is write access to
the JavaScript in every Forge binary. The script treats a mismatch as a hard
failure in every mode, and refuses a pinned version that has no digest at all.

### Bumping to a real release

1. Someone with release rights on `xraph/forge-dashboard` tags `vX.Y.Z`
   there. Its `release.yml` builds `apps/shell`, packs
   `forge-dashboard-shell-vX.Y.Z.tar.gz`, and attaches it to the release.
   Nothing in this repo can do that step, and nothing here should try.
2. Download that asset and take its digest:

   ```bash
   VERSION=X.Y.Z   # bare, no leading v
   curl -fL -O \
     "https://github.com/xraph/forge-dashboard/releases/download/v$VERSION/forge-dashboard-shell-v$VERSION.tar.gz"
   shasum -a 256 "forge-dashboard-shell-v$VERSION.tar.gz"
   ```

3. In this repo, write the version into `.dashboard-shell-version` and the
   64-character hash into `.dashboard-shell-version.sha256`, and commit both
   in one commit. Bumping one without the other fails the build, on purpose.
4. Then tag the forge release. The order matters: the shell tag has to exist,
   with its asset attached, before this repo's release CI reaches the
   before-hook that fetches it.

Rolling back is the same procedure with an older version and its own digest.
Set both files back to `none` to fall back to the placeholder deliberately.

## Fetching the real artifact by hand

You need this only if you want a working dashboard UI out of a binary you
built yourself.

```bash
VERSION=0.0.0   # bare, no leading v -- pick a forge-dashboard release
cd extensions/dashboard/shellassets

curl -fL -o "/tmp/forge-dashboard-shell-v$VERSION.tar.gz" \
  "https://github.com/xraph/forge-dashboard/releases/download/v$VERSION/forge-dashboard-shell-v$VERSION.tar.gz"

rm -rf dist/*
tar -xzf "/tmp/forge-dashboard-shell-v$VERSION.tar.gz" -C dist/
```

The bare version, with the `v` written out where a tag or a filename needs
one, is the same convention `.dashboard-shell-version` uses. The tarball has
`index.html` and `assets/` at its root, with no `dist/` wrapper, so it unpacks
straight over this directory.

Then rebuild. When you are done, restore the placeholder so you do not commit
the artifact:

```bash
git checkout -- extensions/dashboard/shellassets/dist/
git clean -fdx extensions/dashboard/shellassets/dist/
```

The `-x` matters. `.gitignore` deliberately ignores everything in here except
the two placeholder files, so the unpacked assets are invisible to plain
`git clean` and would be left sitting on disk.

## Building it from source instead

```bash
git clone https://github.com/xraph/forge-dashboard
cd forge-dashboard && pnpm install
pnpm --filter @forge-go/dashboard-shell build
tar -czf /tmp/shell.tar.gz -C apps/shell/dist .
```

Unpack that tarball the same way. To drive the fetch script against it rather
than unpacking by hand, pass it the digest too, since no committed one
describes a tarball you built:

```bash
DASHBOARD_SHELL_VERSION=0.0.0 \
DASHBOARD_SHELL_URL=file:///tmp/shell.tar.gz \
DASHBOARD_SHELL_SHA256="$(shasum -a 256 /tmp/shell.tar.gz | awk '{print $1}')" \
  scripts/fetch-dashboard-shell.sh
```
