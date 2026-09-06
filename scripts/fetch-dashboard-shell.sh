#!/usr/bin/env bash
#
# Fetches the built dashboard shell (apps/shell in xraph/forge-dashboard) and
# unpacks it over extensions/dashboard/shellassets/dist/, replacing the
# committed placeholder with the real artifact before //go:embed reads that
# directory at build time.
#
# Called from .goreleaser.yml's before.hooks (after the go.work hook, which
# must stay first -- see the comment there). That means this script runs on
# EVERY goreleaser invocation, including a developer's local
# `goreleaser --snapshot`, not just a real release. A local run with no
# network must still succeed and leave the placeholder in place: see
# "Reachability" below.
#
# Usage (from repo root, or anywhere -- it cd's to the repo root itself):
#   scripts/fetch-dashboard-shell.sh
#
# Environment overrides (for local testing and debugging -- none of these
# are needed for a normal run):
#   DASHBOARD_SHELL_VERSION   Use this version instead of reading
#                             .dashboard-shell-version.
#   DASHBOARD_SHELL_URL       Fetch this URL instead of constructing the
#                             GitHub release URL. Accepts file:// URLs, so
#                             a locally built tarball can be exercised
#                             without touching the network:
#                               DASHBOARD_SHELL_URL=file:///tmp/forge-dashboard-shell-v0.0.0.tar.gz \
#                                 scripts/fetch-dashboard-shell.sh
#   DASHBOARD_SHELL_STRICT    "1" to fail loudly when the artifact can't be
#                             reached, "0" to warn and keep the current
#                             dist/ instead. Defaults to strict when CI or
#                             GITHUB_ACTIONS is "true", soft otherwise. See
#                             "Reachability" below.
#   DASHBOARD_SHELL_FORCE     "1" to skip the uncommitted-changes guard
#                             (see "Guard" below) unconditionally.
#
# Reachability (soft-fail vs. hard-fail):
#   This repo's release.yml only runs this workflow on a tag push or an
#   explicit workflow_dispatch -- never on every PR -- so both the real
#   release job (release-cli, a reusable workflow call) and its rehearsal
#   (the snapshot dry-run job) only fire when someone is deliberately
#   cutting or rehearsing a release. In either case, running on a GitHub
#   Actions runner (CI=true / GITHUB_ACTIONS=true, set by the platform
#   itself, not by our workflow file) means the artifact SHOULD be
#   reachable, and a fetch failure there is a real problem worth stopping
#   the release for -- so that path is strict.
#
#   A developer running `goreleaser --snapshot` on their laptop is a
#   different situation: they may be offline, and they are not publishing
#   anything. Failing their local build over a network hiccup, or because
#   no forge-dashboard release has been tagged yet, would make the CLI
#   unbuildable for no good reason. So that path warns and leaves dist/
#   untouched (the committed placeholder, or whatever a previous run left
#   there), and exits 0.
#
#   This distinction applies ONLY to "the artifact could not be reached at
#   all" (DNS/connection failure, HTTP error, missing tag). It does NOT
#   apply to "the artifact was reached but is broken" -- a truncated
#   download, a corrupt archive, or one with the wrong internal layout is
#   always a hard failure, in CI or out of it. A response that arrived is
#   not a network problem; unpacking it anyway would silently ship (or
#   locally build) garbage instead of failing loudly.
#
#   KNOWN LIMITATION: nothing in GoReleaser's before.hooks interface
#   distinguishes "a real `goreleaser release`" from any other invocation.
#   CI=true / GITHUB_ACTIONS=true is a proxy for "this is the blessed
#   pipeline," not a guarantee of it. A maintainer running
#   `goreleaser release --clean` by hand outside CI, with a real
#   GORELEASER_TOKEN, bypassing release.yml entirely, would hit the SOFT
#   path on any network hiccup and could publish a binary with the
#   placeholder embedded, silently. No stronger signal is available to a
#   before-hook -- there is nothing to check it against. Mitigation: cut
#   releases through the workflow, not by hand; if you must run
#   `goreleaser release` outside CI, set DASHBOARD_SHELL_STRICT=1 yourself.
#
# Guard (protecting uncommitted local changes):
#   extensions/dashboard/shellassets/dist/index.html and README.md are
#   tracked files (see the repo .gitignore) so a fresh clone builds without
#   the real artifact. Unpacking a real artifact over them necessarily
#   modifies tracked files -- that is not a bug, it is the whole point of
#   this script -- but it means a naive "refuse if dist/ is dirty" guard
#   would block every run after the first, since the first run's own
#   output is exactly what makes the tree dirty.
#
#   To tell "dirt this script produced" apart from "a local experiment that
#   would be silently clobbered," every successful run records a hash of
#   the tree it just unpacked in dist/.fetched.sha256 (a dotfile inside
#   dist/, which the repo .gitignore already excludes since it only
#   un-ignores index.html and README.md -- so it never gets committed and
#   `git clean -fdx` removes it along with the rest of the real artifact).
#   On the next run:
#     - a clean tree proceeds normally;
#     - a dirty tree whose contents still match the recorded hash is this
#       script's own prior output, and proceeds;
#     - a dirty tree that does not match (no marker, or a marker from a
#       different tree) is presumed to be someone's local edit, and the
#       script refuses, naming the two commands that clear it deliberately
#       (git checkout, then git clean -fdx, since checkout alone cannot
#       touch the ignored real-artifact files) plus the force override.

