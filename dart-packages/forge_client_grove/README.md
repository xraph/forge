# forge_client_grove

Full sync for Grove-backed entities. `GroveSyncSource` implements
forge_client's `SyncSource`: records of the entities in your generated `sync`
table live in a Grove CRDT replica inside the principal's storage. They're
projected into the entity store and synced with the server over pull, push and
the change stream, so they keep working offline.

## Wiring it up

Most apps want the replica on disk, next to the snapshot and the outbox, so
hand the source to `OfflineClient.open` from forge_client_offline:

```dart
final grove = GroveSyncSource(
  declarations: sync,
  entities: entities,
  bindings: {'Note': GroveEntity(codec: noteCodec)},
  baseUrl: api,
  auth: auth,
);

final client = await OfflineClient.open(
  transport: RestTransport(baseUrl: api, auth: auth),
  entities: entities,
  operations: operations,
  storage: encryptedStorage(),
  principal: userId,
  syncSources: [grove],
);
```

OfflineClient keeps grove's entities out of its outbox and out of what it
restores, because the source owns them. Without forge_client_offline, pass the
source to `configureClient` (or straight to `QueryCache`). Leave `storage` out
and the replica lives in memory for the session:

```dart
final cache = configureClient(
  transport: RestTransport(baseUrl: api),
  entities: entities,
  storage: offlineStorage,
  syncSources: [grove],
);
cache.setPrincipal(userId);
```

