import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
);
const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  invalidates: ['Order:{id}'],
);
const opReplaceOrder = OperationMeta(
  id: 'op_replace_order',
  method: 'PUT',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  invalidates: ['Order:{id}'],
);
const opDeleteOrder = OperationMeta(
  id: 'op_delete_order',
  method: 'DELETE',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Order[]'],
);
const opCreateOrder = OperationMeta(
  id: 'op_create_order',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  rootType: 'Order',
  invalidates: ['Order[]'],
);
const opCreateOrderIdempotent = OperationMeta(
  id: 'op_create_order_idempotent',
  method: 'POST',
  path: '/orders/idempotent',
  entity: 'Order',
  rootType: 'Order',
  invalidates: ['Order[]'],
  idempotent: true,
);
const opUpdateCustomer = OperationMeta(
  id: 'op_update_customer',
  method: 'PATCH',
  path: '/customers/{id}',
  entity: 'Customer',
  rootType: 'Customer',
  invalidates: ['Customer:{id}'],
);

/// A write that declares no tags: nothing refetches after it unless the
/// outbox invalidates its entity itself.
const opTouchOrder = OperationMeta(
  id: 'op_touch_order',
  method: 'PATCH',
  path: '/orders/{id}/touch',
  entity: 'Order',
  rootType: 'Order',
);
const opTagOrderForm = OperationMeta(
  id: 'op_tag_order_form',
  method: 'POST',
  path: '/orders/{id}/tags',
  entity: 'Tag',
  requestContentType: 'application/x-www-form-urlencoded',
);
const opUploadScan = OperationMeta(
  id: 'op_upload_scan',
  method: 'PUT',
  path: '/orders/{id}/scan',
  entity: 'Scan',
  requestContentType: 'application/octet-stream',
);

const Map<String, OperationMeta> operations = {
  'op_get_order': opGetOrder,
  'op_update_order': opUpdateOrder,
  'op_replace_order': opReplaceOrder,
  'op_delete_order': opDeleteOrder,
  'op_create_order': opCreateOrder,
  'op_create_order_idempotent': opCreateOrderIdempotent,
  'op_update_customer': opUpdateCustomer,
  'op_upload_scan': opUploadScan,
  'op_touch_order': opTouchOrder,
  'op_tag_order_form': opTagOrderForm,
};

const EntitySchema entities = {
  'Order': EntityMeta(idField: 'id'),
  'Customer': EntityMeta(idField: 'id'),
};

TagContext orderArgs(String id, [Map<String, Object?>? body]) =>
    TagContext(path: {'id': id}, body: body);

/// The `note` a write carries. Maps compare by identity, so tests match on this.
Object? noteOf(TransportRequest request) =>
    (request.args.body as Map<String, Object?>?)?['note'];

/// The server's default answer: echo the body over a stored record.
Future<Object?> echo(TransportRequest request) async {
  final id = request.args.path['id'];
  final body = request.args.body;
  return switch (request.meta.method) {
    'GET' => <String, Object?>{'id': id, 'total': 10, 'note': null},
    'DELETE' => null,
    'POST' => <String, Object?>{
      'id': 'created',
      'total': 10,
      'note': null,
      if (body is Map<String, Object?>) ...body,
    },
    _ => <String, Object?>{
      'id': id,
      'total': 10,
      'note': null,
      if (body is Map<String, Object?>) ...body,
    },
  };
}

final class FakeTransport implements Transport {
  final List<TransportRequest> requests = [];

  /// Who the credentials belonged to when each request in [requests] went
  /// out, read from [credentials].
  final List<String?> sentAs = [];

  /// The principal the fake's credentials belong to right now.
  String? credentials;

  Future<Object?> Function(TransportRequest request) respond = echo;

  List<TransportRequest> get writes =>
      requests.where((r) => r.meta.method != 'GET').toList();

  /// [sentAs] for the writes only, in the order of [writes].
  List<String?> get writesSentAs => [
    for (final (i, r) in requests.indexed)
      if (r.meta.method != 'GET') sentAs[i],
  ];

  @override
  Future<Object?> execute(TransportRequest request) {
    requests.add(request);
    sentAs.add(credentials);
    return respond(request);
  }
}

final class FakeConnectivity implements ConnectivitySignal {
  final StreamController<bool> _controller = StreamController<bool>.broadcast(
    sync: true,
  );

  @override
  Stream<bool> get online => _controller.stream;

