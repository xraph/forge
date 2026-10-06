import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:http/http.dart' as http;

import 'config.dart';
import 'dataset_sync.dart';
import 'field_writer.dart';
import 'projection.dart';
import 'replica_space.dart';
import 'status.dart';

/// One principal's run of the source: everything [GroveSyncSource.start]
/// builds for one [SyncContext], and nothing that outlives it.
final class _Run {
  _Run(this.context);

  /// The principal's fence.
  final SyncContext context;

  /// The principal's replicas, once open.
  ReplicaSpace? space;

  /// By replica key.
  final Map<String, DatasetSync> syncs = {};

  /// The principal's datasets, by id.
  final Map<String, GroveDataset> joined = {};

  /// Joins and leaves in flight; [GroveSyncSource.stop] waits for them, so
  /// none touches the session after it closed.
  final Set<Future<void>> busy = {};

  StreamSubscription<bool>? online;
  bool stopped = false;

  /// Whether the cache still serves this principal and the run was not
  /// stopped. Checked after every await.
  bool get live => !stopped && context.active;
}

/// Owns the Grove-backed entities of a generated client and syncs them over
/// Grove's CRDT protocol. One per app, registered once in
/// `QueryCache.syncSources`; datasets are joined and left at runtime.
///
/// Nothing crosses principals. Every write into the entity store goes through
/// the [SyncContext] the cache started this principal's run with, which the
/// cache fences synchronously inside `setPrincipal`. The auth adapter refuses
/// to read credentials (or refresh them) for a fenced context, so a request
/// the old principal's run still makes never carries the next principal's
/// credentials. Public reads ([records], [rejected], [replica], [statusOf],
/// [describeForDevtools], [joined]) answer as if nothing were joined once the
/// context is fenced, a [join] made then waits for the next [start], and
/// status changes are no longer published.
final class GroveSyncSource implements SyncSource, DevtoolsInspectable {
  /// Creates the source. See the package README for the parameters.
  ///
  /// [newNodeId] mints a first-time node id for a principal (default
  /// `dart-<uuid v4>`) and [sseConnect] replaces how the SSE request is made;
  /// both are for tests.
  ///
  /// Throws [ArgumentError] naming any grove entity without a [bindings] entry
  /// or without an `idField` in [entities], and when the declarations that
  /// carry a dataset do not share one set of endpoints.
  GroveSyncSource({
    required List<SyncDeclaration> declarations,
    required EntitySchema entities,
    required Map<String, GroveEntity> bindings,
    required this.baseUrl,
    this.datasets,
    this.envelope = groveEnvelope,
    this.envelopes = const {},
    this.auth,
    this.security = const [],
    this.httpClient,
    this.live = LiveChannel.auto,
    this.pollInterval = const Duration(seconds: 30),
    this.pushDebounce = const Duration(milliseconds: 150),
    this.goneRecheck = const Duration(minutes: 5),
    this.connectivity,
    this.nowMs,
    this.newId,
    this.newNodeId,
    this.sseConnect,
  }) : schema = entities,
       _declarations = [
         for (final d in declarations)
           if (d.protocol == groveCrdtProtocol) d,
       ],
       _bindings = bindings {
    final missing = [
      for (final d in _declarations)
        if (!bindings.containsKey(d.entity) ||
            entities[d.entity]?.idField == null)
          d.entity,
    ];

    if (missing.isNotEmpty) {
      throw ArgumentError(
        'grove: no GroveEntity binding or idField for ${missing.join(', ')}',
      );
    }

    final shapes = {
      for (final d in _withDataset)
        '${d.pull}|${d.push}|${d.stream}|${d.socket}|${d.dataset}',
    };

    if (shapes.length > 1) {
      throw ArgumentError(
        'grove: declarations with a dataset must share one set of sync '
        'endpoints',
      );
    }
  }

  final List<SyncDeclaration> _declarations;
  final Map<String, GroveEntity> _bindings;

  /// The generated entities table (the `entities` constructor argument).
  final EntitySchema schema;

  /// Server root the declared paths hang off.
  final Uri baseUrl;

  /// Datasets joined at every start, in addition to [join] calls. Called in
  /// the background after [start] returned; its answer is used only while the
  /// same principal is still signed in.
  final GroveDatasets? datasets;