Mutations on an owned entity go through `cache.mutate` as usual. The source
writes them to the replica, shows them at once and pushes them in the
background. Typed CRDT ops you make through `grove.replica(entity)` (counters,
text) are pushed the same way. A push that fails on a network error or a 5xx is
retried with a jittered backoff capped at five minutes (or `pollInterval`, when
that's shorter), whichever live channel you use.

### Depending on grove_crdt

You'll import `grove_crdt` yourself for `CrdtType` (in `GroveEntity.types`),
for `camelDtoEnvelope` and for the typed ops on `replica()`. The barrel doesn't
re-export it, so add it next to this package, at the same pin:

```yaml
dependencies:
  forge_client_grove:
    path: ../forge_client_grove
  grove_crdt:
    git:
      url: https://github.com/xraph/grove.git
      path: crdt-dart
      ref: 11453d954cbb830703813e4a8ec858ad80862220
```

Keep the `ref` equal to the one in this package's `pubspec.yaml`. For local
work see [Local development](#local-development).

### The pending bound

Each dataset's replica holds at most 10,000 unsynced changes. A mutation that
would go past that is refused whole: `cache.mutate` throws a `CrdtError` with
code `offlineQueueFull`, nothing of it is written, and the error also goes to
the cache's `onError` with the context `grove`. Edits already queued are never
dropped to make room. A typed op on `replica()` past the bound throws the same
error at the call.

### Making sure an edit is on disk

The replica writes an acknowledged edit to storage about 50 ms after
`cache.mutate` returns. Stopping the source or disposing the cache flushes it,
but a process the OS kills inside that window loses it. If you need to know an
edit is on disk, for instance when the app goes to the background, await
`flush()`:

```dart
case AppLifecycleState.paused:
  await grove.flush();
```

It flushes every running dataset of the signed-in principal and throws the
first `ReplicaPersistFailed`, if any. What failed stays queued and is retried.

## Datasets

TwinOS creates collaborative datasets at runtime, one Grove table `ds_<name>`
each. You join and leave them on the one source, and their rows share the
app's entity store under composite ids `<dataset>:<pk>`:

```dart
await grove.join(GroveDataset(datasetId, table: 'ds_$datasetName'));
grove.statusOf(datasetId).listen(showBadge);
await grove.leave(datasetId);              // keeps the replica for a rejoin
await grove.leave(datasetId, erase: true); // deletes it too
```

foundry speaks a camel-cased DTO dialect, not Grove's native wire format, so a
TwinOS app passes `envelope: camelDtoEnvelope` (from `grove_crdt`) when it
builds the source:

```dart
final grove = GroveSyncSource(
  declarations: sync,
  entities: entities,
  bindings: {'DatasetRow': GroveEntity(codec: datasetRowCodec)},
  baseUrl: api,
  auth: auth,
  envelope: camelDtoEnvelope,
);
```

With the default envelope foundry gets Grove's native framing and can't read
it.

Pass `datasets:` to the constructor when you want some joined at every start.
It's called in the background after the replicas loaded, so it can hit the
network. `start` itself returns at once: opening and hydrating the replicas
happens in the background too, and a write made meanwhile waits for it.

Joined datasets belong to the principal. Switching accounts forgets them, and
you join again for the next one. A join belongs to whoever the cache is signed
in as when you call it (`cache.principal`), and only that principal's start
picks it up. So a `join` from the new account's UI while the old account is
still stopping waits for the new account and runs as the new account; the old
account never gets it. A `leave` acts for the same principal and only ever
drops that principal's waiting joins. With nobody signed in, `join` and
`leave(erase: true)` throw a `StateError`.

The very first sign-in is a special case. The cache hands itself to the source
only when it starts it, after the principal's storage opened, so until then
the source can't tell a signed-in user from nobody, and a `join` right after
the first `setPrincipal` throws. Attach the source to its cache once, right
after you build the cache, and that join waits for the principal's start like
any other:

```dart
final cache = configureClient(/* ... */ syncSources: [grove]);
grove.attach(cache);

cache.setPrincipal(userId);
await grove.join(GroveDataset(datasetId)); // queued for userId
```

If you'd rather not, `await cache.idle` after the first `setPrincipal` does
the same job. A join made for one principal is still never handed to whoever
signs in after them.

Right after sign-in the replicas load in the background. A `join` or `leave`
then still works: `leave` drops the dataset from the load, and
`leave(erase: true)` waits for the load before it erases. A `join` of a
dataset whose erase is still running waits for it, then starts the dataset
fresh over the erased replica.

## Switching accounts

When you call `setPrincipal(next)`, the cache fences the old account's run on
the spot, before it empties the store. From then on:

- none of its replica rows or status reach the store, the watchers or the
  observer;
- its requests can't pick up the next account's credentials (the auth adapter
  refuses to read or refresh them for a fenced run);
- its push debounce, retry backoff and gone probe are cancelled, and none of
  them would act for a fenced run anyway;
- `records`, `rejected`, `replica`, `statusOf`, `joined` and
  `describeForDevtools` answer as if nothing were joined.

The old run then stops: the engine first, abandoning any request in flight,
then the transports, then the replica, which flushes before the session
closes.

The credential fence only covers credentials the source reads through the
`auth` you pass it (forge's `AuthProvider`). Don't bake credentials into a
custom `httpClient` (a cookie jar, an interceptor that adds a token). A request
the old run still has in flight between the fence and its stop reads nothing
through `auth`, so it carries whatever that client holds by then, which may be
the next account's.

## After leave with erase

`leave(id, erase: true)` cancels the dataset's run and disposes its replica
before it deletes the namespace, so a response still in flight can't write it
back. Two things it does not reach:

- If your app persists a denormalized snapshot of its own (one that embeds
  records), rewrite it after `leave(erase: true)`. OfflineClient writes
  normalized snapshots, which never hold grove rows, so its snapshot is clean
  on the next write.
- Query skeletons can still name composite ids, the dataset id and the pk
  (never field values), until those queries leave the cache.

## Status and devtools

`status(entity)` folds an entity across every dataset carrying it, and
`statusOf(id)` folds one dataset across its entities. A gone dataset (404 or
410) reports `SyncFailed(GroveDatasetGone)` and is probed again every
`goneRecheck`; refused changes report `SyncFailed(GroveChangeRejected)`, and
you clear them with `retryRejected` or `discardRejected`. A replica that can't
be read from storage fails its dataset with `SyncFailed(ReplicaUnavailable)`
and never syncs, so the edits it holds are never overwritten.

`describeForDevtools()` gives the Sync panel its node id, clock, per-entity and
per-dataset state and the peers that edited the replica.

## Local development

grove_crdt is a git dependency on github.com/xraph/grove (`crdt-dart`), pinned
to a commit. Next to a grove checkout, keep a `pubspec_overrides.yaml`
(git-ignored) that points `grove_crdt` at the worktree, so you pick up
grove_crdt changes without a push:

```yaml
dependency_overrides:
  grove_crdt:
    path: ../../../forgery/grove-crdt-dart/crdt-dart
```

An app that depends on `grove_crdt` directly needs the same override, or the
two constraints won't resolve to one package.

The OfflineClient test imports forge_client_offline's
`src/offline_client.dart` directly. The package barrel exports the Flutter key
stores, which plain `dart test` can't load.
