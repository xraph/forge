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
#   DASHBOARD_SHELL_SHA256    Expect this SHA-256 of the downloaded tarball
#                             instead of reading
#                             .dashboard-shell-version.sha256. Needed
#                             whenever DASHBOARD_SHELL_URL points somewhere
#                             the committed digest does not describe.
#   DASHBOARD_SHELL_URL       Fetch this URL instead of constructing the
#                             GitHub release URL. Accepts file:// URLs, so
#                             a locally built tarball can be exercised
#                             without touching the network:
#                               DASHBOARD_SHELL_VERSION=0.0.0 \
#                               DASHBOARD_SHELL_URL=file:///tmp/forge-dashboard-shell-v0.0.0.tar.gz \
#                               DASHBOARD_SHELL_SHA256="$(shasum -a 256 /tmp/forge-dashboard-shell-v0.0.0.tar.gz | awk '{print $1}')" \
#                                 scripts/fetch-dashboard-shell.sh
#   DASHBOARD_SHELL_STRICT    "1" to fail loudly when the artifact can't be
#                             reached, "0" to warn and keep the current
#                             dist/ instead. Defaults to strict when CI or
#                             GITHUB_ACTIONS is "true", soft otherwise. See
#                             "Reachability" below.
#   DASHBOARD_SHELL_FORCE     "1" to skip the uncommitted-changes guard
#                             (see "Guard" below) unconditionally.
#
# The version pin (and the "nothing pinned yet" sentinel):
#   .dashboard-shell-version holds a bare semver -- 1.4.0, no leading "v".
#   The tag and the asset name both carry the v, and this script adds it. A
#   leading v in the file is tolerated and stripped, because the mistake is
#   easy to make and "download/vv1.4.0/" is a confusing way to find out.
#
#   An empty file, or the literal "none", is the sentinel for "no shell
#   release is pinned yet". That is a real state, not a failure: the shell
#   lives in a separate repo on its own release cadence, and this one has to
#   keep building before the first tarball exists. The sentinel skips the
#   fetch entirely, leaves the committed placeholder in dist/, and exits 0 --
#   in CI as well as out of it, which is the one place the STRICT rule below
#   does not apply. It warns loudly on the way out, because a release built
#   this way ships a dashboard that renders a placeholder page.
#
# Integrity (why a digest sits beside the version):
#   GitHub release assets are mutable. An asset can be deleted and
#   re-uploaded on an existing tag, with no commit and no trace in git
#   history, so pinning a version pins a name and not a single byte of
#   content. Anybody who can write to xraph/forge-dashboard's releases could
#   otherwise put arbitrary JavaScript into every Forge binary's dashboard,
#   and this script would fetch it, verify that it is a well-formed tar with
#   an index.html, and embed it.
#
#   So .dashboard-shell-version.sha256 pins the content next to the version
#   pinning the name, and the downloaded tarball is checked against it before
#   anything is unpacked. A mismatch is a hard failure in every mode, CI or
#   not, strict or not: unlike an unreachable artifact, it is not a network
#   condition, and there is no reading of it that is safe to continue past.
#   A pinned version with no digest available is refused for the same reason
#   -- an unverified artifact and a wrong one are the same risk.
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
DIGEST_FILE=".dashboard-shell-version.sha256"
DIST_DIR="extensions/dashboard/shellassets/dist"
MARKER="$DIST_DIR/.fetched.sha256"
GITHUB_REPO="xraph/forge-dashboard"

mkdir -p "$DIST_DIR"

# --- 1. Resolve the version -------------------------------------------------

if [ -n "${DASHBOARD_SHELL_VERSION:-}" ]; then
  VERSION="$(printf '%s' "$DASHBOARD_SHELL_VERSION" | tr -d '[:space:]')"
  VERSION_SOURCE='the DASHBOARD_SHELL_VERSION environment variable'
else
  [ -f "$VERSION_FILE" ] || die "$VERSION_FILE not found (expected at repo root)"
  VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
  VERSION_SOURCE="$VERSION_FILE"
fi

