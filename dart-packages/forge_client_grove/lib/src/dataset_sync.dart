import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:http/http.dart' as http;

import 'config.dart';
import 'field_writer.dart';
import 'projection.dart';
import 'status.dart';

/// Whether [error] is a deliberate cancellation, which an account switch or a
/// stop causes on purpose and which is never a failure.
bool isCancellation(Object error) =>
    error is CrdtError && error.code == CrdtErrorCode.cancelled;

/// Whether a live channel failed with a status that a pull classifies as a
/// terminal state: 404 or 410 (gone), 401 or 403 (unauthorized).
bool _refusesStream(Object error) =>
    error is CrdtError && const {401, 403, 404, 410}.contains(error.statusCode);

/// One dataset behind one set of sync endpoints, for one principal: replica,
/// client, engine and live channel.
///
/// Everything it puts in the entity store goes through [context]'s
/// [SyncContext.write], so nothing lands once the cache has moved to another
/// principal. Every callback that would project, report or sync checks
/// [SyncContext.active] first. Every timer (the push debounce, the retry
/// backoff, the gone probe, a delayed stream reopen) checks it again when it
/// fires, and all of them are cancelled synchronously when the cache starts
/// moving to another principal, and again by [stop].
final class DatasetSync {
  /// Wires a dataset. [entityTables] maps each entity to its table, or null
  /// while unknown.
  DatasetSync({
    required this.context,
    required this.datasetId,
    required this.replicaKey,
    required this.endpoints,
    required Map<String, String?> entityTables,
    required this.bindings,
    required this.projectors,
    required KeyValueReplicaStorage storage,
    required String nodeId,
    required Uri baseUrl,
    required SyncEnvelope envelope,
    required this.live,
    required this.pollInterval,
    required this.pushDebounce,
    required this.goneRecheck,
    required this.onStatusChange,
    http.Client? httpClient,
    CrdtAuthProvider? auth,
    Future<bool> Function()? onUnauthorized,
    SseConnect? sseConnect,
    int Function()? nowMs,
  }) : _tables = {...entityTables},
       _baseUrl = baseUrl,
       _auth = auth {
    final now = nowMs ?? () => DateTime.now().millisecondsSinceEpoch;
    final skew = ClockSkew(systemNowMs: now);
    final clock = HybridClock(nodeId, nowMs: now);

    _transport = HttpStreamTransport(
      baseUrl: baseUrl,
      client: httpClient,
      auth: auth,
      envelope: envelope,
      pullPath: endpoints.pull,
      pushPath: endpoints.push,
      streamPath: endpoints.stream ?? '/stream',
      onServerTime: skew.observe,
      onUnauthorized: onUnauthorized,
      sseConnect: sseConnect,
    );
    store = CrdtStore(
      nodeId,
      clock,
      storage: storage,
      onError: (error, _) => _report(error),
      onStorageError: _report,
      onPluginError: (error, _) => _report(error),
    );
    client = CrdtClient(nodeId: nodeId, clock: clock, transport: _transport);
    engine = SyncEngine(
      client,
      store,
      tables: {for (final t in _tables.values) t ?? '*'}.toList(),
      cursors: storage,
      skew: skew,
      baseNodeId: nodeId,
    );

    final cap = pollInterval < _retryCap ? pollInterval : _retryCap;

    _retry = Backoff(
      initialDelay: cap < _retryStart ? cap : _retryStart,
      maxDelay: cap,
    );
    _streamBackoff = Backoff(
      initialDelay: cap < _retryStart ? cap : _retryStart,
      maxDelay: cap,
    );
    // Synchronously, as the cache starts moving to another principal: the
    // context is fenced by then, so a timer firing later would do nothing, but
    // none is left armed at all.
    _unwatchPrincipal = context.cache.watchPrincipalChanging(
      (_) => _cancelTimers(),
    );
  }

  /// The first retry delay after an interrupted run.
  static const _retryStart = Duration(seconds: 1);

  /// The longest retry delay, unless [pollInterval] is shorter.
  static const _retryCap = Duration(minutes: 5);

  /// The principal's context: the only way into the entity store.
  final SyncContext context;

  /// The dataset id (`''` for a declaration without a dataset).
  final String datasetId;

  /// This dataset's replica namespace key.
  final String replicaKey;

  /// Resolved paths.
  final GroveEndpoints endpoints;

  /// Entity mappings.
  final Map<String, GroveEntity> bindings;

