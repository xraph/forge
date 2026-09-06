# Dashboard shell artifact

`index.html` and this README are the placeholder, and they are the only two
files that belong in here. Anything else you see is an unpacked artifact.
**Do not commit a built shell here.** An earlier design committed an 11MB `dist`
and `//go:embed all:dist` pulled every byte of it into every binary that
imported the dashboard extension, 6MB of which was sourcemaps.

The real shell is built and published by
[`xraph/forge-dashboard`](https://github.com/xraph/forge-dashboard) as a
release tarball. On release, this repo's CI downloads that tarball and unpacks
it over this directory before it builds. The placeholder is here only so that
`//go:embed` finds files at build time, which keeps `go build ./...` working
offline and in every consumer's CI.

`shellassets.IsPlaceholder()` reports whether what is embedded is this
placeholder, and the dashboard extension logs a warning at startup when it is.

## Fetching the real artifact by hand

You need this only if you want a working dashboard UI out of a binary you
built yourself.

```bash
VERSION=v0.0.0   # pick a forge-dashboard release tag
cd extensions/dashboard/shellassets

curl -fL -o /tmp/forge-dashboard-shell-$VERSION.tar.gz \
  "https://github.com/xraph/forge-dashboard/releases/download/$VERSION/forge-dashboard-shell-$VERSION.tar.gz"

rm -rf dist/*
tar -xzf /tmp/forge-dashboard-shell-$VERSION.tar.gz -C dist/
```

The tarball has `index.html` and `assets/` at its root, with no `dist/`
wrapper, so it unpacks straight over this directory.

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

Unpack that tarball the same way.