# The sentinel. Empty or "none" means no shell release is pinned yet, which
# is a state this repo has to be able to sit in: the shell ships from another
# repo, and the first tarball does not exist until somebody tags it. Warn
# loudly, keep the placeholder, and exit 0 -- deliberately including CI, so a
# release cut before the first shell tag produces a working binary with a
# placeholder dashboard rather than a failed before-hook.
if [ -z "$VERSION" ] || [ "$(printf '%s' "$VERSION" | tr '[:upper:]' '[:lower:]')" = "none" ]; then
  warn "================================================================"
  warn "NO DASHBOARD SHELL PINNED."
  warn "$VERSION_SOURCE is ${VERSION:-empty} (the \"nothing pinned yet\" sentinel)."
  warn "$DIST_DIR keeps the committed PLACEHOLDER, so any binary built from"
  warn "this tree serves a placeholder page at {BasePath}/ui, not the real"
  warn "dashboard. If you are cutting a release, this is what it will ship."
  warn "To pin a real shell, see $DIST_DIR/README.md."
  warn "================================================================"
  exit 0
fi

# One leading "v" is tolerated and stripped: the file holds a bare semver,
# the tag and asset name carry the v, and this script is what adds it.
# Without this, a file saying "v1.4.0" builds a URL under "download/vv1.4.0/"
# and fails with a 404 that names neither the cause nor the fix.
VERSION="${VERSION#v}"

if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$'; then
  die "$VERSION_SOURCE holds \"$VERSION\", which is not a bare semver.
  Expected something like 1.4.0 (no leading \"v\" -- this script adds it),
  or the sentinel \"none\" for \"no shell pinned yet\"."
fi

ASSET="forge-dashboard-shell-v${VERSION}.tar.gz"
URL="${DASHBOARD_SHELL_URL:-https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/${ASSET}}"

# --- 1b. Resolve the expected digest ----------------------------------------
#
# Resolved here, before the download, so a missing or malformed pin fails on
# configuration rather than after a network round trip. See "Integrity" in
# the header for why a version pin alone is not enough.

if [ -n "${DASHBOARD_SHELL_SHA256:-}" ]; then
  EXPECTED_SHA="$DASHBOARD_SHELL_SHA256"
  DIGEST_SOURCE='the DASHBOARD_SHELL_SHA256 environment variable'
else
  [ -f "$DIGEST_FILE" ] || die "$VERSION_SOURCE pins v$VERSION but $DIGEST_FILE does not exist.
  A pinned version names an artifact; it does not pin its bytes, because a
  GitHub release asset can be replaced in place. Record the digest with:
    shasum -a 256 $ASSET
  and write the 64-character hash into $DIGEST_FILE.
  For a one-off local run against your own tarball, set DASHBOARD_SHELL_SHA256."
  # First field of the first non-empty line, so both a bare hash and the
  # "<hash>  <filename>" that shasum itself prints are accepted.
  EXPECTED_SHA="$(awk 'NF {print $1; exit}' "$DIGEST_FILE")"
  DIGEST_SOURCE="$DIGEST_FILE"
fi

EXPECTED_SHA="$(printf '%s' "$EXPECTED_SHA" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')"
EXPECTED_SHA="${EXPECTED_SHA#sha256:}"

if ! printf '%s' "$EXPECTED_SHA" | grep -Eq '^[0-9a-f]{64}$'; then
  die "$DIGEST_SOURCE does not hold a SHA-256 digest (got: \"${EXPECTED_SHA:-<empty>}\").
  Expected 64 hex characters, as printed by: shasum -a 256 $ASSET
  The version and the digest are bumped together -- if one still reads
  \"none\" while the other names a release, only one of the two moved."
fi

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
# corrupt or wrongly-shaped tarball. Every check below is an unconditional
# hard failure regardless of STRICT: something was downloaded, so this is
# no longer a reachability problem.
#
# The digest goes first. The structural checks that follow it answer "is this
# a dashboard shell"; only this one answers "is this THE dashboard shell we
# pinned", and a substituted artifact would sail through the other two.

ACTUAL_SHA="$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')"
if [ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]; then
  die "SHA-256 mismatch on the downloaded artifact. NOT unpacking it.
  url:      $URL
  expected: $EXPECTED_SHA  (from $DIGEST_SOURCE)
  actual:   $ACTUAL_SHA
  Either the release asset was replaced after the digest was recorded, or
  the pin is stale. Confirm which before touching either file: a release
  asset changing under a fixed tag is exactly what this check exists for."
fi

log "verified: SHA-256 matches the digest pinned in $DIGEST_SOURCE"

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
