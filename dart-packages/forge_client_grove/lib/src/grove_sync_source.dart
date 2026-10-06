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

  /// The background load [GroveSyncSource.start] kicked off: open the
  /// replicas, hydrate and project them. Never fails.
  Future<void> loading = Future<void>.value();

  /// Whether [loading] finished (or was abandoned).
  bool loaded = false;

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
/// context is fenced, a [join] made then waits for the start of the principal
/// the cache serves, and status changes are no longer published.
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

  /// The cache the source last started under. Only its `principal` is ever
  /// read here, never its store.
  QueryCache? _cache;

  /// Joins made while no principal's run was active, by the principal the
  /// cache served when each was made, then dataset id. Consumed only by a
  /// [start] for that principal.
  final Map<String, Map<String, GroveDataset>> _pendingJoins = {};

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
  /// running, the joins the cache's current principal has waiting for its
  /// [start].
  Set<String> get joined {
    final run = _active;

    if (run != null) return {...run.joined.keys};

    return {...?_pendingJoins[_cache?.principal]?.keys};
  }

  void _removePending(String? principal, String datasetId) {
    final joins = _pendingJoins[principal];

    if (joins == null) return;

    joins.remove(datasetId);

    if (joins.isEmpty) _pendingJoins.remove(principal);
  }

  /// Returns at once. Opening the principal's replicas, hydrating and
  /// projecting them runs in the background under [context], and is
  /// abandoned at the first await after the context is fenced; [stop] waits
  /// for it. The app's `datasets` callback runs after that, and is not waited
  /// for.
  @override
  Future<void> start(SyncContext context) async {
    // The cache stops a source before starting it again; be safe anyway.
    if (_run != null) await stop();

    final run = _run = _Run(context);
    final principal = context.principal;

    _cache = context.cache;
    _lastEntity.clear();
    _lastDataset.clear();

    // This principal's own joins, made while it was not running.
    for (final ds in [...?_pendingJoins.remove(principal)?.values]) {
      run.joined[ds.id] = ds;
    }

    run.loading = _load(run).whenComplete(() => run.loaded = true);
  }

  Future<void> _load(_Run run) async {
    final context = run.context;

    try {
      final space = await ReplicaSpace.open(
        context.storage,
        newNodeId: newNodeId,
      );

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
        // Left while an earlier dataset was loading.
        if (!run.joined.containsKey(ds.id)) continue;

        await _startGroup(run, _withDataset.toList(), ds);

        if (!run.live) return;
      }
    } on Object catch (error) {
      // After the fence the failure (a session closed under the load) is the
      // superseded run's, and nobody's to hear.
      if (run.live) context.cache.report(error, 'grove');

      return;
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
  /// A join belongs to the principal the cache serves when it is made
  /// (`cache.principal`). When that principal's run is not active yet (the
  /// switch window, while the previous principal's fenced run is still
  /// stopping, or between two runs) the join waits, and only that principal's
  /// [start] joins it. A fenced run is never credited with a join made after
  /// its fence. Joining an already joined id is a no-op, except that a table
  /// given now and unknown before is adopted.
  ///
  /// While the principal's replicas are still loading after [start], the
  /// join is recorded and returns at once; the load starts the dataset.
  /// Writes wait for the load, so they find it.
  ///
  /// Throws [StateError] when no declaration takes a dataset, and when no
  /// principal is signed in (`join requires a signed-in principal`), which
  /// includes before the cache first started this source.
  Future<void> join(GroveDataset dataset) async {
    if (_withDataset.isEmpty) {
      throw StateError(
        'grove: no declaration in the sync table takes a dataset',
      );
    }

    final run = _active;
    final current = run?.context.principal ?? _cache?.principal;

    if (current == null) {
      throw StateError('join requires a signed-in principal');
    }

    return _joinAs(current, dataset);
  }

  /// Joins [dataset] for [principal], the principal captured when [join] was
  /// called. Never re-reads the cache's principal after an await.
  ///
  /// A rejoin while that principal's erase of the dataset is in flight waits
  /// for it. Then, while [principal]'s run is active, the dataset starts fresh
  /// over the erased replica; otherwise the join waits in [principal]'s queue
  /// for its next start, whoever the cache serves by then.
  Future<void> _joinAs(String principal, GroveDataset dataset) async {
    final key = _erasingKey(principal, dataset.id);

    for (
      var erasing = _erasing[key];
      erasing != null;
      erasing = _erasing[key]
    ) {
      await erasing;
    }

    final run = _active;

    if (run != null && run.context.principal == principal) {
      return _joinInto(run, dataset);
    }

    final joins = _pendingJoins[principal] ??= {};

    joins[dataset.id] = _merge(joins[dataset.id], dataset);
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
  /// flight cannot write it back.
  ///
  /// A leave acts for the principal the cache serves now. While that
  /// principal's replicas are still loading, the dataset is dropped from the
  /// load, and an erase waits for the load to finish (or tear the dataset
  /// down) before it erases. While its run has not started yet (the switch
  /// window), the leave drops that principal's waiting join, never another
  /// principal's, and an erase waits until the run is active.
  ///
  /// Throws [StateError] with [erase] when no principal is signed in, or when
  /// the principal changes before the erase could run.
  Future<void> leave(String datasetId, {bool erase = false}) async {
    if (datasetId.isEmpty) {
      throw ArgumentError.value(datasetId, 'datasetId', 'must not be empty');
    }

    final active = _active;

    if (!erase) {
      if (active == null) {
        _removePending(_cache?.principal, datasetId);

        return;
      }

      return _leave(active, datasetId, erase: false);
    }

    final cache = _cache;
    final principal = active?.context.principal ?? cache?.principal;

    if (cache == null || principal == null) {
      throw StateError(
        'grove: leave("$datasetId", erase: true) requires a signed-in '
        'principal',
      );
    }

    // A join of the same dataset by the same principal waits for this.
    final key = _erasingKey(principal, datasetId);
    final done = Completer<void>();
    final previous = _erasing[key];

    _erasing[key] = done.future;

    try {
      if (previous != null) await previous;

      var run = active;

      if (run == null) {
        _removePending(principal, datasetId);

        // The principal's run starts once the transition in progress is done.
        await cache.idle;
        run = _active;

        if (run == null || run.context.principal != principal) {
          throw _principalChanged(datasetId);
        }
      }

      await _leave(run, datasetId, erase: true);
    } finally {
      _erasing.removeWhere((k, f) => k == key && identical(f, done.future));

      done.complete();
    }
  }

  Future<void> _leave(_Run run, String datasetId, {required bool erase}) async {
    // Before any await: a load that has not reached it skips it.
    run.joined.remove(datasetId);

    if (erase && !run.loaded) {
      await run.loading;

      if (!run.live) throw _principalChanged(datasetId);
    }

    final sync = _syncOf(run, datasetId);

    if (sync != null) run.syncs.remove(sync.replicaKey);

    final space = run.space;

    if (erase && space == null) {
      throw StateError(
        'grove: the replicas of this principal could not be opened; '
        '"$datasetId" was not erased',
      );
    }

    await _track(run, () async {
      if (sync != null) await sync.leave();

      if (erase) {
        await space!.erase(
          sync?.replicaKey ?? replicaKey(datasetId: datasetId, pullPath: ''),
        );
      }
    });

    _lastDataset.remove(datasetId);
    _publish();
  }

  /// Erases in flight, by principal and dataset id. Each completes normally
  /// whatever the erase did.
  final Map<String, Future<void>> _erasing = {};

  static String _erasingKey(String principal, String datasetId) =>
      '${principal.length}:$principal$datasetId';

  static StateError _principalChanged(String datasetId) => StateError(
    'grove: the principal changed before leave("$datasetId", erase: true) '
    'could erase',
  );

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

    // Right after a start the replicas may still be loading.
    if (!run.loaded) {
      await run.loading;

      if (!run.live) {
        return Rejected(
          StateError('grove: the principal changed; the write was not applied'),
        );
      }
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
  ///
  /// It refetches the field first. A discard cut off by a principal switch
  /// fails with a [CrdtError] of code [CrdtErrorCode.cancelled] and leaves the
  /// change rejected, as it was; nothing is dropped.
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
  /// Waits for the background load (which gives up at its next await), and
  /// for joins and leaves in flight, then stops each dataset (engine first,
  /// abandoning the request in flight, then client and transports, then the
  /// replica, which flushes it), all before the cache closes the session.
  /// Joins waiting for a principal's next start are kept (see [join]).
  @override
  Future<void> stop() async {
    final run = _run;

    if (run == null) return;

    _run = null;
    run.stopped = true;
    // Bounded: the load gives up at its next await now that the run stopped.
    await run.loading;
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