  /// Default framing.
  final SyncEnvelope envelope;

  /// Per-entity framing overrides.
  final Map<String, SyncEnvelope> envelopes;

  /// Credentials for sync requests.
  final AuthProvider? auth;

  /// Security scheme names passed to [auth].
  final List<String> security;

  /// HTTP client override.
  final http.Client? httpClient;

  /// Live channel preference.
  final LiveChannel live;

  /// Poll interval when polling.
  final Duration pollInterval;

  /// Delay between a write and its push.
  final Duration pushDebounce;

  /// Minimum time between probes of a gone dataset.
  final Duration goneRecheck;

  /// Optional connectivity signal.
  final ConnectivitySignal? connectivity;

  /// Wall clock override (tests).
  final int Function()? nowMs;

  /// Row id minting override (tests).
  final String Function()? newId;

  /// Node id minting override (tests).
  final String Function()? newNodeId;

  /// SSE connector override (tests).
  final SseConnect? sseConnect;

  Iterable<SyncDeclaration> get _withDataset =>
      _declarations.where((d) => datasetParam(d) != null);

  Iterable<SyncDeclaration> get _withoutDataset =>
      _declarations.where((d) => datasetParam(d) == null);

  _Run? _run;

  /// Joins made while no principal's run was active, for the next [start].
  final Map<String, GroveDataset> _pendingJoins = {};

  final Map<String, SyncStatus> _lastEntity = {};
  final Map<String, SyncStatus> _lastDataset = {};
  final _entityChanges = StreamController<(String, SyncStatus)>.broadcast(
    sync: true,
  );
  final _datasetChanges = StreamController<(String, SyncStatus)>.broadcast(
    sync: true,
  );

  /// The run whose principal the cache still serves, or null.
  _Run? get _active {
    final run = _run;

    return run != null && run.live ? run : null;
  }

  @override
  Set<String> get entities => {for (final d in _declarations) d.entity};

  /// Ids of the joined datasets: the running principal's, or, while none is
  /// running, the joins waiting for the next [start].
  Set<String> get joined {
    final run = _active;

    return run == null ? {..._pendingJoins.keys} : {...run.joined.keys};
  }

  @override
  Future<void> start(SyncContext context) async {
    // The cache stops a source before starting it again; be safe anyway.
    if (_run != null) await stop();

    final run = _run = _Run(context);

    _lastEntity.clear();
    _lastDataset.clear();

    // Joins made before this start (signed out, or in the window after the
    // previous principal's context was fenced) belong to this principal.
    for (final ds in _pendingJoins.values) {
      run.joined[ds.id] = ds;
    }

    _pendingJoins.clear();

    final ReplicaSpace space;

    try {
      space = await ReplicaSpace.open(context.storage, newNodeId: newNodeId);
    } on Object {
      // The session closed under a run that was already superseded.
      if (!run.live) return;

      rethrow;
    }

    if (!run.live) return;

    run.space = space;

    final plain = <String, List<SyncDeclaration>>{};

    for (final d in _withoutDataset) {
      (plain[resolveEndpoints(d, '').pull] ??= []).add(d);
    }

    for (final group in plain.values) {
      await _startGroup(run, group, const GroveDataset(''));

      if (!run.live) return;
    }

    for (final ds in [...run.joined.values]) {
      await _startGroup(run, _withDataset.toList(), ds);

      if (!run.live) return;
    }

    run.online = connectivity?.online.listen((online) {
      if (!run.live) return;

      for (final s in run.syncs.values) {
        s.setOnline(online);
      }
    });
    _publish();

    final initial = datasets;

    if (initial != null && _withDataset.isNotEmpty) {
      unawaited(_resolveDatasets(run, initial));
    }
  }

  /// Runs the app's `datasets` callback after [start] returned, which may do
  /// network work, and joins its answer only while [run] is still live.
  Future<void> _resolveDatasets(_Run run, GroveDatasets initial) async {
    final List<GroveDataset> found;

    try {
      found = await initial(_withDataset.first);
    } on Object catch (error) {
      if (run.live) run.context.cache.report(error, 'grove');

      return;
    }

    if (!identical(_run, run) || !run.live) return;

    for (final ds in found) {
      await _joinInto(run, ds);

      if (!run.live) return;
    }
  }

