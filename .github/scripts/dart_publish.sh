#!/usr/bin/env bash
# Publishes packages under dart-packages/ to pub.dev, one at a time, in the
# order given. .github/workflows/dart-publish.yml runs it; that file says why
# the order is what it is.
#
#     dart_publish.sh PACKAGE...
#
# Settings come from the environment:
#
#   VERSION        X.Y.Z stamped on every package, with every dependency on a
#                  sibling raised to ^X.Y.Z. Empty keeps each pubspec's own.
#   DRY_RUN        true (the default) or false.
#   PUB_API        pub.dev's base URL. Only the tests change it.
#   WAIT_ATTEMPTS  How many times to look for a version after publishing it.
#   WAIT_SECONDS   How long to sleep between looks. 60 x 10s by default.
#
# A dry run validates each package the way dart-packages.yml's dry run does:
# hosted constraints for pub's validator, with the local copies kept as
# dependency overrides, so a package still resolves when the sibling it needs
# is not on pub.dev at that version. It publishes nothing and never calls
# pub.dev's API.
#
# A real run drops the overrides and resolves against live pub.dev, so each
# package is validated against what its users will download. That is why the
# order matters, and why the script waits for each version to go live before
# it moves on. It skips any version pub.dev already has, so re-running a run
# that failed halfway finishes the job instead of failing on the first
# package. On failure it says what was published and what was not.
#
# Runs under the bash 3.2 macOS ships, so the rehearsal and the tests can run
# it on a laptop: no mapfile, and no expanding an empty array under set -u.

set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PUB_API=${PUB_API:-https://pub.dev}
WAIT_ATTEMPTS=${WAIT_ATTEMPTS:-60}
WAIT_SECONDS=${WAIT_SECONDS:-10}

done_list=""
skipped_list=""
current=""
remaining=""
dry_run=true

say() {
  echo "$1"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$1" >> "$GITHUB_STEP_SUMMARY"
  fi
}

or_none() {
  if [ -n "$1" ]; then echo "$1"; else echo "none"; fi
}

# Prints the HTTP status pub.dev answers for PACKAGE VERSION, or 000 when
# there is no answer at all.
version_status() {
  curl -sS -o /dev/null -w '%{http_code}' --max-time 30 \
    "$PUB_API/api/packages/$1/versions/$2" || true
}

# Succeeds if pub.dev has PACKAGE VERSION, fails if it does not, and stops the
# run on any other answer rather than guess.
already_published() {
  local code
  code=$(version_status "$1" "$2")
  case "$code" in
    200) return 0 ;;
    404) return 1 ;;
  esac
  echo "::error::pub.dev answered $code for $1 $2, so it is not known whether that version exists." >&2
  exit 1
}

# pub resolves the next package against live pub.dev, so it cannot start
# until the one it depends on is served there.
wait_until_live() {
  local attempt=1
  while [ "$attempt" -le "$WAIT_ATTEMPTS" ]; do
    if [ "$(version_status "$1" "$2")" = 200 ]; then
      echo "$1 $2 is live on pub.dev."
      return 0
    fi
    sleep "$WAIT_SECONDS"
    attempt=$((attempt + 1))
  done
  echo "::error::$1 $2 was published, but pub.dev did not serve it within $((WAIT_ATTEMPTS * WAIT_SECONDS))s." >&2
  return 1
}

# pub.dev's package listing, which `pub get` reads, can trail the version
# endpoint wait_until_live polls, so a real run gives resolution a few tries.
resolve() {
  local attempt=1
  while ! "$1" pub get --no-example; do
    if [ "$attempt" -ge 6 ]; then
      return 1
    fi
    echo "pub get failed (attempt $attempt of 6). Retrying in ${WAIT_SECONDS}s." >&2
    sleep "$WAIT_SECONDS"
    attempt=$((attempt + 1))
  done
}

# setup-dart registered the pub.dev token as "read it from PUB_TOKEN". A
# GitHub OIDC token is short-lived and a run waits between packages, so the
# value is minted again right before every upload.
refresh_pub_token() {
  if [ -z "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] || [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]; then
    echo "::error::No OIDC token to request. The publish job needs permissions: id-token: write." >&2
    return 1
  fi
  local token
  token=$(curl -sSf --max-time 30 \
    -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
    "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=https://pub.dev" |
    python3 -c 'import json, sys; print(json.load(sys.stdin)["value"])')
  echo "::add-mask::$token"
  export PUB_TOKEN="$token"
}

report() {
  local status=$1
  trap - EXIT
  if [ -n "$current" ]; then
    echo "::endgroup::"
  fi
  if [ "$dry_run" = true ]; then
    if [ "$status" -eq 0 ]; then
      say "Dry run passed: $(or_none "$done_list")."
    else
      say "Dry run failed on ${current:-setup}. Passed before it: $(or_none "$done_list")."
    fi
    exit "$status"
  fi
  say "Published: $(or_none "$done_list")."
  say "Already on pub.dev, skipped: $(or_none "$skipped_list")."
  if [ "$status" -ne 0 ]; then
    say "Failed: ${current:-setup}. Not attempted: $(or_none "$remaining")."
    say "Fix the cause and re-run. Versions already on pub.dev are skipped."
  fi
  exit "$status"
}

flutter_or_dart() {
  if grep -Eq '^[[:space:]]+sdk:[[:space:]]*flutter[[:space:]]*$' pubspec.yaml; then
    echo flutter
  else
    echo dart
  fi
}

main() {
  if [ $# -eq 0 ]; then
    echo "usage: dart_publish.sh PACKAGE..." >&2
    exit 2
  fi
  dry_run=${DRY_RUN:-true}
  case "$dry_run" in
    true | false) ;;
    *)
      echo "dart_publish: DRY_RUN must be true or false, got '$dry_run'" >&2
      exit 2
      ;;
  esac

  local root pkg tool version
  root=$(git rev-parse --show-toplevel)
  remaining="$*"
  trap 'report $?' EXIT

  for pkg in "$@"; do
    current=$pkg
    remaining=${remaining#"$pkg"}
    remaining=${remaining# }
    echo "::group::$pkg"
    cd "$root/dart-packages/$pkg"
    tool=$(flutter_or_dart)

    # The same rewrite as dart-packages.yml's dry run, plus the release
    # version, which CHANGELOG.md has to mention or pub warns. It only ever
    # happens on the runner.
    if [ -n "${VERSION:-}" ]; then
      python3 "$here/dart_ci_pubspec.py" . --hosted --version "$VERSION" --changelog
    else
      python3 "$here/dart_ci_pubspec.py" . --hosted
    fi
    # pub warns (and --dry-run exits 65) on checked-in files modified in git.
    git update-index --assume-unchanged pubspec.yaml CHANGELOG.md
    version=$(sed -n 's/^version:[[:space:]]*//p' pubspec.yaml)

    if [ "$dry_run" = true ]; then
      "$tool" pub get --no-example
      "$tool" pub publish --dry-run
      done_list="${done_list:+$done_list, }$pkg $version"
    elif already_published "$pkg" "$version"; then
      echo "$pkg $version is already on pub.dev. Skipping it."
      skipped_list="${skipped_list:+$skipped_list, }$pkg $version"
    else
      rm -f pubspec_overrides.yaml
      resolve "$tool"
      "$tool" pub publish --dry-run
      refresh_pub_token
      "$tool" pub publish --force
      done_list="${done_list:+$done_list, }$pkg $version"
      wait_until_live "$pkg" "$version"
    fi

    echo "::endgroup::"
    current=""
  done
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