set -euo pipefail

log()  { printf '[fetch-dashboard-shell] %s\n' "$*" >&2; }
warn() { printf '[fetch-dashboard-shell] warning: %s\n' "$*" >&2; }
die()  { printf '[fetch-dashboard-shell] error: %s\n' "$*" >&2; exit 1; }

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

VERSION_FILE=".dashboard-shell-version"
DIST_DIR="extensions/dashboard/shellassets/dist"
MARKER="$DIST_DIR/.fetched.sha256"
GITHUB_REPO="xraph/forge-dashboard"

mkdir -p "$DIST_DIR"

# --- 1. Resolve the version -------------------------------------------------

if [ -n "${DASHBOARD_SHELL_VERSION:-}" ]; then
  VERSION="$DASHBOARD_SHELL_VERSION"
else
  [ -f "$VERSION_FILE" ] || die "$VERSION_FILE not found (expected at repo root)"
  VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
  [ -n "$VERSION" ] || die "$VERSION_FILE is empty"
fi

ASSET="forge-dashboard-shell-v${VERSION}.tar.gz"
URL="${DASHBOARD_SHELL_URL:-https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/${ASSET}}"

# --- 2. Decide strict vs. soft-fail on an unreachable artifact --------------

if [ -n "${DASHBOARD_SHELL_STRICT:-}" ]; then
  STRICT="$DASHBOARD_SHELL_STRICT"