  Future<void> _startGroup(
    _Run run,
    List<SyncDeclaration> decls,
    GroveDataset ds,
  ) async {
    final space = run.space;

    if (space == null || !run.live) return;

    final endpoints = resolveEndpoints(decls.first, ds.id);
    final key = replicaKey(datasetId: ds.id, pullPath: endpoints.pull);

    if (run.syncs.containsKey(key)) return;

    final context = run.context;
    final tables = {
      for (final d in decls) d.entity: declaredTable(d) ?? ds.table,
    };
    final sync = DatasetSync(
      context: context,
      datasetId: ds.id,
      replicaKey: key,
      endpoints: endpoints,
      entityTables: tables,
      bindings: _bindings,
      projectors: {
        for (final entity in tables.keys)
          entity: Projector(
            entity: entity,
            binding: _bindings[entity]!,
            wireIdKey: wireIdKeyFor(
              _bindings[entity]!,
              schema[entity]!.idField!,
            ),
            datasetId: ds.id,
          ),
      },
      storage: space.dataset(key),
      nodeId: space.nodeId,
      baseUrl: baseUrl,
      envelope: envelopes[decls.first.entity] ?? envelope,
      live: live,
      pollInterval: pollInterval,
      pushDebounce: pushDebounce,
      goneRecheck: goneRecheck,
      onStatusChange: _publish,
      httpClient: httpClient,
      auth: _authFor(context, endpoints),
      onUnauthorized: _refreshFor(context),
      sseConnect: sseConnect,
      nowMs: nowMs,
    );

    run.syncs[key] = sync;
    await sync.start();
  }

  CrdtAuthProvider? _authFor(SyncContext context, GroveEndpoints endpoints) {
    final provider = auth;

    if (provider == null) return null;

    return _Auth(
      provider,
      context,
      OperationMeta(
        id: 'grove_sync',
        method: 'POST',
        path: endpoints.pull,
        security: security,
      ),
    );
  }

  Future<bool> Function()? _refreshFor(SyncContext context) {
    final provider = auth;

    if (provider == null) return null;

    return () async {
      // A fenced context's 401 is not the next principal's to refresh.
      if (!context.active) throw _cancelled();

      await provider.refresh();

      if (!context.active) throw _cancelled();

      return true;
    };
  }

  static GroveDataset _merge(GroveDataset? known, GroveDataset next) =>
      known == null || known.table != null || next.table == null
      ? (known ?? next)
      : GroveDataset(known.id, table: next.table);

  /// Starts syncing [dataset].
  ///
  /// While no principal is running, or between the moment the cache fenced
  /// the previous principal and the moment it stopped this source, the join
  /// waits for the next [start]: it belongs to whoever is signed in next,
  /// never to the principal on the way out. Joining an already joined id is a
  /// no-op, except that a table given now and unknown before is adopted.
  ///
  /// Throws [StateError] when no declaration takes a dataset.
  Future<void> join(GroveDataset dataset) async {
    if (_withDataset.isEmpty) {
      throw StateError(
        'grove: no declaration in the sync table takes a dataset',
      );
    }

    final run = _active;

    if (run == null) {
      _pendingJoins[dataset.id] = _merge(_pendingJoins[dataset.id], dataset);

      return;
    }

    await _joinInto(run, dataset);
  }

  Future<void> _joinInto(_Run run, GroveDataset dataset) async {
    final existing = _syncOf(run, dataset.id);

    if (existing != null) {
      final table = dataset.table;

      if (table != null) {
        for (final e in existing.entities) {
          existing.adoptTable(e, table);
        }
      }

      run.joined[dataset.id] = _merge(run.joined[dataset.id], dataset);

      return;
    }

    final prior = run.joined[dataset.id];

    run.joined[dataset.id] = _merge(prior, dataset);

    // Already queued for the start in progress, which starts it.
    if (prior != null || run.space == null) return;

    await _track(
      run,
      () => _startGroup(run, _withDataset.toList(), run.joined[dataset.id]!),
    );

    if (run.live) _publish();
  }

