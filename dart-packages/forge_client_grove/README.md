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
background.

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

Right after sign-in the replicas load in the background. A `join` or `leave`
then still works: `leave` drops the dataset from the load, and
`leave(erase: true)` waits for the load before it erases.

## Switching accounts

When you call `setPrincipal(next)`, the cache fences the old account's run on
the spot, before it empties the store. From then on:

- none of its replica rows or status reach the store, the watchers or the
  observer;
- its requests can't pick up the next account's credentials (the auth adapter
  refuses to read or refresh them for a fenced run);
- `records`, `rejected`, `replica`, `statusOf`, `joined` and
  `describeForDevtools` answer as if nothing were joined.

The old run then stops: the engine first, abandoning any request in flight,
then the transports, then the replica, which flushes before the session
closes.

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

grove_crdt is a git dependency on github.com/xraph/grove (`crdt-dart`). Next to
a grove checkout, keep a `pubspec_overrides.yaml` (git-ignored) that points
`grove_crdt` at `../../../forgery/grove-crdt-dart/crdt-dart`.

The OfflineClient test imports forge_client_offline's
`src/offline_client.dart` directly. The package barrel exports the Flutter key
stores, which plain `dart test` can't load.
