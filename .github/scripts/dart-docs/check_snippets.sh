#!/usr/bin/env bash
# Compile every ```dart block in the Dart client docs against the real
# packages, so a sample that drifts from the API fails here and not in a
# reader's editor.
#
#   .github/scripts/dart-docs/check_snippets.sh [DOCS_DIR]
#
# It generates two clients with the Dart generator in this tree:
# orders_forge_client from orders.openapi.json, which every block gets through
# the prelude, and catalog_forge_client from catalog.openapi.json plus
# catalog.asyncapi.json, which has what orders lacks (an enum, scopes, roles
# and permissions, a paginated list, a stream). A block that uses the catalog
# client imports it by name and so gets no prelude. It then checks that each
# generated excerpt on a page is real output (check_excerpts.py), builds a
# throwaway Flutter package that depends on every dart-packages/ package plus
# both clients, writes one file per block (extract_snippets.py) and runs the
# analyzer over them.
#
# Environment:
#   GROVE_DIR  a grove checkout holding crdt-dart/ (default: ../grove beside
#              this repository), needed because forge_client_grove depends
#              on grove_crdt
#   WORK_DIR   where to build (default: a fresh temporary directory)
#   PUB_GET_FLAGS  extra flags for `flutter pub get`; a run with no network
#              passes --offline (default: none)
#   GOPROXY    read by `go run` when it builds the generator. The script does
#              not set it, so a run with no network exports GOPROXY=off itself;
#              left alone, a cold module cache downloads
#
# Calls `fvm flutter` and `fvm dart`. In CI the workflow puts a stand-in fvm
# on PATH that runs the one Flutter it installed.
set -euo pipefail

for tool in go python3 fvm; do
  command -v "$tool" >/dev/null || { echo "check_snippets: $tool not found on PATH" >&2; exit 1; }
done

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
DOCS_DIR="$(cd "${1:-$ROOT/docs/content/docs/dart-client}" && pwd)"
GROVE_DIR="${GROVE_DIR:-$(cd "$ROOT/.." && pwd)/grove}"
WORK_DIR="${WORK_DIR:-$(mktemp -d)}"
mkdir -p "$WORK_DIR"
PACKAGES="$ROOT/dart-packages"

if [[ ! -f "$GROVE_DIR/crdt-dart/pubspec.yaml" ]]; then
  echo "check_snippets: no crdt-dart/pubspec.yaml under $GROVE_DIR; set GROVE_DIR" >&2
  exit 1
fi

echo "check_snippets: building in $WORK_DIR"

# cmd/forge pins the published root module, so a plain `go run` would
# generate with the last release. A workspace outside the repository points
# it at this tree, the same way go.yml's Build CLI job does.
WORKFILE="$WORK_DIR/forge.work"
rm -f "$WORKFILE"
GOWORK="$WORKFILE" go work init "$ROOT" "$ROOT/cmd/forge"

CLIENT="$WORK_DIR/orders_forge_client"
rm -rf "$CLIENT"
(
  cd "$ROOT/cmd/forge"
  GOWORK="$WORKFILE" go run . client generate \
    --from-spec "$HERE/orders.openapi.json" \
    --language dart \
    --output "$CLIENT" \
    --package orders_forge_client \
    --base-url http://localhost:8097 \
    --hooks
)

CATALOG="$WORK_DIR/catalog_forge_client"
rm -rf "$CATALOG"
(
  cd "$ROOT/cmd/forge"
  GOWORK="$WORKFILE" go run . client generate \
    --from-spec "$HERE/catalog.openapi.json" \
    --from-spec "$HERE/catalog.asyncapi.json" \
    --language dart \
    --output "$CATALOG" \
    --package catalog_forge_client \
    --base-url http://localhost:8098 \
    --hooks
)

# A page that pastes generated output has to match what was just generated.
python3 "$HERE/check_excerpts.py" "$DOCS_DIR" "$CLIENT" "$CATALOG"

SNIPPETS="$WORK_DIR/snippets"
rm -rf "$SNIPPETS"
mkdir -p "$SNIPPETS/lib"

cat > "$SNIPPETS/pubspec.yaml" <<YAML
name: dart_docs_snippets
description: Every dart block in the Dart client docs, compiled against the real packages.
version: 0.0.0
publish_to: none
environment:
  sdk: ^3.13.0
  flutter: ">=3.47.0"
dependencies:
  flutter:
    sdk: flutter
  flutter_riverpod: ^3.4.3
  path_provider: ^2.1.6
  forge_client: any
  forge_client_flutter: any
  forge_client_riverpod: any
  forge_client_offline: any
  forge_client_grove: any
  grove_crdt: any
  orders_forge_client: any
  catalog_forge_client: any
dependency_overrides:
  forge_client:
    path: "$PACKAGES/forge_client"
  forge_client_flutter:
    path: "$PACKAGES/forge_client_flutter"
  forge_client_riverpod:
    path: "$PACKAGES/forge_client_riverpod"
  forge_client_offline:
    path: "$PACKAGES/forge_client_offline"
  forge_client_grove:
    path: "$PACKAGES/forge_client_grove"
  grove_crdt:
    path: "$GROVE_DIR/crdt-dart"
  orders_forge_client:
    path: "$CLIENT"
  catalog_forge_client:
    path: "$CATALOG"
YAML

# A sample is allowed to declare a local it never reads, or import more than
# it uses: it is a fragment of someone's file, not a file. Everything else
# the analyzer reports, including every warning, fails the check.
cat > "$SNIPPETS/analysis_options.yaml" <<'YAML'
analyzer:
  errors:
    unused_import: ignore
    unused_local_variable: ignore
    unused_element: ignore
    unused_field: ignore
    dead_code: ignore
YAML

python3 "$HERE/extract_snippets.py" "$DOCS_DIR" "$SNIPPETS/lib"

# fvm resolves its SDK from the nearest .fvmrc. A temporary directory has
# none above it, so without this copy fvm would fall back to its global
# default, which is older than the sdk constraint above.
cp "$PACKAGES/.fvmrc" "$SNIPPETS/.fvmrc"

cd "$SNIPPETS"
# PUB_GET_FLAGS is deliberately unquoted: it holds zero or more flags.
# shellcheck disable=SC2086
fvm flutter pub get ${PUB_GET_FLAGS:-}
fvm dart analyze --fatal-warnings lib
echo "check_snippets: every dart block in $DOCS_DIR compiles"