  Future<void> _track(_Run run, Future<void> Function() body) async {
    final future = body();

    run.busy.add(future);

    try {
      await future;
    } finally {
      run.busy.remove(future);
    }
  }

  /// Stops syncing [datasetId] and evicts its rows from the store. The replica
  /// stays, so a later [join] restores it without the network.
  ///
  /// With [erase], the run is cancelled and the replica disposed (which
  /// flushes it) before its namespace is erased, so a response still in
  /// flight cannot write it back. Erasing needs the principal's session:
  /// with [erase] and no principal running this throws [StateError]. Without
  /// [erase] and no principal running it only drops a join waiting for the
  /// next [start].
  Future<void> leave(String datasetId, {bool erase = false}) async {
    if (datasetId.isEmpty) {
      throw ArgumentError.value(datasetId, 'datasetId', 'must not be empty');
    }

    final run = _active;
    final space = run?.space;

    if (run == null || space == null) {
      if (erase) {
        throw StateError(
          'grove: leave("$datasetId", erase: true) needs a running principal; '
          'set one first',
        );
      }

      _pendingJoins.remove(datasetId);

      return;
    }

    run.joined.remove(datasetId);

    final sync = _syncOf(run, datasetId);

    if (sync != null) run.syncs.remove(sync.replicaKey);

    await _track(run, () async {
      if (sync != null) await sync.leave();

      if (erase) {
        await space.erase(
          sync?.replicaKey ?? replicaKey(datasetId: datasetId, pullPath: ''),
        );
      }
    });

    _lastDataset.remove(datasetId);
    _publish();
  }

  DatasetSync? _syncOf(_Run run, String datasetId) {
    if (datasetId.isEmpty) return null;

    for (final s in run.syncs.values) {
      if (s.datasetId == datasetId) return s;
    }

    return null;
  }

  Iterable<DatasetSync> _of(_Run run, String entity) =>
      run.syncs.values.where((s) => s.entities.contains(entity));

  SyncStatus _entityStatus(String entity) {
    final run = _active;

    if (run == null) return const Synced();

    final parts = [for (final s in _of(run, entity)) s.status(entity)];

    return parts.isEmpty ? const Synced() : foldSyncStatus(parts);
  }

  SyncStatus _datasetStatus(String datasetId) {
    final run = _active;

    if (run == null) return const Synced();

    return _syncOf(run, datasetId)?.overall() ?? const Synced();
  }

  /// Announces status changes, only while the principal is still served: a
  /// status computed after the fence would describe the previous principal.
  void _publish() {
    final run = _active;

    if (run == null) return;

    for (final entity in entities) {
      final s = _entityStatus(entity);

      if (_lastEntity[entity] == s) continue;

      _lastEntity[entity] = s;
      _entityChanges.add((entity, s));
      run.context.cache.observer?.call(
        SyncStatusChanged(entity: entity, status: s),
      );
    }

    for (final id in run.joined.keys) {
      final s = _datasetStatus(id);

      if (_lastDataset[id] == s) continue;

      _lastDataset[id] = s;
      _datasetChanges.add((id, s));
    }
  }

  Stream<SyncStatus> _watch(
    StreamController<(String, SyncStatus)> ctl,
    String key,
    SyncStatus Function() now,
  ) => Stream.multi((c) {
    c.add(now());

    final sub = ctl.stream.where((e) => e.$1 == key).listen((e) => c.add(e.$2));

    c.onCancel = sub.cancel;
  });

  @override
  Stream<SyncStatus> status(String entity) =>
      _watch(_entityChanges, entity, () => _entityStatus(entity));