  /// Entity projectors (built with this dataset's id).
  final Map<String, Projector> projectors;

  /// Live channel preference.
  final LiveChannel live;

  /// Polling interval when polling.
  final Duration pollInterval;

  /// Delay between a local write and its push.
  final Duration pushDebounce;

  /// Minimum time between probes of a gone dataset.
  final Duration goneRecheck;

  /// Called whenever a status input may have changed.
  final void Function() onStatusChange;

  final Map<String, String?> _tables;
  final Uri _baseUrl;
  final CrdtAuthProvider? _auth;
  late final HttpStreamTransport _transport;
  late final Backoff _retry;
  late final Backoff _streamBackoff;
  late final void Function() _unwatchPrincipal;

  /// The replica.
  late final CrdtStore store;

  /// The protocol client.
  late final CrdtClient client;

  /// The sync engine.
  late final SyncEngine engine;

  final List<StreamSubscription<Object?>> _subs = [];
  final Completer<void> _hydrated = Completer<void>();
  Timer? _pushTimer;
  Timer? _goneTimer;
  Timer? _retryTimer;
  Timer? _reopenTimer;

  /// The pending queue's length as last seen; a local write grows it.
  int _pendingSeen = 0;

  /// Whether the live channel was last closed because it was refused with a
  /// terminal status; it reopens after a backoff delay, not at once.
  bool _streamRefused = false;
  Future<void> Function()? _stopPoll;
  CrdtSubscription? _live;
  final List<void Function()> _liveHandlers = [];
  WebSocketTransport? _ws;
  bool _online = true;
  bool _ready = false;
  Future<void>? _stopping;
  Object? _unavailable;
  String _goneMessage = '';

  /// Completes once [start] has hydrated and projected the replica, or found
  /// it unavailable, or found the context inactive. Never fails.
  Future<void> get hydrated => _hydrated.future;

  /// Whether [start] finished hydrating and the dataset takes writes.
  bool get ready => _ready;

  /// Whether [stop] was called.
  bool get stopped => _stopping != null;

  /// The [ReplicaUnavailable] the replica failed to load with, when it did.
  Object? get unavailable => _unavailable;

  bool get _running => _stopping == null && context.active;

  void _markHydrated() {
    if (!_hydrated.isCompleted) _hydrated.complete();
  }

  void _report(Object error) {
    if (context.active) context.cache.report(error, 'grove');
  }

  /// The entity's table, or null while unknown.
  String? tableOf(String entity) => _tables[entity];

  /// Adopts a table supplied later (a second `join` with a table).
  void adoptTable(String entity, String table) => _tables[entity] ??= table;

  /// Entities this dataset carries.
  Iterable<String> get entities => _tables.keys;

  /// Hydrates and projects the replica, then syncs in the background.
  ///
  /// A replica that cannot be read ([ReplicaUnavailable]) marks the dataset
  /// failed, `SyncFailed(ReplicaUnavailable)`, and its engine never runs. The
  /// cause is reported once, by the store. Nothing is projected or
  /// started when the context went inactive, or [stop] was called, while the
  /// replica loaded.
  Future<void> start() async {
    try {
      await store.ready;
    } on ReplicaUnavailable catch (error) {
      // The store reported the cause through onStorageError just before it
      // threw; the status carries the ReplicaUnavailable itself.
      _unavailable = error;
      _markHydrated();

      if (context.active) onStatusChange();

      return;
    }

    if (!_running) {
      _markHydrated();

      return;
    }

    _learn(store.tables);

    for (final table in store.tables) {
      final entity = _entityOf(table);

      if (entity != null) _project(entity, store.exportTable(table).values);
    }

    _pendingSeen = store.pending.length;
    _subs
      ..add(store.documentChanges.listen(_onDoc))
      ..add(engine.events.listen(_onEngine));
    _ready = true;
    _markHydrated();
    unawaited(syncNow());
  }

  void _project(String entity, Iterable<DocumentState> docs) {
    try {
      projectors[entity]!.project(context, docs);
    } on Object catch (error) {
      _report(error);
    }
  }

  void _learn(Iterable<String> tables) {
    for (final table in tables) {
      if (table == '*' || _tables.containsValue(table)) continue;

      final unresolved = [
        for (final e in _tables.entries)
          if (e.value == null) e.key,
      ];

      if (unresolved.length == 1) _tables[unresolved.single] = table;
    }
  }

