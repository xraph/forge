# forge_client_offline

Encrypted on-device storage and a durable outbox for `forge_client`.

Each signed-in principal gets its own SQLite database, encrypted with SQLite3MultipleCiphers under a key from the platform keystore. The cache's snapshot is restored when the principal's session opens and served at once, marked stale, so screens fill before the network answers. Writes the server can't take right now are queued in that database, drawn optimistically, and replayed in order when the device is back online, each with the `Idempotency-Key` it was first sent with.

Web support is pending. The key and storage code for the browser is not in this package yet, so everything below is about the native platforms (iOS, macOS, Android, Linux and Windows), and nothing here says what a browser app gets.

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

The examples below call your generated package `orders_forge_client`. Its `entities` and `operations` tables, `updateOrder` and `UpdateOrderArgs` are the names they use.

## Open it

`OfflineClient.open` builds the transport, the cache and the client in one call, sets the principal, waits for its session and restores the snapshot and the outbox. You don't call `restore()` after it.

```dart
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:orders_forge_client/orders_forge_client.dart';

String? token;
String? tokenOwner;
final heldFailures = <OutboxFailure>[];

Future<OfflineClient> openOffline(
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  token = 'token-for-$userId';
  tokenOwner = userId;

  final offline = await OfflineClient.open(
    transport: RestTransport(
      baseUrl: Uri.parse('https://api.example.com'),
      auth: AuthProvider.callbacks(
        credentials: (_) => token == null ? null : {'Authorization': 'Bearer $token'},
      ),
    ),
    entities: entities,
    operations: operations,
    storage: storage,
    principal: userId,
    connectivity: connectivity,
    authPrincipal: () => tokenOwner,
    onError: (error, context) => debugPrint('offline $context: $error'),
  );

  // Register your own changing listeners after open returns, never before.
  offline.cache.watchPrincipalChanging((next) => heldFailures.clear());
  return offline;
}
```

Build the storage once, with `encryptedStorage(directory: (await getApplicationSupportDirectory()).path)` from `path_provider`, and keep it for the life of the app. Use one storage adapter per directory per isolate, and run the first open on the main isolate. Pass `connectivity` (the `ConnectivityPlusSignal` from `forge_client_flutter` does it) or leave it out and the client assumes the network is up, still queueing a write whose request fails before it reaches the server.

If you build the client yourself instead of calling `open`, pass `storageResets: storage.resets` so a reset is never silent, and call `restore()` once the principal is set:

```dart
Future<OfflineClient> buildByHand(
  Transport inner,
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  final outbox = OutboxTransport(inner);
  final cache = QueryCache(transport: outbox, entities: entities, storage: storage);
  final offline = OfflineClient(
    cache: cache,
    operations: operations,
    connectivity: connectivity,
    transport: outbox,
    storageResets: storage.resets,
  );
  cache.setPrincipal(userId);
  await offline.restore();
  return offline;
}
```

Create the `OfflineClient` before you register any `watchPrincipalChanging` listener of your own. Its listener has to run first so yours never see the previous principal's queue. Those listeners must not read the store or an adapter's providers either.

## Write and watch the queue

```dart
void saveNote(OfflineClient offline, String id, String note) {
  updateOrder(
    offline.cache,
    UpdateOrderArgs(id: id, note: Assign(note)),
    optimistic: OptimisticUpdate((order) => order.copyWith(note: Assign(note))),
  ).ignore();
}
```

A queued write's future stays pending for as long as the write is queued. That can be days offline, or however long the backoff holds it. Draw your screen from the cache, which shows the optimistic value meanwhile, and never put a spinner on that future. If you do await it, expect it to throw the `OutboxFailure` when the server refuses the write.

For status, use the client itself:

```dart
StreamSubscription<OutboxFailure> listenForFailures(OfflineClient offline) {
  heldFailures.addAll(offline.currentFailures);
  debugPrint('${offline.pending.length} writes waiting');

  return offline.failures.listen((failure) {
    heldFailures.add(failure);
    switch (failure) {
      case OutboxConflict():
        askWhichCopyWins(failure);
      case OutboxValidation():
        showInvalid(failure);
      case OutboxUnauthorized():
        signInAgain(failure);
      case OutboxGone():
        failure.discard().ignore();
      case OutboxUncertain():
        askWhetherToResend(failure);
    }
  });
}
```