  /// One dataset's status, folded across its entities. [Synced] for a dataset
  /// that is not joined, and while no principal is running.
  Stream<SyncStatus> statusOf(String datasetId) =>
      _watch(_datasetChanges, datasetId, () => _datasetStatus(datasetId));

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async {
    final run = _active;

    if (run == null) {
      return Rejected(
        StateError('grove: no principal is running; the write was not applied'),
      );
    }

    final entity = mutation.meta.entity;

    if (entity == null || !entities.contains(entity)) {
      return Rejected(
        StateError('grove: ${mutation.meta.id} does not target a grove entity'),
      );
    }

    final decl = _declarations.firstWhere((d) => d.entity == entity);
    final param = datasetParam(decl);
    final GroveWrite write;
    final DatasetSync sync;

    try {
      write = toGroveWrite(
        mutation,
        entity: entity,
        meta: schema[entity]!,
        binding: _bindings[entity]!,
        wireIdKey: wireIdKeyFor(_bindings[entity]!, schema[entity]!.idField!),
        datasetParam: param,
        newId: newId,
      );
      sync = param == null
          ? _of(run, entity).first
          : _datasetFor(run, mutation, entity, param, write.id);
    } on Object catch (error) {
      return Rejected(error);
    }

    // A dataset joined a moment ago may still be loading its replica.
    if (!sync.ready && !sync.stopped && sync.unavailable == null) {
      await sync.hydrated;

      if (!run.live) {
        return Rejected(
          StateError('grove: the principal changed; the write was not applied'),
        );
      }
    }

    try {
      final pk = param == null ? write.id : _rawPk(mutation, param, write.id);
      final raw = switch (write) {
        GroveUpsert(:final wireFields) => GroveUpsert(pk, wireFields),
        GroveDelete() => GroveDelete(pk),
      };

      return Applied(sync.apply(entity, raw));
    } on Object catch (error) {
      // A codec or mapping failure, or a replica that will not take writes.
      return Rejected(error);
    }
  }

  /// The dataset a write to a datasetted [entity] goes to: the path argument
  /// named by the declaration's dataset placeholder, else the composite id's
  /// prefix, else the only joined dataset.
  DatasetSync _datasetFor(
    _Run run,
    PendingMutation mutation,
    String entity,
    String param,
    String id,
  ) {
    final fromPath = _pathDataset(mutation, param);
    final datasetId = fromPath ?? splitCompositeId(id)?.datasetId;

    if (datasetId != null) {
      return _syncOf(run, datasetId) ??
          (throw StateError(
            'grove: dataset "$datasetId" is not joined; call '
            'GroveSyncSource.join first',
          ));
    }

    final candidates = _of(run, entity).toList();

    if (candidates.length != 1) {
      throw ArgumentError(
        'grove: ${mutation.meta.id} matches ${candidates.length} joined '
        'datasets of $entity; target a composite id or pass the dataset as '
        'the "$param" path argument',
      );
    }

    return candidates.single;
  }

  static String? _pathDataset(PendingMutation mutation, String param) {
    final value = mutation.args.path[param];

    return value == null ? null : '$value';
  }

  /// The server's pk for [id]. With the dataset in the path, the id is raw
  /// unless it starts with that dataset's own prefix, so a raw pk holding a
  /// `:` is never cut at it. Without, the composite prefix is dropped.
  static String _rawPk(PendingMutation mutation, String param, String id) {
    final fromPath = _pathDataset(mutation, param);

    if (fromPath != null) {
      final prefix = compositeId(fromPath, '');

      return id.startsWith(prefix) ? id.substring(prefix.length) : id;
    }

    return splitCompositeId(id)?.pk ?? id;
  }

  /// Syncs every dataset now, probing gone ones once.
  Future<void> syncNow() async {
    final run = _active;

    if (run == null) return;

    for (final s in [...run.syncs.values]) {
      await s.syncNow();
    }
  }

  /// Rejected changes of [entity] across datasets; empty while no principal
  /// is running.
  List<GroveRejectedChange> rejected(String entity) {
    final run = _active;

    if (run == null) return const [];

    return [for (final s in _of(run, entity)) ...s.rejected(entity)];
  }

  DatasetSync? _owning(String key) {
    final run = _active;

    if (run == null) return null;

    for (final s in run.syncs.values) {
      if (s.store.pending.any((p) => p.key == key)) return s;
    }

    return null;
  }

  /// Makes a rejected change pushable again.
  Future<void> retryRejected(String key) async =>
      _owning(key)?.retryRejected(key);

  /// Drops a rejected change and restores the server's value.
  Future<void> discardRejected(String key) async =>
      _owning(key)?.discardRejected(key);

