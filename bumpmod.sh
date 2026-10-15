#!/usr/bin/env bash
#
# bumpmod.sh: set the required version of every locally replaced module.
#
# In a repo with nested Go modules, a nested go.mod usually carries a pair like
#
#     replace github.com/acme/lib => ../..
#     require github.com/acme/lib v1.2.0
#
# The replace points at the checkout, so the version on the require line is only
# a label. This script rewrites that label, in every go.mod under a directory,
# for every module the go.mod replaces with a local path. It needs no network,
# changes nothing else, and knows nothing about the repo it runs in, so you can
# copy it into any Go repo.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bumpmod.sh [-n] [-t] [-d DIR] VERSION [MODULE...]

Set the required version of every module that a go.mod replaces with a local
path (a replace target starting with ./ or ../ or /).

  VERSION   Version to require, such as v1.13.0. The leading v is optional.
  MODULE    Only bump these modules. Shell-style globs work, so quote them:
              bumpmod.sh v1.13.0 github.com/xraph/forge
              bumpmod.sh v1.13.0 'github.com/xraph/forge/extensions/*'
            With no MODULE, every locally replaced module is bumped.

Options (they go before VERSION):
  -n        Dry run. Print what would change and write nothing.
  -t        Run go mod tidy in each go.mod that changed.
  -d DIR    Look for go.mod files under DIR instead of the current directory.
  -h        Show this help.

Notes:
  - Only modules that are both replaced locally and already required are
    touched. A replace with no require line is left alone.
  - A replace pinned to one version (replace M v1.2.0 => ../m) is skipped with
    a warning, because bumping the require would stop the replace matching.
  - node_modules, vendor, .git and testdata directories are not searched.
  - The version is not checked against any tag. Local replaces hide a missing
    tag until you drop them, so make sure the tag exists before you release.

Exit status is 1 if any go.mod could not be edited.
EOF
}

die() {
  echo "bumpmod.sh: $*" >&2
  exit 2
}

DRY=0
TIDY=0
ROOT=.
while getopts ":ntd:h" opt; do
  case $opt in
    n) DRY=1 ;;
    t) TIDY=1 ;;
    d) ROOT=$OPTARG ;;
    h) usage; exit 0 ;;
    :) usage >&2; die "option -$OPTARG needs a value" ;;
    *) usage >&2; die "unknown option -$OPTARG" ;;
  esac
done
shift $((OPTIND - 1))

[ $# -ge 1 ] || { usage >&2; die "missing VERSION"; }
TYPED=$1
shift
PATTERNS=("$@")

case $TYPED in
  v*) VERSION=$TYPED ;;
  *) VERSION=v$TYPED ;;
esac
VERSION_RE='^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$'
[[ $VERSION =~ $VERSION_RE ]] || die "'$TYPED' is not a version like v1.2.3 or v1.2.3-rc.1"

command -v go >/dev/null 2>&1 || die "go is not on PATH"
[ -d "$ROOT" ] || die "'$ROOT' is not a directory"

# True when the module matches one of the MODULE patterns, or none were given.
matches() {
  [ "${#PATTERNS[@]}" -eq 0 ] && return 0
  local p
  for p in "${PATTERNS[@]}"; do
    # shellcheck disable=SC2254  # $p is meant to be a glob
    case $1 in $p) return 0 ;; esac
  done
  return 1
}

# Reads one go.mod and prints "module<TAB>required version<TAB>pinned" for each
# module that is both required and replaced with a local path. "pinned" is the
# replaced version, or "-" when the replace covers every version.
read -r -d '' AWK_PROG <<'EOF' || true
function unquote(s) { gsub(/^["`]|["`]$/, "", s); return s }
function islocal(p) { return p == "." || p == ".." || p ~ /^\.\.?\// || p ~ /^\// }
function shift_tokens(   i) { for (i = 2; i <= nt; i++) t[i - 1] = t[i]; nt-- }
function handle_replace(   i, arrow, old, oldv, new, newv) {
  arrow = 0
  for (i = 1; i <= nt; i++) if (t[i] == "=>") { arrow = i; break }
  if (arrow < 2 || arrow + 1 > nt) return
  old = unquote(t[1])
  oldv = (arrow >= 3) ? t[2] : "-"
  new = unquote(t[arrow + 1])
  newv = (arrow + 2 <= nt) ? t[arrow + 2] : "-"
  if (newv == "-" && islocal(new)) loc[old] = oldv
}
{
  line = $0
  sub(/\/\/.*$/, "", line)
  gsub(/^[ \t\r]+|[ \t\r]+$/, "", line)
  if (line == "") next
  nt = split(line, t, /[ \t]+/)

  if (block != "") {
    if (t[1] == ")") { block = ""; next }
    if (block == "require") { if (nt >= 2) req[unquote(t[1])] = t[2] }
    else if (block == "replace") handle_replace()
    next
  }

  kw = t[1]
  if (kw == "require" || kw == "replace" || kw == "exclude" || kw == "retract" || kw == "tool" || kw == "godebug" || kw == "ignore") {
    if (nt >= 2 && t[2] == "(") { block = kw; next }
    shift_tokens()
    if (kw == "require") { if (nt >= 2) req[unquote(t[1])] = t[2] }
    else if (kw == "replace") handle_replace()
  }
}
END {
  for (m in loc) if (m in req) printf "%s\t%s\t%s\n", m, req[m], loc[m]
}
EOF

GOMODS=()
while IFS= read -r -d '' f; do
  GOMODS+=("$f")
done < <(find "$ROOT" \( -name node_modules -o -name vendor -o -name .git -o -name testdata \) -prune \
  -o -name go.mod -type f -print0 | sort -z)

if [ "${#GOMODS[@]}" -eq 0 ]; then
  echo "No go.mod files found under $ROOT."
  exit 0
fi

bumped=0 files=0 current=0 skipped=0 failed=0

for gomod in "${GOMODS[@]}"; do
  edits=0
  while IFS=$'\t' read -r mod cur pin; do
    [ -n "$mod" ] || continue
    matches "$mod" || continue

    if [ "$pin" != "-" ]; then
      echo "$gomod: skipped $mod, its replace is pinned to $pin" >&2
      skipped=$((skipped + 1))
      continue
    fi
    if [ "$cur" = "$VERSION" ]; then
      current=$((current + 1))
      continue
    fi

    echo "$gomod: $mod $cur -> $VERSION"
    if [ "$DRY" -eq 1 ]; then
      bumped=$((bumped + 1))
      edits=$((edits + 1))
    elif go mod edit -require="$mod@$VERSION" "$gomod"; then
      bumped=$((bumped + 1))
      edits=$((edits + 1))
    else
      echo "$gomod: could not set $mod to $VERSION" >&2
      failed=$((failed + 1))
    fi
  done < <(awk "$AWK_PROG" "$gomod" | sort)

  if [ "$edits" -gt 0 ]; then
    files=$((files + 1))
    if [ "$TIDY" -eq 1 ] && [ "$DRY" -eq 0 ]; then
      echo "$gomod: running go mod tidy"
      (cd "$(dirname "$gomod")" && go mod tidy) || {
        echo "$gomod: go mod tidy failed" >&2
        failed=$((failed + 1))
      }
    fi
  fi
done

verb="Bumped"
[ "$DRY" -eq 1 ] && verb="Would bump"
echo "$verb $bumped requires in $files of ${#GOMODS[@]} go.mod files to $VERSION" \
  "($current already there, $skipped skipped, $failed failed)."
[ "$DRY" -eq 1 ] && echo "Dry run: nothing was written."

[ "$failed" -eq 0 ]