`failures` is a broadcast stream, so read `currentFailures` for the ones that were already there when your screen opened. `pending` lists every stored write, queued or failed, in replay order. `pendingCount` emits the count after each change, and `pendingCountNow` seeds it. A failure's overlay is rolled back and its record stays. Writes queued behind it for the same entity wait until you call `retry()`, `edit(args)` or `discard()` on it.

`forge_client_flutter` has a `ForgeOutboxListener(source: offline, ...)` that does the listening part for you. The client is its `OutboxFailureSource`.

## Principals and credentials

Call `cache.setPrincipal(next)` before you swap the credentials the transport sends. The outbox suspends the moment the principal starts changing, so no listener sees the old queue. Pass `authPrincipal` (who the credentials belong to right now) as well, and the outbox also refuses to send a write while the two disagree. That covers an app that swaps credentials first by mistake. Without `authPrincipal`, a write that was already due can go out under the next account's credentials.

```dart
Future<void> switchAccount(OfflineClient offline, String userId) async {
  offline.cache.setPrincipal(userId);
  token = 'token-for-$userId';
  tokenOwner = userId;
  await offline.cache.idle;
}
```

Your failure listeners hold `OutboxFailure` objects that carry the previous account's server responses, and acting on one after a switch would reach the wrong outbox. Drop every one on a principal change, as `heldFailures.clear()` does in the first example.

Anonymous requests are not deduplicated by the server's idempotency middleware unless you opt in with `middleware.IdempotencyAllowAnonymous()` on the route's `forge.WithIdempotency(...)`. Without a principal there is nobody to scope the key to, so by default those requests pass straight through.

## Flush, close and sign out

```dart
Future<void> onPaused(OfflineClient offline) => offline.flush();

Future<void> onExit(OfflineClient offline) => offline.close();

Future<void> signOutAndErase(OfflineClient offline) async {
  await offline.signOut();
  token = null;
  tokenOwner = null;
}

Future<void> signOutAndKeep(OfflineClient offline) => offline.signOut(erase: false);
```

`flush()` writes the snapshot now, otherwise it's written a second after the last commit. `close()` flushes and then disposes the client, and the cache with it when `open` built it. Your data stays.

`signOut()` erases by default, because sign-out should crypto-shred: it deletes the principal's key first and the files second, queued writes included. A client built by hand has no storage to erase, so `signOut()` throws a `StateError` for it. Run `cache.setPrincipal(null)`, `await cache.idle` and `storage.destroy(principal)` yourself. `signOut(erase: false)` flushes and keeps everything for the principal's return.

If a destroy is interrupted, call it again. Every step is safe to repeat.

## When storage resets

A database restored without its key (an OS restore onto a new device, say) is deleted unread and started fresh, and the writes queued in it are gone. You hear about it two ways.

```dart
StreamSubscription<StorageReset> reportResets(OfflineClient offline) {
  offline.currentResets.forEach(tellUserAboutReset);
  return offline.resets.listen(tellUserAboutReset);
}
```

Check `currentResets` right after `open`. A reset that happens during open reaches only `onError` and `currentResets`, because nobody can be listening to `resets` yet.

## When the key can't be read

`open` throws `KeyUnavailable` when the keystore can't be read right now. On iOS that is usually transient: the app woke before the first unlock, and the keychain item isn't readable yet. Wait and open again. Nothing is erased.

If it keeps failing while the device is unlocked, the app may offer `resetOfflineData()`. It erases everything this package keeps, for every principal, and any write that never reached the server goes with it. It is never automatic. Call it after the user says yes.

```dart
Future<OfflineClient?> openOrOfferReset(
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  try {
    return await openOffline(storage, connectivity, userId);
  } on KeyUnavailable {
    if (!await userConfirmsErase()) return null;
    await storage.resetOfflineData();
    return openOffline(storage, connectivity, userId);
  }
}
```

A failed `open` has already disposed its client, so the reset goes to the storage directly. On a live client, `offline.resetOfflineData()` signs out first and then does the same.

## What gets retried

The outbox owns retries for every write it tracks. Each attempt carries the same `Idempotency-Key`, so the server's middleware can answer a repeat instead of running it twice.

- 408, 429 and 5xx are retried with a doubling backoff and jitter, up to five minutes.
- A 409 with `Retry-After` is the middleware saying the same key is still in flight, so it is retried too.
- Any other 4xx surfaces as an `OutboxFailure`.
- After 8 consecutive retryable failures the write stops retrying and surfaces as `OutboxUncertain`. The record and its key are kept, and `retry()` resends with the same key.
- A request that never left the device is queued and replayed.