  /// Live client-shaped records of [entity], for lists while offline; empty
  /// while no principal is running.
  List<Object?> records(String entity, {String? datasetId}) {
    final run = _active;

    if (run == null) return const [];

    return [
      for (final s in _of(run, entity))
        if (datasetId == null || s.datasetId == datasetId) ...s.records(entity),
    ];
  }

  /// The replica holding [entity], for typed CRDT ops (counters, text); null
  /// while no principal is running.
  CrdtStore? replica(String entity, [String? datasetId]) {
    final run = _active;

    if (run == null) return null;

    for (final s in _of(run, entity)) {
      if (datasetId == null || s.datasetId == datasetId) return s.store;
    }

    return null;
  }

  @override
  Future<Map<String, Object?>> describeForDevtools() async {
    final run = _active;
    final syncs = run == null ? const <DatasetSync>[] : [...run.syncs.values];
    var hlc = HLC.zero;
    final peers = <String>{};

    for (final s in syncs) {
      hlc = hlcMax(s.store.clock.last, hlc);
      peers.addAll(s.peers());
    }

    Map<String, Object?> entityView(String entity) {
      final parts = [
        for (final s in syncs)
          if (s.entities.contains(entity)) s.describe(entity),
      ];
      final s = _entityStatus(entity);
      String? latest;

      for (final p in parts) {
        final at = p['lastPull'] as String?;

        if (at != null && (latest == null || at.compareTo(latest) > 0)) {
          latest = at;
        }
      }

      return {
        'table': parts.isEmpty ? null : parts.first['table'],
        'pending': parts.fold<int>(0, (n, p) => n + (p['pending']! as int)),
        'status': statusName(s),
        'lastPull': latest,
        if (s is SyncFailed) 'error': s.error.toString(),
        'datasets': parts,
      };
    }

    return {
      'protocol': groveCrdtProtocol,
      'nodeId': run?.space?.nodeId,
      'hlc': {'ts': hlc.ts.toString(), 'counter': hlc.c, 'nodeId': hlc.node},
      'entities': {for (final entity in entities) entity: entityView(entity)},
      'peers': peers.toList()..sort(),
    };
  }

  /// Stops every dataset of the running principal and forgets its joins.
  ///
  /// Waits for joins and leaves in flight, then stops each dataset (engine
  /// first, abandoning the request in flight, then client and transports,
  /// then the replica, which flushes it), all before the cache closes the
  /// session. Joins waiting for the next [start] are kept: they were made
  /// after this principal's context was fenced.
  @override
  Future<void> stop() async {
    final run = _run;

    if (run == null) return;

    _run = null;
    run.stopped = true;
    await run.online?.cancel();
    run.online = null;

    while (run.busy.isNotEmpty) {
      for (final future in [...run.busy]) {
        try {
          await future;
        } on Object {
          // Its caller sees the error.
        }
      }
    }

    for (final s in run.syncs.values) {
      try {
        await s.stop();
      } on Object catch (error) {
        run.context.cache.report(error, 'grove');
      }
    }

    run.syncs.clear();
    run.joined.clear();
    run.space = null;
    _lastEntity.clear();
    _lastDataset.clear();
  }
}

CrdtError _cancelled() => CrdtError(
  'grove: the principal changed; this request belongs to the previous one',
  code: CrdtErrorCode.cancelled,
);

/// forge's [AuthProvider] as grove's [CrdtAuthProvider], fenced by one
/// principal's [SyncContext].
///
/// The provider is the app's and follows whoever is signed in now, so a read
/// for a fenced context would hand the previous principal's request the next
/// principal's credentials. The context is checked before the read and again
/// after it (the read may await a token refresh across a switch); a fenced
/// context throws a [CrdtError] with [CrdtErrorCode.cancelled], which every
/// transport passes on unretried.
final class _Auth implements CrdtAuthProvider {
  _Auth(this._auth, this._context, this._meta);

  final AuthProvider _auth;
  final SyncContext _context;
  final OperationMeta _meta;

  @override
  Future<Map<String, String>> getHeaders() async {
    if (!_context.active) throw _cancelled();

    final headers = await _auth.credentials(_meta);

    if (!_context.active) throw _cancelled();

    return headers ?? const {};
  }
}