  String? _entityOf(String table) {
    _learn([table]);

    for (final e in _tables.entries) {
      if (e.value == table) return e.key;
    }

    return null;
  }

  void _onDoc(DocKey k) {
    if (!_running) return;

    final entity = _entityOf(k.table);
    final doc = store.getDocumentState(k.table, k.pk);

    if (entity != null && doc != null) _project(entity, [doc]);

    // A local write from any path (apply, or a typed op through
    // GroveSyncSource.replica) grows the queue; a remote change does not.
    final queued = store.pending.length;

    if (queued > _pendingSeen && store.pendingCount > 0) _schedulePush();

    _pendingSeen = queued;
    onStatusChange();
  }

  void _onEngine(SyncEngineEvent e) {
    if (!_running) return;

    switch (e) {
      case DatasetGone(:final message):
        _goneMessage = message;
        _retryTimer?.cancel();
        _closeLive();
        _armProbe();
      case AuthRequired():
        _retryTimer?.cancel();
        _closeLive();
        _armProbe();
      case SyncSucceeded():
        _goneTimer?.cancel();
        _retryTimer?.cancel();
        _retry.reset();
        _reopenLive();
      case SyncInterrupted():
        _armRetry();
      case ChangesApplied(:final affected):
        _learn(affected.map((k) => k.table));
      case ChangeRejected() || ClockRebased():
        break;
    }

    // Only ever lowered here (a push drained the queue), so a local write
    // that lands during a run is still seen as growth by _onDoc.
    final queued = store.pending.length;

    if (queued < _pendingSeen) _pendingSeen = queued;

    onStatusChange();
  }

  /// Retries an interrupted run after a jittered, growing delay, whatever the
  /// live channel. Not while offline: coming back online syncs at once.
  void _armRetry() {
    _retryTimer?.cancel();
    _retryTimer = null;

    if (!_running || !_online) return;

    _retryTimer = Timer(_retry.next(), () {
      _retryTimer = null;

      if (_running) unawaited(syncNow());
    });
  }

  /// Opens the live channel after a successful run, unless it is open. After
  /// the stream was refused, waits a backoff delay first, so a stream that
  /// keeps refusing while pulls succeed is not reopened in a tight loop.
  void _reopenLive() {
    if (_live != null || _stopPoll != null || _reopenTimer != null) return;

    if (!_streamRefused) {
      _openLive();

      return;
    }

    _reopenTimer = Timer(_streamBackoff.next(), () {
      _reopenTimer = null;

      if (!_running || _live != null || _stopPoll != null || _terminal) return;

      _openLive();
    });
  }

  bool get _terminal =>
      engine.state == SyncEngineState.gone ||
      engine.state == SyncEngineState.unauthorized;

  void _cancelTimers() {
    _pushTimer?.cancel();
    _goneTimer?.cancel();
    _retryTimer?.cancel();
    _reopenTimer?.cancel();
    _pushTimer = null;
    _goneTimer = null;
    _retryTimer = null;
    _reopenTimer = null;
  }

  void _armProbe() {
    _goneTimer?.cancel();

    if (_running) _goneTimer = Timer(goneRecheck, () => unawaited(syncNow()));
  }

  HLC? _minCursor() {
    HLC? min;

    for (final t in _tables.values) {
      final c = t == null ? null : engine.cursor(t);

      if (c != null && (min == null || c.compareTo(min) < 0)) min = c;
    }

    return min;
  }