elif [ "${CI:-}" = "true" ] || [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  STRICT=1
else
  STRICT=0
fi

# --- 3. Guard: refuse to clobber changes this script didn't make -----------

tree_hash() {
  # Stable hash of every file under $DIST_DIR except the marker itself:
  # path-sorted "sha256  path" lines, hashed again as a single blob.
  find "$DIST_DIR" -type f ! -name "$(basename "$MARKER")" -print0 \
    | LC_ALL=C sort -z \
    | xargs -0 shasum -a 256 \
    | shasum -a 256 \
    | awk '{print $1}'
}

if [ "${DASHBOARD_SHELL_FORCE:-0}" = "1" ]; then
  warn "DASHBOARD_SHELL_FORCE=1: skipping the uncommitted-changes guard"
else
  # Captured and checked separately -- not inlined into the `[ -n ... ]` test
  # below -- so that a failing `git status` (not merely "no output") cannot
  # be misread as "clean" and silently skip the guard it is supposed to run.
  DIST_STATUS="$(git status --porcelain -- "$DIST_DIR")" \
    || die "git status failed while checking $DIST_DIR for uncommitted changes"

  if [ -n "$DIST_STATUS" ]; then
    if [ -f "$MARKER" ] && [ "$(cat "$MARKER")" = "$(tree_hash)" ]; then
      log "dist/ is dirty but matches this script's last recorded fetch; continuing"
    else
      die "uncommitted changes under $DIST_DIR that this script did not make.
  Restore with:  git checkout -- $DIST_DIR && git clean -fdx $DIST_DIR
  Or override with DASHBOARD_SHELL_FORCE=1 if you are sure."
    fi
  fi
fi

# --- 4. Download -------------------------------------------------------------

WORK_DIR="$(mktemp -d)"
STAGING_DIR=""
STALE_DIR=""
cleanup() {
  # Every statement here is best-effort: this runs on the EXIT trap, under
  # `set -e`, and its own exit status would otherwise become the script's
  # exit status (bash re-raises a failing EXIT-trap command as the process's
  # final code) -- silently turning a successful run into a reported
  # failure. `|| true` on each, and an explicit `return 0`, keep cleanup
  # from ever overriding the real result.
  rm -rf "$WORK_DIR" || true
  if [ -n "$STAGING_DIR" ] && [ -d "$STAGING_DIR" ]; then rm -rf "$STAGING_DIR" || true; fi
  if [ -n "$STALE_DIR" ] && [ -d "$STALE_DIR" ]; then rm -rf "$STALE_DIR" || true; fi
  return 0
}
trap cleanup EXIT
ARCHIVE="$WORK_DIR/$ASSET"

log "fetching $URL"
if ! curl --fail --location --silent --show-error --output "$ARCHIVE" "$URL"; then
  MSG="could not fetch $URL"
  if [ "$STRICT" = "1" ]; then
    die "$MSG (STRICT mode -- set by CI, or DASHBOARD_SHELL_STRICT=1). Refusing to ship the placeholder as a real release."
  else
    warn "$MSG"
    warn "leaving $DIST_DIR untouched (not in CI; set DASHBOARD_SHELL_STRICT=1 to make this fatal)"
    exit 0
  fi
fi

# --- 5. Verify before unpacking ----------------------------------------------
#
# A 404 (or a captive-portal / proxy error page) can still produce a
# 200-shaped response body; --fail above catches the HTTP status, but not a
# corrupt or wrongly-shaped tarball. Both checks below are unconditional
# hard failures regardless of STRICT: something was downloaded, so this is
# no longer a reachability problem.

LISTING="$WORK_DIR/listing.txt"
if ! tar -tzf "$ARCHIVE" > "$LISTING" 2>"$WORK_DIR/tar-error.txt"; then
  die "downloaded archive is not a valid gzip tar: $(cat "$WORK_DIR/tar-error.txt")"
fi

if ! grep -qx './index.html' "$LISTING"; then
  die "archive does not have index.html at its root (got: $(head -5 "$LISTING" | tr '\n' ' ')...) -- check for an extra dist/ wrapper"
fi

log "verified: $ASSET has index.html at its root ($(wc -l < "$LISTING" | tr -d ' ') entries)"

# --- 6. Extract into a staging directory, then swap it into place -----------
#
# Extraction happens in a scratch directory, never in $DIST_DIR itself: if
# `tar -xzf` fails partway (disk full, an I/O error, a permission problem on
# one entry), the failure lands entirely in $STAGING_DIR and $DIST_DIR is
# never touched. Clearing $DIST_DIR first and extracting into it directly
# would leave an empty dist/ on exactly that failure -- which fails the
# //go:embed at compile time with an error nowhere near its real cause.
#
# The staging directory is created as a sibling of $DIST_DIR (same
# filesystem) rather than under $WORK_DIR (which may be a different
# filesystem, e.g. a tmpfs /tmp), so the swap below is two same-filesystem
# renames rather than a cross-filesystem copy -- fast, and the only window
# where dist/ could be observed missing is the instant between those two
# renames, not however long extraction takes.

STAGING_PARENT="$(dirname "$DIST_DIR")"
if ! STAGING_DIR="$(mktemp -d "${STAGING_PARENT}/.dist-staging.XXXXXX" 2>"$WORK_DIR/mktemp-error.txt")"; then
  die "could not create a staging directory for extraction: $(cat "$WORK_DIR/mktemp-error.txt")"
fi
chmod 755 "$STAGING_DIR"

if ! tar -xzf "$ARCHIVE" -C "$STAGING_DIR" 2>"$WORK_DIR/extract-error.txt"; then
  die "extraction into the staging directory failed (dist/ left untouched): $(cat "$WORK_DIR/extract-error.txt")"
fi

if [ ! -f "$STAGING_DIR/index.html" ]; then
  die "extraction incomplete: index.html missing from the staged output (dist/ left untouched)"
fi

STALE_DIR="${DIST_DIR}.stale.$$"
rm -rf "$STALE_DIR"
mv "$DIST_DIR" "$STALE_DIR" || die "could not move the current dist/ aside for the swap (dist/ unchanged)"
if ! mv "$STAGING_DIR" "$DIST_DIR"; then
  # Extremely unlikely (both renames are on the same filesystem, moments
  # apart) but worth guarding: put the previous contents straight back
  # rather than leave dist/ missing.
  mv "$STALE_DIR" "$DIST_DIR" 2>/dev/null || true
  die "could not move staged content into dist/; restored the previous dist/ contents"
fi
STAGING_DIR=""   # moved into place; nothing left for cleanup() to remove there
rm -rf "$STALE_DIR"
STALE_DIR=""

# --- 7. Record provenance for the guard on the next run ----------------------

tree_hash > "$MARKER"

log "unpacked $ASSET into $DIST_DIR"