  void set(bool value) => _controller.add(value);
}

/// Records how a future ended without awaiting it.
final class Watched<T> {
  Watched(Future<T> future) {
    future.then(
      (v) {
        value = v;
        done = true;
      },
      onError: (Object e) {
        error = e;
        done = true;
      },
    );
  }

  bool done = false;
  T? value;
  Object? error;
}

/// Lets microtasks and zero-length timers run.
Future<void> settle() async {
  for (var i = 0; i < 25; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

/// Switches the cache's principal and waits for its session to follow.
Future<void> switchTo(QueryCache cache, String? principal) async {
  final changed = cache.sessionChanges.firstWhere(
    (s) => s?.principal == principal,
  );
  cache.setPrincipal(principal);
  await changed.timeout(const Duration(seconds: 2));
}

Future<List<PendingMutationRecord>> storedOutbox(
  StorageAdapter storage,
  String principal,
) async {
  final session = await storage.open(principal);
  final records = await session.readOutbox();
  await session.close();
  return records;
}

Future<void> seedOutbox(
  StorageAdapter storage,
  String principal,
  List<OutboxEntry> entries,
) async {
  final session = await storage.open(principal);
  for (final entry in entries) {
    await session.enqueue(entry.toRecord());
  }
  await session.close();
}

OutboxEntry seededEntry({
  required String id,
  required OperationMeta meta,
  required int seq,
  TagContext args = const TagContext(
    path: {'id': '7'},
    body: {'note': 'seeded'},
  ),
  DateTime? createdAt,
  DateTime? sentAt,
}) => OutboxEntry(
  id: id,
  operationId: meta.id,
  args: args,
  requestHeaders: const {},
  intent: const NoOverlay(),
  idempotencyKey: 'key-$id',
  createdAt: createdAt ?? DateTime.utc(2026, 10, 4, 12),
  seq: seq,
  sentAt: sentAt,
);

/// Wraps a [StorageAdapter] so a test can keep a session usable after the
/// cache closes it, or run code while a state write is in progress.
final class ScriptedStorage implements StorageAdapter {
  ScriptedStorage(this.inner);

  final StorageAdapter inner;

  /// Ignore `close`, so a previous principal's session stays writable.
  bool keepOpen = false;

  /// Runs before each `updateState` reaches [inner].
  FutureOr<void> Function(String mutationId, String stateJson)?
  beforeUpdateState;

  /// Runs before each `readOutbox` reaches [inner], with the principal.
  FutureOr<void> Function(String principal)? beforeReadOutbox;

  @override
  Future<StorageSession> open(String principal) async =>
      _ScriptedSession(this, await inner.open(principal));

  @override
  Future<void> destroy(String principal) => inner.destroy(principal);
}

final class _ScriptedSession implements StorageSession {
  _ScriptedSession(this._storage, this._inner);

  final ScriptedStorage _storage;
  final StorageSession _inner;

  @override
  String get principal => _inner.principal;

  @override
  Future<Snapshot?> readSnapshot() => _inner.readSnapshot();

  @override
  Future<void> writeSnapshot(Snapshot snapshot) =>
      _inner.writeSnapshot(snapshot);

  @override
  Future<List<PendingMutationRecord>> readOutbox() async {
    await _storage.beforeReadOutbox?.call(_inner.principal);
    return _inner.readOutbox();
  }

  @override
  Future<void> enqueue(PendingMutationRecord record) => _inner.enqueue(record);

  @override
  Future<void> remove(String mutationId) => _inner.remove(mutationId);

  @override
  Future<void> updateState(String mutationId, String stateJson) async {
    await _storage.beforeUpdateState?.call(mutationId, stateJson);
    await _inner.updateState(mutationId, stateJson);
  }

  @override
  KeyValueStore namespace(String name) => _inner.namespace(name);

  @override
  Future<void> close() async {
    if (!_storage.keepOpen) await _inner.close();
  }
}

/// A jitter source with no jitter: backoff delays are exact.
double _midpoint() => 0.5;

/// A sync source that owns [entities] and records what it is asked to apply.
final class FakeSource implements SyncSource {
  FakeSource(this.entities);

  @override
  final Set<String> entities;

  final List<PendingMutation> applied = [];

  @override
  Future<void> start(SyncContext context) async {}

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async {
    applied.add(mutation);
    return const Applied(null);
  }

  @override
  Stream<SyncStatus> status(String entity) =>
      Stream<SyncStatus>.value(const Synced());

  @override
  Future<void> stop() async {}
}

final class Harness {
  Harness._({
    required this.network,
    required this.cache,
    required this.connectivity,
    required this.storage,
    required this.offline,
    required this.events,
    required this.clock,
    required this.errors,
  });

  final FakeTransport network;
  final QueryCache cache;
  final FakeConnectivity connectivity;
  final StorageAdapter storage;
  final OfflineClient offline;
  final List<CacheEvent> events;
  final ManualClock clock;

  /// What the client reported through `onError`, as (context, error).
  final List<(String, Object)> errors;

  static Future<Harness> create({
    bool online = false,
    StorageAdapter? storage,
    String principal = 'alice',
    Duration? duplicateWindow,
    Set<String> excludedEntities = const {},
    bool wireAuthPrincipal = false,
    OverlayIntentFor overlayIntent = deriveOverlayIntent,
    List<SyncSource> syncSources = const [],
    double Function() random = _midpoint,
    ManualClock? clock,
    Duration idempotencyWindow = const Duration(hours: 24),
  }) async {
    final network = FakeTransport()..credentials = principal;
    final outbox = OutboxTransport(network);
    clock ??= ManualClock();
    final store = storage ?? memoryStorage();
    final cache = QueryCache(
      transport: outbox,
      entities: entities,
      clock: clock,
      storage: store,
      syncSources: syncSources,
    );
    final events = <CacheEvent>[];
    cache.observer = events.add;
    final connectivity = FakeConnectivity();
    final errors = <(String, Object)>[];

    String? authPrincipal() => network.credentials;
    void onError(Object error, String context) => errors.add((context, error));
    // Without a duplicateWindow the client's own default applies.
    final offline = duplicateWindow == null
        ? OfflineClient(
            cache: cache,
            operations: operations,
            connectivity: connectivity,
            transport: outbox,
            clock: clock,
            initiallyOnline: online,
            excludedEntities: excludedEntities,
            overlayIntent: overlayIntent,
            authPrincipal: wireAuthPrincipal ? authPrincipal : null,
            onError: onError,
            random: random,
            idempotencyWindow: idempotencyWindow,
          )
        : OfflineClient(
            cache: cache,
            operations: operations,
            connectivity: connectivity,
            transport: outbox,
            clock: clock,
            initiallyOnline: online,
            duplicateWindow: duplicateWindow,
            excludedEntities: excludedEntities,
            overlayIntent: overlayIntent,
            authPrincipal: wireAuthPrincipal ? authPrincipal : null,
            onError: onError,
            random: random,
            idempotencyWindow: idempotencyWindow,
          );

    await switchTo(cache, principal);
    await offline.restore();

    return Harness._(
      network: network,
      cache: cache,
      connectivity: connectivity,
      storage: store,
      offline: offline,
      events: events,
      clock: clock,
      errors: errors,
    );
  }

  /// [create] inside fakeAsync.
  static Harness createIn(
    FakeAsync async, {
    bool online = false,
    StorageAdapter? storage,
    bool wireAuthPrincipal = false,
    double Function() random = _midpoint,
  }) {
    Harness? created;
    unawaited(
      Harness.create(
        online: online,
        storage: storage,
        wireAuthPrincipal: wireAuthPrincipal,
        random: random,
      ).then((h) => created = h),
    );
    async.flushMicrotasks();
    return created!;
  }

  Future<void> seed(String id) async {
    await cache.fetch(opGetOrder, orderArgs(id));
  }

  Map<String, Object?>? order(String id) =>
      cache.getState(opGetOrder, orderArgs(id)).dataOrNull
          as Map<String, Object?>?;

  Future<List<PendingMutationRecord>> stored([String principal = 'alice']) =>
      storedOutbox(storage, principal);

  List<TransportRequest> get writes => network.writes;

  Future<Object?> update(String id, Map<String, Object?> body) => cache.mutate(
    opUpdateOrder,
    orderArgs(id, body),
    options: MutateOptions(
      optimistic: OptimisticUpdate<Object?>(
        (p) => p is Map<String, Object?> ? <String, Object?>{...p, ...body} : p,
        key: 'Order:$id',
      ),
    ),
  );

  Future<Object?> write(
    OperationMeta meta,
    TagContext args, {
    Map<String, String> headers = const {},
  }) => cache.mutate(meta, args, options: MutateOptions(headers: headers));

  void goOnline() => connectivity.set(true);
}