  void _openLive() {
    if (!_running) return;

    final known = [for (final t in _tables.values) ?t];
    final allKnown = known.length == _tables.length;
    final choice = switch (live) {
      LiveChannel.auto =>
        endpoints.stream != null
            ? LiveChannel.sse
            : (endpoints.socket != null && allKnown
                  ? LiveChannel.websocket
                  : LiveChannel.poll),
      final other => other,
    };
    final CrdtSubscription sub;

    switch (choice) {
      case LiveChannel.sse when endpoints.stream != null:
        // Through the client, which announces presence again after every
        // reconnect (the server drops it when the stream ends).
        sub = client.stream(
          StreamConfig(
            tables: known,
            since: _minCursor(),
            nodeId: store.nodeId,
          ),
        );
      case LiveChannel.websocket when endpoints.socket != null && allKnown:
        final ws = WebSocketTransport(
          url: _baseUrl.replace(
            scheme: _baseUrl.scheme == 'https' ? 'wss' : 'ws',
            path: endpoints.socket,
          ),
          auth: _auth,
        );

        _ws = ws;
        sub = ws.subscribe(StreamConfig(tables: known));
      default:
        _stopPoll = engine.start(interval: pollInterval);

        return;
    }

    _live = sub;
    // The engine applies streamed changes, and starts a run on a reconnect
    // but not on an idle recycle, which loses nothing.
    _liveHandlers
      ..add(engine.attachStream(sub))
      ..add(
        sub.on((event) {
          if (event is StreamConnected) {
            _streamRefused = false;
            _streamBackoff.reset();

            return;
          }

          // A cancellation is the stream ending because the principal moved
          // on or the dataset stopped: expected, never a failure.
          if (event is! StreamError || isCancellation(event.error)) return;

          _report(event.error);

          if (!_running || !_refusesStream(event.error)) return;

          // The dataset went away, or the credentials stopped working, while
          // the stream was up: the stream alone would reconnect against it
          // forever and the engine would never hear. Close it and let a pull
          // classify the status; a terminal state then keeps it closed until
          // the probe finds the dataset again.
          _streamRefused = true;
          _closeLive();
          unawaited(syncNow());
        }),
      );
    sub.connect();
  }

  void _closeLive() {
    for (final remove in _liveHandlers) {
      remove();
    }

    _liveHandlers.clear();
    _live?.disconnect();
    _live = null;

    final ws = _ws;

    _ws = null;

    if (ws != null) unawaited(ws.close());

    final stopPoll = _stopPoll;

    _stopPoll = null;

    if (stopPoll != null) unawaited(stopPoll());
  }

  /// Runs one sync; probes a gone or unauthorized dataset once. Does nothing
  /// once stopped, while the replica is unavailable or before it hydrated.
  Future<void> syncNow() async {
    if (!_running || !_ready || _unavailable != null) return;

    if (engine.state == SyncEngineState.gone ||
        engine.state == SyncEngineState.unauthorized) {
      engine.resume();
    }

    try {
      await engine.sync();
    } on Object {
      // A retryable failure is reported as SyncInterrupted and the status says
      // Offline; a cancellation belongs to a stop.
    }
  }

  void _schedulePush() {
    if (!_running || _terminal) return;

    _pushTimer?.cancel();
    _pushTimer = Timer(pushDebounce, () {
      _pushTimer = null;

      if (_running) unawaited(syncNow());
    });
  }

  /// Follows the connectivity signal.
  void setOnline(bool online) {
    if (!_running) return;

    _online = online;

    if (online) {
      unawaited(syncNow());
    } else {
      _retryTimer?.cancel();
      _retryTimer = null;
    }

    onStatusChange();
  }

  String _columnFor(GroveEntity b, String wireKey) {
    for (final e in b.columns.entries) {
      if (e.value == wireKey) return e.key;
    }

    return wireKey;
  }

  /// Writes [write] (whose id is the raw pk) to the replica, projects it,
  /// and schedules a push. Returns the client-shaped record, null for a
  /// delete.
  ///
  /// Throws a [StateError] when the dataset stopped or its replica is
  /// unavailable, or while the entity's table is unknown.
  Object? apply(String entity, GroveWrite write) {
    final unavailable = _unavailable;

    if (unavailable != null) {
      throw StateError(
        'grove: the replica of dataset "$datasetId" is unavailable: '
        '$unavailable',
      );
    }

    if (!_running || !_ready) {
      throw StateError('grove: dataset "$datasetId" is not syncing');
    }

    final table =
        _tables[entity] ??
        (throw StateError(
          'grove: the server table for $entity in dataset "$datasetId" is not '
          'known yet; pass it as GroveDataset.table or sync once before '
          'writing',
        ));
    final binding = bindings[entity]!;

    switch (write) {
      case GroveDelete(:final id):
        store.deleteDocument(table, id);
      case GroveUpsert(:final id, :final wireFields):
        store.transact(() {
          for (final e in wireFields.entries) {
            store.reconcileField(
              table,
              id,
              _columnFor(binding, e.key),
              binding.types[e.key] ?? CrdtType.lww,
              e.value,
            );
          }
        });
    }

    final doc = store.getDocumentState(table, write.id);

    _schedulePush();
    onStatusChange();

    if (doc == null) return null;

    projectors[entity]!.project(context, [doc]);

    return write is GroveDelete ? null : projectors[entity]!.clientRecord(doc);
  }

