# forge_client_offline

Encrypted on-device storage and a durable outbox for `forge_client`.

Each signed-in principal gets its own SQLite database, encrypted with SQLite3MultipleCiphers under a key held in the platform keystore. Writes made offline are queued in that database and replayed in order when the device is back online, each with an `Idempotency-Key`.

## Install

Everything in `dart-packages/` is `1.0.0-dev` and `publish_to: none`, so there is no version to depend on yet. Until publishing lands, depend on the packages by path:

```yaml
dependencies:
  forge_client:
    path: ../forge_client
  forge_client_offline:
    path: ../forge_client_offline
```

or straight from the repository:

```yaml
dependencies:
  forge_client:
    git:
      url: https://github.com/xraph/forge
      path: dart-packages/forge_client
      ref: <commit sha>
  forge_client_offline:
    git:
      url: https://github.com/xraph/forge
      path: dart-packages/forge_client_offline
      ref: <commit sha>
```

Put the same full commit sha in both `ref:` lines. Pub turns this package's own `path: ../forge_client` into a git dependency pinned to the commit it resolved, so your direct `forge_client` entry has to name that exact commit too.

You also need to select the encrypted SQLite build in your app's `pubspec.yaml`. The `hooks` block is required, and it has to be in the app's own pubspec because user defines are read from the root package only. Without it the app links plain SQLite, and opening storage fails with `EncryptionUnavailable` rather than writing anything unencrypted.

```yaml
hooks:
  user_defines:
    sqlite3:
      source: sqlite3mc
```

## Testing in this package

The web tests need `sqlite3mc.wasm` next to them. `tool/fetch_sqlite3mc.sh` downloads the build that matches the resolved `sqlite3` version and checks it against `tool/sqlite3mc.sha256`. The file is gitignored.
