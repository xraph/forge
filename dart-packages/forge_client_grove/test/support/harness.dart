import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart' show SseConnect;
import 'package:test/test.dart';

import 'fake_grove_http.dart';
import 'kit.dart';

/// A declaration without a dataset.
const noteSync = SyncDeclaration(
  protocol: 'grove-crdt',
  entity: 'Note',
  table: 'notes',
  pull: '/sync/pull',
  push: '/sync/push',
);

/// foundry-shaped: one table per dataset, omitted from the declaration.
const rowsSync = SyncDeclaration(
  protocol: 'grove-crdt',
  entity: 'Note',
  table: null,
  pull: '/d/{id}/pull',
  push: '/d/{id}/push',
  dataset: '{id}',
);

var _issued = 0;

/// Distinct node ids without the system random source, which `uuid`'s
/// secure generator needs and the Node test runner lacks.
String nextNodeId() => 'dart-test-${_issued++}';

/// The fixed clock of every harness: matches [fakeHeaders]' `date`.
int harnessNow() => DateTime.utc(2026, 10, 4, 12).millisecondsSinceEpoch;

/// A cache with one [GroveSyncSource] over a [FakeGroveHttp].
final class Harness {
  Harness({
    StorageAdapter? storage,
    bool withStorage = true,
    List<SyncDeclaration> declarations = const [noteSync],
    Duration goneRecheck = const Duration(minutes: 5),
    AuthProvider? auth,
    GroveDatasets? datasets,
    LiveChannel live = LiveChannel.poll,
    SseConnect? sseConnect,
    ConnectivitySignal? connectivity,
    Transport? transport,
    FakeGroveHttp? server,
    Map<String, GroveEntity> bindings = const {
      'Note': GroveEntity(codec: NoteCodec()),
    },
    List<SyncSource> before = const [],
  }) : server = server ?? FakeGroveHttp(),
       storage = withStorage ? (storage ?? memoryStorage()) : null {
    source = GroveSyncSource(
      declarations: declarations,
      entities: noteEntities,
      bindings: bindings,
      baseUrl: Uri.parse('http://grove.test'),
      datasets: datasets,
      auth: auth,
      httpClient: this.server.client,
      live: live,
      pollInterval: const Duration(hours: 1),
      pushDebounce: Duration.zero,
      goneRecheck: goneRecheck,
      connectivity: connectivity,
      nowMs: harnessNow,
      newId: () => 'new-id',
      newNodeId: nextNodeId,
      sseConnect: sseConnect,
    );
    cache =
        QueryCache(
            transport: transport ?? NoRest(),
            entities: noteEntities,
            syncSources: [...before, source],
            storage: this.storage,
            onError: (error, context) => errors.add((error, context)),
          )
          ..observer = (e) {
            if (e is SyncStatusChanged) events.add(e);
          };
  }

  final FakeGroveHttp server;
  final StorageAdapter? storage;
  late final GroveSyncSource source;
  late final QueryCache cache;
  final List<SyncStatusChanged> events = [];
  final List<(Object, String)> errors = [];
  var _mutations = 0;

  Future<void> signIn([String principal = 'alice']) async {
    cache.setPrincipal(principal);
    await pumpEventQueue(times: 50);
  }

  Map<String, Object?>? record(String id) =>
      cache.store.getRecord('Note:$id')?.data;

  /// The `Note:` keys the entity store holds.
  List<String> get noteKeys => [
    for (final k in cache.store.keys)
      if (k.startsWith('Note:')) k,
  ];

  /// Hands one mutation to the source, as the cache does once it is running,
  /// and answers as `cache.mutate` would: the response, or the [Rejected]
  /// error thrown. The cache's own path mints uuids from the secure random
  /// source, which the Node test runner lacks; the VM tests also go through
  /// `cache.mutate`.
  Future<Object?> mutate(OperationMeta meta, TagContext args) async {
    final n = _mutations++;
    final outcome = await source.apply(
      PendingMutation(
        id: 'm$n',
        meta: meta,
        args: args,
        optimistic: null,
        idempotencyKey: 'k$n',
        createdAt: DateTime.utc(2026, 10, 4),
      ),
    );

    return switch (outcome) {
      Applied(:final response) => response,
      Queued() => null,
      Rejected(:final error) => throw error,
    };
  }
}

/// A source that stops only when [hold] completes. Registered before the
/// grove source, it keeps the cache from stopping the grove source after a
/// switch, so the previous principal's run stays alive, fenced, for as long
/// as a test needs: the window every privacy rule is about.
final class SlowStop implements SyncSource {
  /// Set to hold the next stop open.
  Completer<void>? hold;

  @override
  Set<String> get entities => const {'Slow'};

  @override
  Future<void> start(SyncContext context) async {}

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      const Applied(null);

  @override
  Stream<SyncStatus> status(String entity) => Stream.value(const Synced());

  @override
  Future<void> stop() async {
    final held = hold;

    if (held != null) await held.future;
  }
}