  /// Pushable pending changes of [entity].
  int pending(String entity) {
    final table = _tables[entity];

    return table == null
        ? store.pendingCount
        : store.getPendingChanges().where((c) => c.table == table).length;
  }

  /// Rejected changes of [entity].
  List<GroveRejectedChange> rejected(String entity) {
    final table = _tables[entity];

    return [
      for (final p in store.pending)
        if (p.isRejected && (table == null || p.change.table == table))
          GroveRejectedChange(
            key: p.key,
            entity: entity,
            id: projectors[entity]!.storeId(p.change.pk),
            field: p.change.field,
            kind: p.rejection!.kind,
            reason: p.rejection!.reason,
          ),
    ];
  }

  /// This dataset's status for [entity].
  SyncStatus status(String entity) {
    final unavailable = _unavailable;

    if (unavailable != null) return SyncFailed(unavailable);

    return datasetStatus(
      engine: engine.state,
      online: _online,
      pending: pending(entity),
      rejected: rejected(entity),
      datasetId: datasetId,
      goneMessage: _goneMessage,
    );
  }

  /// This dataset's status across its entities.
  SyncStatus overall() => foldSyncStatus([for (final e in entities) status(e)]);

  /// Live client-shaped records of [entity].
  List<Object?> records(String entity) {
    final table = _tables[entity];

    if (table == null) return const [];

    return [
      for (final d in store.exportTable(table).values)
        if (!d.tombstone) projectors[entity]!.clientRecord(d),
    ];
  }

  /// Node ids other than this device's that authored replica state.
  Set<String> peers() {
    final mine = store.nodeId.split('~').first;

    return {
      for (final table in store.tables)
        for (final d in store.exportTable(table).values)
          for (final f in d.fields.values)
            if (f.nodeId.split('~').first != mine) f.nodeId,
    };
  }

  /// Devtools view of [entity] in this dataset.
  Map<String, Object?> describe(String entity) {
    final s = status(entity);

    return {
      'dataset': datasetId,
      'table': _tables[entity],
      'pending': pending(entity),
      'status': statusName(s),
      'lastPull': engine.lastSyncTime?.toUtc().toIso8601String(),
      if (s is SyncFailed) 'error': s.error.toString(),
    };
  }

  /// Makes a rejected change pushable again.
  void retryRejected(String key) {
    if (!_running || !_ready) return;

    engine.retryRejected(key);
    _schedulePush();
    onStatusChange();
  }

  /// Drops a rejected change and restores the server's value.
  Future<void> discardRejected(String key) async {
    if (!_running || !_ready) return;

    await engine.discardRejected(key);

    if (_running) onStatusChange();
  }

  /// Stops syncing (see [stop]) and evicts this dataset's records from the
  /// store, through the fence. The replica stays at rest; the caller erases
  /// it when asked to.
  Future<void> leave() async {
    await stop();

    final evicted = <String>{};

    context.write((entityStore) {
      final stamp = entityStore.nextFrame();

      for (final table in store.tables) {
        final entity = _entityOf(table);

        if (entity == null) continue;

        for (final pk in store.exportTable(table).keys) {
          entityStore.evict(
            '$entity:${projectors[entity]!.storeId(pk)}',
            stamp,
          );
        }

        evicted.add(entity);
      }
    });

    if (context.active && evicted.isNotEmpty) {
      context.cache.invalidate([for (final e in evicted) '$e[]']);
    }
  }

  /// Stops the timers and the live channel, stops the engine and waits for
  /// it (the request in flight is abandoned, not awaited), stops the client
  /// and the transports, then disposes the replica, which flushes it.
  /// Idempotent: a second call returns the first call's future.
  ///
  /// After this the replica takes no write and persists nothing, so a late
  /// response cannot bring data back, for instance after an erase.
  Future<void> stop() => _stopping ??= _stop();

  Future<void> _stop() async {
    _cancelTimers();
    _unwatchPrincipal();
    _closeLive();

    _markHydrated();

    await engine.stop();

    // Broadcast subscriptions: nothing to wait for on cancel.
    for (final s in _subs) {
      unawaited(s.cancel());
    }

    _subs.clear();
    await engine.dispose();
    await client.dispose();
    _transport.close();

    try {
      await store.dispose();
    } on Object catch (error) {
      // Data that did not reach storage: say so even after a switch. The
      // error names the storage failure, never a row.
      context.cache.report(error, 'grove');
    }
  }
}
