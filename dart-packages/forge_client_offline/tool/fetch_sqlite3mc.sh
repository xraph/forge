#!/usr/bin/env bash
# Downloads the sqlite3mc.wasm that matches the resolved sqlite3 package into
# test/, where `flutter test --platform chrome` serves it at /sqlite3mc.wasm,
# and verifies it against tool/sqlite3mc.sha256.
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(awk '/^  sqlite3:$/ {found = 1} found && /version:/ {gsub(/"/, "", $2); print $2; exit}' pubspec.lock)
if [ -z "${version}" ]; then
  echo "sqlite3 is not in pubspec.lock; run fvm flutter pub get first" >&2
  exit 1
fi

expected=$(awk -v v="${version}" '$1 == v {print $2}' tool/sqlite3mc.sha256)
if [ -z "${expected}" ]; then
  echo "no checksum for sqlite3 ${version} in tool/sqlite3mc.sha256." >&2
  echo "Add the sha256 of sqlite3mc.wasm listed on https://github.com/simolus3/sqlite3.dart/releases/tag/sqlite3-${version}" >&2
  exit 1
fi

out=test/sqlite3mc.wasm
curl -fsSL -o "${out}.tmp" "https://github.com/simolus3/sqlite3.dart/releases/download/sqlite3-${version}/sqlite3mc.wasm"
actual=$(shasum -a 256 "${out}.tmp" | awk '{print $1}')
if [ "${actual}" != "${expected}" ]; then
  rm -f "${out}.tmp"
  echo "checksum mismatch for sqlite3mc.wasm ${version}: got ${actual}, want ${expected}" >&2
  exit 1
fi

mv "${out}.tmp" "${out}"
echo "test/sqlite3mc.wasm is sqlite3 ${version} (${actual})"