A write that was sent and got no answer is resent on its own only when that's safe: PUT and DELETE, or a route that uses `forge.WithIdempotency()` on the server (generated as `idempotent: true`). Anything else surfaces as `OutboxUncertain` and waits for you, because resending it could apply it twice.

Some smaller facts. A write whose caller cancelled it is never stored. A binary body is stored as bytes and replays as bytes. Credential headers are never written to disk: `Authorization`, `Cookie`, `X-API-Key` and any header whose name contains `token`, `secret` or `auth` are dropped from the record, and the transport supplies them fresh on replay. Two identical writes are two requests, unless you pass a short `duplicateWindow` to the constructor. And the devtools Outbox panel can force a replay out of turn, which can overtake an earlier write in its lane and fail where the normal order would have worked.

Browser apps that talk to the API across origins will need `Idempotency-Key` allowed in the server's CORS configuration once web support lands.

## Keys and their limits

- `keystoreKeys()` is the native default: a random 256-bit key per principal in the Keychain or the Android Keystore, `first_unlock_this_device` on Apple platforms, so the outbox can replay in the background after the first unlock. Pass `requireUserPresence: true` through `encryptedStorage` to gate it behind biometrics or the device passcode. Background replay then waits for the app to be opened.
- `passphraseKey(secret, ...)` derives the key with Argon2id from a secret that is never stored. A wrong passphrase raises `WrongKey` and the data is kept.

Entries live in a dedicated `forge_client_offline` namespace of the keystore, so your own `FlutterSecureStorage` use doesn't collide with them. Linux and Windows have no namespace option in `flutter_secure_storage`, so there an app-wide `FlutterSecureStorage().deleteAll()` wipes this package's salt and keys. Don't call it on those platforms.

Opening storage with a passphrase takes a few more lines, because the salt sits beside the database and the file names come from a shared `PrincipalLabels`:

```dart
EncryptedSqliteStorage passphraseStorage(String directory) {
  final labels = PrincipalLabels(
    FlutterSecretStore(
      secureStorageFor(requireUserPresence: false, useDataProtectionKeychain: true),
    ),
  );
  return encryptedSqliteStorage(
    keys: passphraseKey(
      askForPassphrase,
      salts: platformPassphraseSaltStore(directory: directory),
      labels: labels,
    ),
    labels: labels,
    directory: directory,
  );
}
```

The passphrase is a weaker crypto-shred than a keystore key. The salt is a plain file next to the database. A copy of the database, plus that salt, plus the passphrase still decrypts after a destroy. Signing out removes the salt and the database, but it can't reach a copy somebody already made.

A power loss right after the first open can lose the salt while keeping the database. That fails closed: the database can't be opened until you `destroy` the principal or call `resetOfflineData`.

The Argon2id defaults are 64 MiB, 3 passes and 1 lane, adapted from RFC 9106's second recommendation. On an Apple M3 Max the derivation takes about 450 ms, measured in pure Dart both under the VM and as an AOT executable. That is a laptop, and a phone will be slower. Nobody has timed it on one yet, and the iOS simulator would only repeat the host's number. Parameters below 19 MiB or 2 passes are refused. The derivation runs whenever a principal's database is opened fresh, so every cold start pays it.

### Android Auto Backup

Exclude two things from Auto Backup: the secure-storage preferences, and the package's database directory, `forge_client_offline/` under the app support directory. A restore that brings the preferences back without the Keystore key that decrypts them leaves the install salt and every key unreadable, and `open` throws `KeyUnavailable` for good. `resetOfflineData()` is the fix, run on the user's say-so.

That reset path, clearing a namespace whose Keystore key is gone, is unverified. It has not been tested on Android yet. Treat it as untested until a run says otherwise.

## Testing in this package

The package's own tests fake the keystore, so none of them reach a real one. The `example/` app does. It is a small macOS app whose integration test uses the real login Keychain and the sqlite3mc build linked into a real app:

    cd example
    flutter test integration_test/keystore_test.dart -d macos

It checks that a key is created once and forgotten on delete, and that a database written through the Keychain reads back and is gone after a destroy. It runs against the login keychain (`useDataProtectionKeychain: false`) because the data protection keychain needs a signing team and the `keychain-access-groups` entitlement.

The web tests need `sqlite3mc.wasm` next to them. `tool/fetch_sqlite3mc.sh` downloads the build that matches the resolved `sqlite3` version and checks it against `tool/sqlite3mc.sha256`. The file is gitignored.
