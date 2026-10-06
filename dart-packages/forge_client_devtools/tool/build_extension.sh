#!/usr/bin/env bash
# Builds the forge DevTools extension into extension/devtools/build and
# validates the result. build_and_copy shells out to `flutter`, so it runs
# under `fvm exec`, which puts the pinned SDK (dart-packages/.fvmrc) first on
# PATH; the machine default is too old.
set -euo pipefail
cd "$(dirname "$0")/.."

fvm flutter pub get
fvm exec dart run devtools_extensions build_and_copy --source=. --dest=extension/devtools
fvm exec dart run devtools_extensions validate --package=.
