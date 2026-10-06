# forge_client_grove

Full sync for Grove-backed entities. It implements forge_client's
`SyncSource`: records of the entities in your generated `sync` table live in
a Grove CRDT replica inside the principal's storage, are projected into the
entity store, and sync with the server over pull, push and the change stream.

```dart
final cache = configureClient(
  transport: RestTransport(baseUrl: api),
  entities: entities,
  storage: offlineStorage,
  syncSources: [
    GroveSyncSource(
      declarations: sync,
      entities: entities,
      bindings: {'Note': GroveEntity(codec: noteCodec)},
      baseUrl: api,
    ),
  ],
);
cache.setPrincipal(userId);
```

Datasets created at runtime (TwinOS collaborative datasets, one Grove table
`ds_<name>` each) are joined and left on the one source; their rows share the
app's entity store under composite ids `<dataset>:<pk>`:

```dart
await grove.join(GroveDataset(datasetId, table: 'ds_$datasetName'));
grove.statusOf(datasetId).listen(showBadge);
await grove.leave(datasetId, erase: true);
```

grove_crdt is a git dependency on github.com/xraph/grove (`crdt-dart`). For
local development next to a grove checkout, keep a `pubspec_overrides.yaml`
(git-ignored) that points `grove_crdt` at `../../../forgery/grove-crdt-dart/crdt-dart`.
