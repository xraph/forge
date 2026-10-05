import 'dart:async';

import 'package:uuid/uuid.dart';

import 'invalidate.dart';
import 'observe.dart';
import 'operation.dart';
import 'overlay.dart';
import 'owned.dart';
import 'ref.dart';
import 'registry.dart';
import 'state.dart';
import 'storage.dart';
import 'store.dart';
import 'stream_types.dart';
import 'sync.dart';
import 'tags.dart';
import 'transport.dart';
import 'types.dart';

/// Extra per-call knobs. On a query they apply only when the call starts a
/// new request sequence: a call that joins a request already in flight shares
/// that request, its headers and its cancellation.
final class RequestOptions {
  /// Creates the options.
  const RequestOptions({this.headers = const {}, this.cancel});

  /// Extra headers for the request.
  final Map<String, String> headers;

  /// Completing this future aborts the request.
  final Future<void>? cancel;
}

/// What a mutation call may add. For an entity a sync source owns, only
/// [optimistic] reaches the source; [place], [headers] and [cancel] have no
/// effect there (see [QueryCache.mutate]).
final class MutateOptions extends RequestOptions {
  /// Creates the options.
  const MutateOptions({
    super.headers,
    super.cancel,
    this.place = const {},
    this.optimistic,
  });

  /// Per-tag placement callbacks. See [Placement].
  final Map<String, Placement> place;

  /// What this mutation changes, shown immediately and reconciled on settle.
  /// Client-shaped: `MutationBinding` converts typed specs before calling.
  final Optimistic<Object?>? optimistic;
}

/// When a stream commit runs. The Flutter adapter supplies a per-frame one.
abstract interface class CommitScheduler {
  /// Arranges for [commit] to run later.
  void schedule(void Function() commit);
}

final class _MicrotaskCommitScheduler implements CommitScheduler {
  const _MicrotaskCommitScheduler();

  @override
  void schedule(void Function() commit) => scheduleMicrotask(commit);
}

/// The default commit scheduler: one commit per microtask.
CommitScheduler microtaskCommitScheduler() => const _MicrotaskCommitScheduler();

/// The stream runtime, as the cache and the adapters see it. Plan 01b's
/// `StreamBinder` implements it and assigns itself to [QueryCache.live].
abstract interface class LiveBinding {
  /// Makes one query live and returns the release. Ref-counted per query and
  /// per socket.
  void Function() subscribe(OperationMeta meta, TagContext args);

  /// Which channels this operation's entities are pushed on.
  List<String> channelsFor(OperationMeta meta);

  /// Subscribes to a declared duplex channel and takes its frames raw.
  void Function() raw(
    String channel,
    FrameHandler handler, [
    SubscribeOptions options = const SubscribeOptions(),
  ]);
}

/// One tracked query, as an inspector sees it. Handed out live: read-only.
abstract interface class TrackedRecord {
  /// The cache key.
  String get key;

  /// The operation.
  OperationMeta get meta;

  /// The arguments.
  TagContext get args;

  /// The lifecycle status.
  QueryStatus get status;

  /// The last failure, if the status is [QueryStatus.error].
  Object? get error;

  /// Whether a request is in flight.
  bool get fetching;

  /// Whether the query has ever settled.
  bool get settled;

  /// The request sequence in flight, if any.
  Future<Object?>? get inflight;

  /// An invalidation landed mid-flight and the answer in progress predates it.
  bool get restart;

  /// How many times a stream frame has overtaken this query's request.
  int get frameRestarts;
}

/// One settled query, as `dehydrate` (plan 01b) reads it.
final class CachedQuery {
  /// Creates the row.
  const CachedQuery({
    required this.key,
    required this.meta,
    required this.args,
    required this.skeleton,
    required this.settledTime,
  });

  /// The cache key.
  final String key;

  /// The operation.
  final OperationMeta meta;

  /// The arguments that reproduce [key]: [TagContext.empty] for a query keyed
  /// by its operation alone.
  final TagContext args;

  /// The skeleton the store holds.
  final Object? skeleton;

  /// When it settled, on the cache's clock.
  final int settledTime;
}

/// What [QueryCache.restore] installs.
final class RestoreInput {
  /// Creates the input.
  const RestoreInput({
    required this.meta,
    this.args = TagContext.empty,
    required this.skeleton,
    this.tags,
    this.response,
    this.stale = false,
    this.settledTime,
  });

  /// The operation.
  final OperationMeta meta;

  /// Its arguments.
  final TagContext args;

  /// The skeleton to settle with. Its references must already resolve against
  /// the store.
  final Object? skeleton;

  /// Resolved tags, for a payload that carries no response.
  final Iterable<String>? tags;

  /// The response, for a payload that does. Ignored when [tags] is present.
  final Object? response;

  /// Settle behind the server, so a mount refetches.
  final bool stale;

  /// When the query settled, from the payload. Null stamps it now.
  final int? settledTime;
}

/// What a request sequence rejects with when the cache was cleared under it:
/// the question it asked no longer has an answer.
final class RequestAbandoned implements Exception {
  /// Creates the error.
  const RequestAbandoned(this.key);

  /// The query's cache key.
  final String key;

  @override
  String toString() =>
      '[forge] request abandoned: the cache was cleared ($key)';
}

final class _Record implements TrackedRecord {
  _Record({
    required this.key,
    required this.meta,
    required this.args,
    required this.spec,
  });

  @override
  final String key;
  @override
  final OperationMeta meta;
  @override
  final TagContext args;
  final QuerySpec spec;

  /// Listener to its staleTime. A null staleTime is infinite.
  final Map<void Function(), Duration?> listeners =
      <void Function(), Duration?>{};

  Object? skeleton;
  @override
  bool settled = false;
  int settledTime = 0;
  @override
  QueryStatus status = .idle;
  @override
  Object? error;
  @override
  bool fetching = false;
  @override
  Future<Object?>? inflight;
  int run = 0;
  @override
  bool restart = false;
  @override
  int frameRestarts = 0;
  bool discard = false;
  Unmount? unmount;

  QueryState<Object?>? state;
  QueryStatus? stateStatus;
  Object? stateData;
  Object? stateError;
}

/// The query cache: the entity store, the tag graph and a transport, wired.
///
/// Running a query issues the request, normalizes the response into the
/// store, hands the registry the skeleton's dependencies, and returns the
/// rehydrated value. The cached thing is a skeleton of references, and reads
/// of it are memoized by the store, so a value keeps its identity for as long
/// as the entities under it do not move.
///
/// One request per query, not one per subscriber; and a response that
/// predates a write never settles the query it was for.
final class QueryCache {
  /// Creates a cache.
  ///
  /// [limit] bounds the queries remembered with nobody watching. [staleTime]
  /// is the default freshness window; null means results never go stale by
  /// time. [frameRestarts] bounds how often a request a stream frame overtook
  /// is re-run before it commits around the frames.
  ///
  /// Each of [syncSources] owns the entity types it names; two sources owning
  /// one type throw [ArgumentError]. With `storage` set, the cache opens the
  /// principal's [session] on every [setPrincipal] and closes it on the next.
  QueryCache({
    required this._transport,
    required this.entities,
    Scheduler? scheduler,
    CommitScheduler? commitScheduler,
    this._limit = 128,
    this._onError,
    this._frameRestarts = 3,
    this.clock = realClock,
    this._staleTime,
    List<SyncSource> syncSources = const [],
    this._storage,
  }) : commitScheduler = commitScheduler ?? microtaskCommitScheduler(),
       _syncSources = List.unmodifiable(syncSources) {
    overlays = OverlayStack(store, report);
    store.overlays = overlays;

    invalidator = Invalidator(
      registry,
      execute: _refetchAll,
      scheduler: scheduler,
      onError: _onError,
      onPlace: (entry, value) {
        observer?.call(QueryPlaced(key: entry.key));
        _adopt(entry, value);
      },
      onInvalidated: (entry, matched) {
        observer?.call(QueryInvalidated(key: entry.key, matched: matched));
        _stale(entry);
      },
    );

    _indexOwners();
  }

  /// The normalized entity store.
  final EntityStore store = EntityStore();

  /// The mounted-query registry and tag index.
  final QueryRegistry registry = QueryRegistry();

  /// The batched invalidator.
  late final Invalidator invalidator;

  /// Pending optimistic changes, layered over the store.
  late final OverlayStack overlays;

  /// The generated `entities` table this cache normalizes against.
  final EntitySchema entities;

  /// The stream runtime, when one was attached. Null for a REST-only app.
  LiveBinding? live;

  /// Where the cache reports what it did. Null in production.
  CacheObserver? observer;

  /// When stream commits run. Read by plan 01b's `StreamBinder`.
  final CommitScheduler commitScheduler;

  /// The injected clock. Read only on a settle and when a finite staleTime is
  /// in play.
  final Clock clock;

  final Transport _transport;
  final int _limit;
  final void Function(Object error, String context)? _onError;
  final int _frameRestarts;
  final Duration? _staleTime;

  final Map<String, _Record> _records = <String, _Record>{};
  int _runs = 0;
  final Map<int, int> _dispatches = <int, int>{};
  int _dispatchToken = 0;
  String? _principal;

  /// Bumped by every [clear], so a mutation can tell that the cache it was
  /// dispatched against is gone.
  int _generation = 0;
  final Set<void Function(String? principal)> _principals =
      <void Function(String? principal)>{};
  final Set<void Function(String? next)> _principalsChanging =
      <void Function(String? next)>{};
  final StreamController<String?> _principalChanges =
      StreamController<String?>.broadcast(sync: true);
  final StreamController<void> _commits = StreamController<void>.broadcast();
  final Map<MultiStreamController<QueryState<Object?>>, void Function()>
  _watchers = <MultiStreamController<QueryState<Object?>>, void Function()>{};
  bool _disposed = false;

  /// How many queries are tracked, watched or merely remembered.
  int get size => _records.length;

  /// Whether [dispose] has run. A disposed cache refuses new work.
  bool get isDisposed => _disposed;

  /// The cache key this operation and these arguments resolve to.
  String key(OperationMeta meta, TagContext args) => queryKey(meta, args);

  /// Watches a query, fetching it if there is nothing to show yet. The low
  /// level primitive [watch] is built on; the returned function unsubscribes.
  ///
  /// [listener] is called synchronously on every state transition. The
  /// registry sees one mount per query, not one per listener.
  void Function() subscribe(
    OperationMeta meta,
    TagContext args,
    void Function() listener, {
    Duration? staleTime,
  }) {
    _ensureOpen();

    final record = _open(meta, args);

    record.listeners[listener] = staleTime ?? meta.staleTime ?? _staleTime;

    if (record.listeners.length == 1) {
      record.unmount = registry.mount(record.spec);
    }

    if ((!record.settled || _expired(record)) && record.inflight == null) {
      _detach(_start(record));
    }

    var released = false;

    return () {
      if (released) return;

      released = true;
      record.listeners.remove(listener);

      if (record.listeners.isNotEmpty) return;

      record.unmount?.call();
      record.unmount = null;
      _reap();
    };
  }

  /// Watches a query as a stream of states.
  ///
  /// Each listen ref-counts the query and cancelling releases it. The first
  /// event is the current state; a Dart stream cannot deliver inside `listen`,
  /// so it arrives on the next microtask, ahead of every later event. Read
  /// [getState] for the synchronous value. Later events are delivered
  /// synchronously, and an unchanged state is never re-emitted. [live]
  /// subscribes the query to its stream channels for as long as it is watched.
  Stream<QueryState<Object?>> watch(
    OperationMeta meta,
    TagContext args, {
    bool live = false,
    Duration? staleTime,
  }) {
    _ensureOpen();

    return Stream<QueryState<Object?>>.multi((controller) {
      final record = _open(meta, args);
      QueryState<Object?>? last;
      var subscribing = true;

      void emit() {
        if (subscribing) return;

        final state = _snapshot(record);

        if (identical(state, last)) return;

        last = state;
        controller.addSync(state);
      }

      final release = subscribe(meta, args, emit, staleTime: staleTime);
      final stopLive = live ? watchLive(meta, args) : null;

      subscribing = false;
      emit();

      var cancelled = false;

      void cancel() {
        // Dispose cancels, then closes the controller, and closing delivers
        // done, which cancels the subscription and runs this a second time.
        if (cancelled) return;

        cancelled = true;
        release();
        stopLive?.call();
        _watchers.remove(controller);
      }

      _watchers[controller] = cancel;
      controller.onCancel = cancel;
    });
  }

  /// This query's current state, opening its record if it is new. Stable
  /// across reads while nothing changes.
  QueryState<Object?> getState(OperationMeta meta, TagContext args) =>
      _snapshot(_open(meta, args));

  /// This query's state without opening a record or moving the LRU order.
  /// Null means nothing is cached.
  QueryState<Object?>? peek(OperationMeta meta, TagContext args) {
    final record = _records[key(meta, args)];

    return record == null ? null : _snapshot(record);
  }

  /// When this query last settled, on [clock], or null if it never has.
  int? settledTimeOf(OperationMeta meta, TagContext args) {
    final record = _records[key(meta, args)];

    return record != null && record.settled ? record.settledTime : null;
  }

  /// The staleTime governing this query now: the strictest live subscriber's,
  /// else the operation's, else the cache default. Null when no record exists
  /// or nothing bounds it.
  Duration? effectiveStaleTime(OperationMeta meta, TagContext args) {
    final record = _records[key(meta, args)];

    return record == null ? null : _staleTimeOf(record);
  }

  Duration? _staleTimeOf(_Record record) {
    if (record.listeners.isEmpty) return record.meta.staleTime ?? _staleTime;

    Duration? strictest;

    for (final value in record.listeners.values) {
      if (value != null && (strictest == null || value < strictest)) {
        strictest = value;
      }
    }

    return strictest;
  }

  /// Whether time has made this record stale. The clock is read only when a
  /// finite staleTime is in play.
  bool _expired(_Record record) {
    final staleTime = _staleTimeOf(record);

    return staleTime != null &&
        clock.now() - record.settledTime > staleTime.inMilliseconds;
  }

  /// Every query this cache is tracking, for an inspector. No copy, no open.
  Iterable<TrackedRecord> tracked() => _records.values;

  /// Every query that settled successfully, as `dehydrate` reads them.
  Iterable<CachedQuery> get queries => [
    for (final record in _records.values)
      if (record.settled && record.status == .success)
        CachedQuery(
          key: record.key,
          meta: record.meta,
          args: key(record.meta, TagContext.empty) == record.key
              ? TagContext.empty
              : record.args,
          skeleton: record.skeleton,
          settledTime: record.settledTime,
        ),
  ];

  /// Settles a query from a skeleton, with no request behind it: the seam
  /// hydrate writes through. Merges rather than replaces, so restoring the same
  /// payload twice moves no version and changes no identity.
  void restore(RestoreInput input) {
    final record = _open(input.meta, input.args);

    record.skeleton = input.skeleton;
    record.settled = true;
    record.settledTime = input.settledTime ?? clock.now();
    record.status = .success;
    record.error = null;
    record.fetching = false;

    registry.settle(
      record.key,
      SettleResult.withValue(
        _base(record),
        deps: store.dependencies(input.skeleton),
        tags: input.tags,
        response: input.response,
      ),
    );

    final entry = registry.get(record.key);

    if (input.stale && entry != null) registry.markStale(entry);

    _notify(record);
    _committed();
  }

  /// Runs this query or joins the request already running for it. Serves the
  /// cached value without a request when the query has settled, nothing has
  /// invalidated it and it has not aged past its staleTime.
  Future<Object?> fetch(
    OperationMeta meta,
    TagContext args, {
    RequestOptions options = const RequestOptions(),
  }) {
    _ensureOpen();

    final record = _open(meta, args);
    final entry = registry.get(record.key);

    if (record.settled &&
        record.inflight == null &&
        !(entry?.stale ?? false) &&
        !_expired(record)) {
      return Future<Object?>.value(_value(record));
    }

    return _start(record, options);
  }

  /// Runs this query again whatever the cache holds.
  Future<Object?> refetch(
    OperationMeta meta,
    TagContext args, {
    RequestOptions options = const RequestOptions(),
  }) {
    _ensureOpen();

    return _restart(_open(meta, args), options);
  }

  /// Runs a mutation, commits what it returned, and invalidates what it
  /// declared. The response is normalized first, so placement callbacks and
  /// refetched queries see the mutated entity.
  ///
  /// A response a stream frame overtook is never re-issued: it commits around
  /// the raced entities instead, because re-sending a write is how duplicate
  /// orders happen.
  ///
  /// A response that lands after [clear] or [setPrincipal] emptied the cache
  /// belongs to the data that was dropped. It is not committed, promoted,
  /// placed or invalidated, and the call still completes with the decoded
  /// response rather than throwing: the server applied the write, and a
  /// failure here could lead an outbox to send it again under the new
  /// principal.
  ///
  /// A mutation on an entity a sync source owns never reaches the transport:
  /// it goes to the source's `apply`. An `Applied` outcome returns its
  /// response and invalidates `meta.invalidates`, `Queued` returns null, and
  /// `Rejected` throws its error. With no source running for the principal it
  /// throws [StateError]. On that path [MutateOptions.place],
  /// [RequestOptions.cancel] and [RequestOptions.headers] have no effect and
  /// are ignored rather than refused: the source decides how, and when, the
  /// write is sent.
  Future<Object?> mutate(
    OperationMeta meta,
    TagContext args, {
    MutateOptions options = const MutateOptions(),
  }) async {
    _ensureOpen();

    final owner = _owners[meta.entity];

    if (owner != null) return _mutateThroughSource(owner, meta, args, options);

    final overlay = _push(meta, args, options);
    final dispatchedAt = store.frameVersion;
    final token = _dispatched(dispatchedAt);
    final generation = _generation;

    Object? response;

    try {
      response = await _transport.execute(
        TransportRequest(
          meta: meta,
          args: args,
          headers: options.headers,
          cancel: options.cancel,
        ),
      );
    } on Object catch (error, stack) {
      _landed(token);

      // Base was never touched, so nothing is owed: no tags, no refetch.
      if (overlay != null) {
        overlays.take(overlay);
        _refresh(notify: true);
      }

      Error.throwWithStackTrace(error, stack);
    }

    if (generation != _generation) {
      _landed(token);

      // The clear already dropped every overlay; this only guards the rule
      // that this mutation's optimism never comes back.
      if (overlay != null && overlays.take(overlay) != null) {
        _refresh(notify: true);
      }

      return response;
    }

    final staged = store.stage(response, entities, _rootTypeOf(meta));
    final skip = store.racedSince(staged.records.keys, dispatchedAt).toSet();

    // Taken before it is promoted, so a computed merge is evaluated against
    // base alone.
    if (overlay != null) {
      final entry = overlays.take(overlay);

      if (entry != null) {
        final overtaken = store
            .racedSince(entry.patches.keys, dispatchedAt)
            .toSet();

        // An owned record's patch is discarded like an overtaken one: the
        // source, not this response, decides what the record holds.
        skip.addAll(
          overlays.promote(
            entry,
            _withOwned(overtaken, entry.patches.keys) ?? overtaken,
          ),
        );
      }
    }

    store.commit(
      staged,
      CommitOptions(
        skip: _withOwned(skip.isEmpty ? null : skip, staged.records.keys),
      ),
    );
    _committed();

    // A raced key the store no longer holds is a delete: hand back what the
    // server said and decline placement, which would resurrect it.
    final buried = skip.any((entityKey) => !store.has(entityKey));
    final created = buried ? response : store.read(staged.skeleton);

    _landed(token);

    // Refresh and notify: a response commits entities no invalidated tag
    // reaches, and this is the only chance their subscribers get.
    _refresh(notify: true);

    observer?.call(
      MutationCommitted(meta: meta, args: args, response: response),
    );

    invalidator.settled(
      MutationSettled(
        invalidates: meta.invalidates,
        args: args,
        response: response,
        created: created,
        place: buried || options.place.isEmpty ? null : options.place,
      ),
    );

    return created;
  }

  /// Pushes this mutation's declared change. Everything application code can
  /// reach from here is guarded: this runs before dispatch, and a throw would
  /// silently stop the write.
  int? _push(OperationMeta meta, TagContext args, MutateOptions options) {
    final spec = options.optimistic;

    if (spec == null) return null;

    ResolvedPatches? resolved;

    try {
      resolved = specToPatches(
        spec,
        meta,
        args,
        entities,
        overlays.mint,
        _safeReport,
      );
    } on Object catch (error) {
      // A typed spec handed straight to the cache fails its covariance check
      // here. Not being optimistic is the smaller failure.
      _safeReport(error, 'optimistic');

      return null;
    }

    if (resolved == null) return null;

    final tags = resolveTags(meta.invalidates, args).tags;
    final id = overlays.add(
      resolved.patches,
      options.place.isEmpty ? null : options.place,
      tags,
      resolved.created,
    );

    try {
      _refresh(notify: true);
    } on Object catch (error) {
      _safeReport(error, 'optimistic');
    }

    return id;
  }

  void _safeReport(Object error, String context) {
    try {
      report(error, context);
    } on Object {
      // Nowhere further to send it that would not risk the same failure.
    }
  }

  /// Subscribes this query to the channels its entities are pushed on and
  /// returns the release: the whole of what `live: true` does. With no stream
  /// runtime attached it reports through `onError` (context `live`) and
  /// returns a no-op, exactly as TypeScript does.
  void Function() watchLive(OperationMeta meta, TagContext args) {
    final binding = live;

    if (binding == null) {
      report(
        StateError(
          '[forge] live: no stream runtime attached for ${operationName(meta)}',
        ),
        'live',
      );

      return () {};
    }

    return binding.subscribe(meta, args);
  }

  /// Invalidates already-resolved tags, as a stream frame or a manual refresh
  /// would.
  void invalidate(Iterable<String> tags) => invalidator.invalidate(tags);

  /// Marks one query stale: its in-flight answer is discarded and, when it is
  /// watched, it refetches in the next batch.
  void invalidateQuery(OperationMeta meta, TagContext args) {
    final entry = registry.get(key(meta, args));

    if (entry == null) return;

    observer?.call(QueryInvalidated(key: entry.key, matched: const {}));
    _stale(entry);
    registry.markStale(entry);
  }

  /// Something wrote to the store behind this cache's back: re-read every
  /// tracked query and notify the ones whose value moved. The seam the stream
  /// layer commits through.
  void notifyChanged() {
    _refresh(notify: true);
    _committed();
  }

  int _dispatched(int at) {
    final token = ++_dispatchToken;

    _dispatches[token] = at;

    return token;
  }

  void _landed(int token) {
    _dispatches.remove(token);

    int? oldest;

    for (final at in _dispatches.values) {
      if (oldest == null || at < oldest) oldest = at;
    }

    store.expireTombstones(oldest);
  }

  /// Reports a failure through the cache's own error channel.
  void report(Object error, String context) => _onError?.call(error, context);

  /// Who the cached data belongs to.
  String? get principal => _principal;

  /// Which life of the cache this is. [clear], and so every [setPrincipal]
  /// that changes the principal, starts a new one. A writer that captured it
  /// before an await or a batching delay and sees it differ afterwards holds
  /// data that belonged to what was dropped, and must not commit it.
  int get generation => _generation;

  /// Every identity change, after the cache has been emptied. Synchronous.
  Stream<String?> get principalChanges => _principalChanges.stream;

  /// Calls [listener] on every identity change, after the cache has been
  /// emptied. A throwing listener is reported with the context `principal`.
  void Function() watchPrincipal(void Function(String? principal) listener) {
    _principals.add(listener);

    return () => _principals.remove(listener);
  }

  /// Calls [listener] on every identity change, synchronously, before the
  /// cache is emptied: after [principal] already returns the next principal,
  /// and before the clear notifies any watcher and before any [watchPrincipal]
  /// listener or [principalChanges] event. An adapter uses it to stop showing
  /// the previous principal's data before anything can observe the change.
  ///
  /// The cache still holds the previous principal's records while listeners
  /// run, so a listener must not read or write the store, nor call back into
  /// the cache. A throwing listener is reported with the context `principal`.
  /// Returns a function that removes [listener]. [dispose] forgets every
  /// listener.
  void Function() watchPrincipalChanging(
    void Function(String? next) listener,
  ) {
    _principalsChanging.add(listener);

    return () => _principalsChanging.remove(listener);
  }

  /// Declares who the cached data belongs to, dropping everything on a
  /// change. Watched queries are re-mounted and refetched; their in-flight
  /// requests are abandoned. Every running sync source's context is fenced
  /// before anything is dropped; the sources themselves stop in the
  /// background, and [idle] completes once they have.
  void setPrincipal(String? principal) {
    if (principal == _principal) return;

    _principal = principal;

    // Before the clear, synchronously: from here on the store is the next
    // principal's, and a write the old sources make while they stop must not
    // land in it.
    _deactivateContexts();

    // Before the clear, whose notifications would otherwise carry the old
    // principal's sync status to the new principal's watchers.
    _detachStatuses();

    // Before the clear, whose notifications are the first thing a watcher
    // sees of the new principal; see watchPrincipalChanging.
    for (final listener in _principalsChanging.toList()) {
      try {
        listener(principal);
      } on Object catch (error) {
        _onError?.call(error, 'principal');
      }
    }

    _clear(keepOwned: false);
    _scheduleSync(principal);

    for (final listener in _principals.toList()) {
      try {
        listener(principal);
      } on Object catch (error) {
        _onError?.call(error, 'principal');
      }
    }

    if (!_principalChanges.isClosed) _principalChanges.add(principal);
  }

  // Storage and sync, owned per principal. With storage set, the cache opens
  // the principal's session, and nothing else ever does. Records of an owned
  // entity come from its source, never from a REST response or a stream
  // frame, and its mutations go to the source.
  final List<SyncSource> _syncSources;
  final StorageAdapter? _storage;
  final Map<String, SyncSource> _owners = <String, SyncSource>{};
  final Map<String, SyncStatus> _entityStatus = <String, SyncStatus>{};
  final List<StreamSubscription<SyncStatus>> _statusSubscriptions =
      <StreamSubscription<SyncStatus>>[];

  /// Sources whose start returned, in start order. The next transition stops
  /// exactly these; a source whose start threw is not running.
  final Set<SyncSource> _running = Set<SyncSource>.identity();

  /// The contexts handed to the current principal's sources, still active.
  final List<SyncContext> _contexts = <SyncContext>[];
  final StreamController<StorageSession?> _sessionChanges =
      StreamController<StorageSession?>.broadcast(sync: true);
  StorageSession? _session;
  Future<void> _syncTransition = Future<void>.value();

  /// Bumped by every queued transition, so a transition, or a mutation waiting
  /// on one, can tell that a later principal change superseded it.
  int _syncGeneration = 0;

  static const Uuid _uuid = Uuid();

  /// The open storage session for the current principal. Null without
  /// storage, without a principal, or while a switch is in progress.
  StorageSession? get session => _session;

  /// A session each time one opens, and null each time one closes.
  Stream<StorageSession?> get sessionChanges => _sessionChanges.stream;

  /// Completes once every queued principal transition has finished: the old
  /// sources stopped, the old session closed and, for a principal, the next
  /// session opened and its sources started. A transition queued while
  /// waiting is waited for too. Never fails.
  ///
  /// Sign-out with erase awaits it before destroying the old partition, so
  /// nothing still holds or writes it:
  ///
  /// ```dart
  /// cache.setPrincipal(null);
  /// await cache.idle;
  /// await storage.destroy(previous);
  /// ```
  Future<void> get idle async {
    while (true) {
      final transition = _syncTransition;

      await transition;

      if (identical(transition, _syncTransition)) return;
    }
  }

  /// Fences every context the current principal's sources hold. Synchronous,
  /// so it takes effect before the caller empties the store.
  void _deactivateContexts() {
    for (final context in _contexts) {
      deactivateSyncContext(context);
    }

    _contexts.clear();
  }

  void _indexOwners() {
    for (final source in _syncSources) {
      for (final entity in source.entities) {
        if (_owners.containsKey(entity)) {
          throw ArgumentError.value(
            entity,
            'syncSources',
            'is owned by more than one sync source',
          );
        }

        _owners[entity] = source;
      }
    }
  }

  /// Whether a registered sync source owns [typename].
  bool owns(String typename) => _owners.containsKey(typename);

  /// [skip] plus every owned key among [keys], or [skip] itself when no
  /// source owns any of them.
  Set<EntityKey>? _withOwned(Set<EntityKey>? skip, Iterable<EntityKey> keys) =>
      _owners.isEmpty ? skip : withOwned(owns, skip, keys);

  /// Every owned key the store holds.
  List<EntityKey> _ownedKeys() => _owners.isEmpty
      ? const []
      : [
          for (final key in store.keys)
            if (owns(typenameOf(key))) key,
        ];

  /// Drops every owned record. Run between principals, once the old sources
  /// stopped: whatever an owned row holds then, the old principal's source
  /// wrote it around the [SyncContext.write] fence, after the clear that
  /// emptied the cache for the next. The backstop, not the guard: the fence
  /// already dropped every write made through the seam.
  void _evictOwned() {
    final owned = _ownedKeys();

    if (owned.isEmpty) return;

    for (final key in owned) {
      store.evict(key);
    }

    if (!_disposed) notifyChanged();
  }

  /// Drops the records of [source]'s entities, after its start threw: what it
  /// projected before failing belongs to a source that is not running.
  void _evictOwnedBy(SyncSource source) {
    final owned = [
      for (final key in store.keys)
        if (source.entities.contains(typenameOf(key))) key,
    ];

    if (owned.isEmpty) return;

    for (final key in owned) {
      store.evict(key);
    }

    if (!_disposed) notifyChanged();
  }

  /// The fold across the owned entities among [meta]'s entity and [deps].
  SyncStatus _syncStatusFor(OperationMeta meta, Iterable<EntityKey> deps) {
    if (_owners.isEmpty) return const Synced();

    final touched = <String>{
      ?meta.entity,
      for (final key in deps) typenameOf(key),
    };

    return foldSyncStatus([
      for (final entity in touched)
        if (_owners.containsKey(entity))
          _entityStatus[entity] ?? const Synced(),
    ]);
  }

  /// Stops listening to the running sources' statuses at once. What a source
  /// says after this is about a principal the cache no longer serves.
  void _detachStatuses() {
    if (_statusSubscriptions.isEmpty && _entityStatus.isEmpty) return;

    for (final subscription in _statusSubscriptions) {
      unawaited(
        subscription.cancel().catchError(
          (Object error) => _safeReport(error, 'sync'),
        ),
      );
    }

    _statusSubscriptions.clear();
    _entityStatus.clear();
  }

  void _listenTo(SyncSource source) {
    for (final entity in source.entities) {
      try {
        _statusSubscriptions.add(
          source.status(entity).listen((status) {
            if (_entityStatus[entity] == status) return;

            _entityStatus[entity] = status;

            if (!_disposed) _refresh(notify: true);
          }, onError: (Object error) => _safeReport(error, 'sync')),
        );
      } on Object catch (error) {
        _safeReport(error, 'sync');
      }
    }
  }

  /// Queues a lifecycle change. The current session is detached at once, so
  /// nothing told about the new principal can reach the old principal's
  /// storage; the transition closes it after the sources stopped.
  /// Transitions run one at a time.
  void _scheduleSync(String? principal) {
    if (_syncSources.isEmpty && _storage == null) return;

    _detachStatuses();

    final previous = _session;

    if (previous != null) {
      _session = null;

      if (!_sessionChanges.isClosed) _sessionChanges.add(null);
    }

    final generation = ++_syncGeneration;

    _syncTransition = _syncTransition.then(
      (_) => _switchSources(generation, principal, previous),
    );
  }

  /// One transition. Never throws: a failure is reported (context `sync` for a
  /// source, `storage` for the session) and the transition carries on, since
  /// a broken chain would leave every later session open.
  Future<void> _switchSources(
    int generation,
    String? principal,
    StorageSession? previous,
  ) async {
    final running = _running.toList();

    _running.clear();

    for (final source in running) {
      try {
        await source.stop();
      } on Object catch (error) {
        _safeReport(error, 'sync');
      }
    }

    if (previous != null) {
      try {
        await previous.close();
      } on Object catch (error) {
        _safeReport(error, 'storage');
      }
    }

    // Every transition changes the principal, and the old sources are stopped
    // now: an owned row still here is theirs, written after the clear.
    _evictOwned();

    // A later principal change superseded this one, or nobody is signed in.
    if (generation != _syncGeneration || principal == null || _disposed) {
      return;
    }

    final storage = _storage;
    StorageSession? opened;

    if (storage != null) {
      try {
        opened = await storage.open(principal);
      } on Object catch (error) {
        // Without the principal's storage nothing may run for them.
        _safeReport(error, 'storage');

        return;
      }

      if (generation != _syncGeneration || _disposed) {
        try {
          await opened.close();
        } on Object catch (error) {
          _safeReport(error, 'storage');
        }

        return;
      }

      // Recorded before any source starts, so a source that fails to start
      // leaves the session where the next transition closes it.
      _session = opened;

      if (!_sessionChanges.isClosed) _sessionChanges.add(opened);
    }

    for (final source in _syncSources) {
      // A source that failed below leaves the loop here when a principal
      // change arrived while it was being stopped.
      if (generation != _syncGeneration || _disposed) return;

      // One per source, so a source that fails to start is fenced alone.
      final context = SyncContext(
        cache: this,
        principal: principal,
        transport: _transport,
        storage: opened,
      );

      _contexts.add(context);

      try {
        await source.start(context);
      } on Object catch (error) {
        _safeReport(error, 'sync');

        // It may have got part of the way. Nothing it writes from now on
        // lands, stop is idempotent by contract, and what it projected before
        // it threw goes.
        deactivateSyncContext(context);
        _contexts.remove(context);

        try {
          await source.stop();
        } on Object catch (error) {
          _safeReport(error, 'sync');
        }

        _evictOwnedBy(source);

        continue;
      }

      // Running even when superseded below: the next transition stops it.
      _running.add(source);

      if (generation != _syncGeneration || _disposed) return;

      _listenTo(source);
    }
  }

  Future<Object?> _mutateThroughSource(
    SyncSource source,
    OperationMeta meta,
    TagContext args,
    MutateOptions options,
  ) async {
    final lifecycle = _syncGeneration;
    final generation = _generation;

    await _syncTransition;

    // The principal it was made under is gone; its source may be someone
    // else's by now. Nothing was applied, so nothing is owed.
    if (lifecycle != _syncGeneration) {
      throw StateError(
        '[forge] the principal changed before this ${meta.entity} mutation '
        'reached its sync source; it was not applied',
      );
    }

    if (!_running.contains(source)) {
      throw StateError(
        '[forge] ${meta.entity} is owned by a sync source that is not running; '
        'set a principal first',
      );
    }

    final outcome = await source.apply(
      PendingMutation(
        id: _uuid.v4(),
        meta: meta,
        args: args,
        optimistic: options.optimistic,
        idempotencyKey: _uuid.v4(),
        createdAt: DateTime.now(),
      ),
    );

    switch (outcome) {
      case Applied(:final response):
        // The same rule as a REST response that lands after a clear: the
        // caller gets the answer, the emptied cache gets nothing.
        if (generation == _generation) {
          observer?.call(
            MutationCommitted(meta: meta, args: args, response: response),
          );
          invalidator.settled(
            MutationSettled(
              invalidates: meta.invalidates,
              args: args,
              response: response,
            ),
          );
        }

        return response;
      case Queued():
        return null;
      case Rejected(:final error):
        throw error;
    }
  }

  /// Drops every entity, every skeleton and every registry entry. Watched
  /// queries are reset in place and refetched.
  ///
  /// A sync source's records survive: the principal has not changed, the
  /// source is still running and will not project them again, and a REST
  /// refetch never writes them. [setPrincipal] drops them with everything
  /// else.
  void clear() => _clear(keepOwned: true);

  void _clear({required bool keepOwned}) {
    _generation++;

    final tracked = _records.values.toList();

    // Every request out is abandoned first: a sequence drops a response whose
    // record no longer holds its run.
    for (final record in tracked) {
      record.inflight = null;
      record.run = 0;
    }

    final watched = tracked
        .where((record) => record.listeners.isNotEmpty)
        .toList();

    final kept = keepOwned
        ? {for (final key in _ownedKeys()) key: store.getRecord(key)!.data}
        : const <EntityKey, Json>{};

    overlays.clear();
    store.clear();

    for (final MapEntry(:key, value: data) in kept.entries) {
      store.put(key, data);
    }

    registry.clear();
    _records.clear();

    for (final record in watched) {
      _reset(record);
      record.discard = false;

      _records[record.key] = record;
      record.unmount = registry.mount(record.spec);

      _notify(record);
      _detach(_start(record));
    }

    _committed();
  }

  void _reset(_Record record) {
    record.skeleton = null;
    record.settled = false;
    record.status = .pending;
    record.error = null;
    record.fetching = false;
    record.restart = false;
    record.frameRestarts = 0;
    record.state = null;
  }

  /// Forgets one query, or resets and refetches it when somebody is watching.
  /// Returns false when nothing was tracking it.
  bool drop(OperationMeta meta, TagContext args) => dropKey(key(meta, args));

  /// [drop] by cache key. The TypeScript `drop(key)`.
  bool dropKey(String cacheKey) {
    final record = _records[cacheKey];

    if (record == null) return false;

    record.inflight = null;
    record.run = 0;

    if (record.listeners.isEmpty) {
      _records.remove(cacheKey);
      registry.drop(cacheKey);
      collect();

      return true;
    }

    _reset(record);
    _detach(_start(record));

    return true;
  }

  Object? _discardInflight(_Record record) {
    record.discard = false;
    record.restart = false;
    record.inflight = null;
    record.fetching = false;

    _notify(record);

    return _value(record);
  }

  _Record _open(OperationMeta meta, TagContext args) {
    final cacheKey = key(meta, args);
    final existing = _records.remove(cacheKey);

    if (existing != null) {
      // Re-inserted at the end: `_reap` evicts from the front.
      _records[cacheKey] = existing;

      return existing;
    }

    final spec = QuerySpec(
      operation: operationName(meta),
      args: args,
      provides: meta.provides,
      key: cacheKey,
    );
    final record = _Record(key: cacheKey, meta: meta, args: args, spec: spec);

    // Before the insert, so a cap with every other record watched does not
    // evict the one just asked for.
    _reap();
    _records[cacheKey] = record;

    // Create the registry entry and release it at once, so a query fetched
    // before it is watched still has somewhere to record its dependencies.
    registry.mount(spec)();

    return record;
  }

  Future<Object?> _start(
    _Record record, [
    RequestOptions options = const RequestOptions(),
  ]) {
    final running = record.inflight;

    if (running != null) return running;

    record.fetching = true;

    if (!record.settled) record.status = .pending;

    final run = ++_runs;
    record.run = run;

    Future<Object?> sequence() async {
      record.frameRestarts = 0;

      while (true) {
        record.restart = false;
        record.discard = false;

        // Two readings of two clocks for two races: the frame version answers
        // "did a frame overtake this response", the registry stamp answers
        // "was this query invalidated while the request was out".
        final dispatchedAt = store.frameVersion;
        final startedAt = registry.stamp;
        final token = _dispatched(dispatchedAt);

        try {
          Object? response;

          try {
            response = await _transport.execute(
              TransportRequest(
                meta: record.meta,
                args: record.args,
                headers: options.headers,
                cancel: options.cancel,
              ),
            );
          } on Object catch (error, stack) {
            if (record.run != run) throw RequestAbandoned(record.key);
            if (record.discard) return _discardInflight(record);
            if (record.restart) continue;

            record.inflight = null;
            _fail(record, error);

            Error.throwWithStackTrace(error, stack);
          }

          if (record.run != run) throw RequestAbandoned(record.key);
          if (record.discard) return _discardInflight(record);
          if (record.restart) continue;

          final staged = store.stage(
            response,
            entities,
            _rootTypeOf(record.meta),
          );
          final raced = store.racedSince(staged.records.keys, dispatchedAt);

          if (raced.isNotEmpty && record.frameRestarts < _frameRestarts) {
            record.frameRestarts++;

            // Keep the siblings: only the raced keys are stale.
            store.commit(
              staged,
              CommitOptions(
                skip: _withOwned(raced.toSet(), staged.records.keys),
              ),
            );
            _committed();
            _refresh(notify: true);

            continue;
          }

          record.inflight = null;

          return _settle(
            record,
            response,
            staged,
            startedAt,
            raced.isEmpty ? null : raced.toSet(),
          );
        } finally {
          _landed(token);
        }
      }
    }

    // Deferred by a microtask, so a transport that throws synchronously cannot
    // run the catch before `inflight` is assigned.
    final future = Future<Object?>.microtask(sequence);

    record.inflight = future;
    _notify(record);

    return future;
  }

  Future<Object?> _restart(
    _Record record, [
    RequestOptions options = const RequestOptions(),
  ]) {
    final running = record.inflight;

    if (running == null) return _start(record, options);

    record.discard = false;
    record.restart = true;

    return running;
  }

  /// The only place a request in flight learns it answers a changed question.
  void _stale(QueryEntry entry) {
    final record = _records[entry.key];

    if (record == null || record.inflight == null) return;

    record.discard = false;
    record.restart = true;
  }

  void _refetchAll(List<QueryEntry> batch) {
    for (final entry in batch) {
      // Queued before a clear, which dropped the registry entry it names: the
      // invalidation was about data that is gone, and the record now under
      // this key belongs to the cache's next life, perhaps the next principal.
      if (!identical(registry.get(entry.key), entry)) continue;

      final record = _records[entry.key];

      if (record != null && record.inflight == null) _detach(_start(record));
    }
  }

  Object? _settle(
    _Record record,
    Object? response,
    StagedWrite staged,
    int startedAt,
    Set<EntityKey>? skip,
  ) {
    store.commit(
      staged,
      CommitOptions(skip: _withOwned(skip, staged.records.keys)),
    );
    _committed();

    record.skeleton = staged.skeleton;
    record.settled = true;
    record.settledTime = clock.now();
    record.status = .success;
    record.error = null;
    record.fetching = false;

    // Deps from the live store, collected by the same read that keeps the
    // container identity.
    final deps = <EntityKey>{};
    final base = _base(record, deps);
    final entry = registry.get(record.key);
    final value = overlays.empty
        ? base
        : overlays.project(record.key, base, entry);

    // `base`, not `value`: what a placement callback is handed must be
    // entity-plane only.
    registry.settle(
      record.key,
      SettleResult.withValue(
        base,
        deps: deps,
        response: response,
        startedAt: startedAt,
      ),
    );
    _notify(record);

    return value;
  }

  void _fail(_Record record, Object error) {
    record.status = .error;
    record.error = error;
    record.fetching = false;

    _notify(record);
    _onError?.call(error, 'fetch');
  }

  void _adopt(QueryEntry entry, List<Object?> value) {
    final record = _records[entry.key];

    if (record == null) return;

    // `entity`, not the root type: a placement returns a list of records.
    final staged = store.stage(value, entities, record.meta.entity);

    // Skip every overlaid key, so another mutation's pending value cannot
    // ride a callback's result into base.
    store.commit(
      staged,
      CommitOptions(
        skip: _withOwned(
          overlays.empty ? null : overlays.keys(),
          staged.records.keys,
        ),
      ),
    );
    _committed();

    record.skeleton = staged.skeleton;
    record.settled = true;
    record.settledTime = clock.now();
    record.status = .success;
    record.error = null;
    record.fetching = false;

    if (record.inflight != null) {
      record.restart = false;
      record.discard = true;
    }

    final deps = <EntityKey>{};

    _base(record, deps);
    registry.adopt(record.key, deps);

    _notify(record);
  }

  /// The value, with the registry's copy refreshed to match. The previous
  /// read is offered to the store so an unchanged refetch keeps identity.
  Object? _base(_Record record, [Set<EntityKey>? collect]) {
    final entry = registry.get(record.key);
    final value = record.settled
        ? store.read(record.skeleton, entry?.value, collect)
        : null;

    if (entry != null) entry.value = value;

    return value;
  }

  Object? _value(_Record record) {
    final base = _base(record);

    if (overlays.empty) return base;

    return overlays.project(record.key, base, registry.get(record.key));
  }

  QueryState<Object?> _snapshot(_Record record) {
    final data = _value(record);
    final entry = registry.get(record.key);
    final optimistic = overlays.affects(entry);
    final sync = _syncStatusFor(record.meta, entry?.deps ?? const {});
    final previous = record.state;

    // `==` for the sync status: the fold builds a fresh Pending on every read,
    // and an equal status must not cost the state its identity.
    if (previous != null &&
        sameValue(record.stateData, data) &&
        record.stateStatus == record.status &&
        identical(record.stateError, record.error) &&
        previous.isFetching == record.fetching &&
        previous.isOptimistic == optimistic &&
        previous.syncStatus == sync) {
      return previous;
    }

    final QueryState<Object?> next = switch (record.status) {
      .idle => QueryIdle(
        isFetching: record.fetching,
        isOptimistic: optimistic,
        syncStatus: sync,
      ),
      .pending => QueryLoading(
        isFetching: record.fetching,
        isOptimistic: optimistic,
        syncStatus: sync,
      ),
      .success => QuerySuccess(
        data,
        isFetching: record.fetching,
        isOptimistic: optimistic,
        syncStatus: sync,
      ),
      .error => QueryFailure(
        record.error!,
        previous: data,
        isFetching: record.fetching,
        isOptimistic: optimistic,
        syncStatus: sync,
      ),
    };

    record.state = next;
    record.stateStatus = record.status;
    record.stateData = data;
    record.stateError = record.error;

    return next;
  }

  void _notify(_Record record) {
    observer?.call(
      QueryTransition(
        key: record.key,
        status: record.status,
        fetching: record.fetching,
      ),
    );

    for (final listener in record.listeners.keys.toList()) {
      listener();
    }
  }

  void _refresh({required bool notify}) {
    for (final record in _records.values.toList()) {
      final before = record.state;
      final after = _snapshot(record);

      if (notify && !identical(before, after)) _notify(record);
    }
  }

  /// Forgets the least recently used queries nobody is watching, past
  /// [_limit]. A watched or in-flight query is never evicted.
  void _reap() {
    if (_records.length <= _limit) return;

    var reaped = false;

    for (final MapEntry(key: cacheKey, value: record)
        in _records.entries.toList()) {
      if (_records.length <= _limit) break;
      if (record.listeners.isNotEmpty || record.inflight != null) continue;

      _records.remove(cacheKey);
      registry.drop(cacheKey);
      reaped = true;
    }

    if (reaped) collect();
  }

  /// Drops every entity no cached query and no pending overlay can reach.
  /// Returns how many records were dropped.
  int collect() {
    final reachable = overlays.keys();

    for (final record in _records.values) {
      if (!record.settled) continue;

      reachable.addAll(store.dependencies(record.skeleton));
    }

    // An owned record lives as long as its source keeps it, reachable or
    // not: nothing else would ever write it back.
    final garbage = [
      for (final entityKey in store.keys)
        if (!reachable.contains(entityKey) &&
            !(_owners.isNotEmpty && owns(typenameOf(entityKey))))
          entityKey,
    ];

    // No frame stamp, so no tombstone: the record is merely unreferenced.
    for (final entityKey in garbage) {
      store.evict(entityKey);
    }

    if (garbage.isNotEmpty) _committed();

    return garbage.length;
  }

  /// Refetches every watched, settled query that is not already in flight,
  /// and returns how many it started. With [onlyStale] true (the default) only
  /// queries that aged past their staleTime are refetched. Driven by the
  /// freshness installers.
  int revalidate({bool onlyStale = true}) {
    var started = 0;

    for (final record in _records.values.toList()) {
      if (record.listeners.isEmpty || !record.settled) continue;
      if (record.inflight != null) continue;
      if (onlyStale && !_expired(record)) continue;

      _detach(_start(record));
      started++;
    }

    return started;
  }

  /// Fires after every store commit. Persistence listens here. Asynchronous,
  /// so a burst of commits costs listeners nothing until they run.
  Stream<void> get commits => _commits.stream;

  void _committed() {
    if (_commits.hasListener && !_commits.isClosed) _commits.add(null);
  }

  /// Abandons every request in flight, closes every [watch] stream, stops
  /// every sync source, closes the [session], and closes the
  /// [principalChanges], [commits] and [sessionChanges] streams. The cache is
  /// unusable after.
  Future<void> dispose() async {
    if (_disposed) return;

    _disposed = true;
    _principalsChanging.clear();

    // Synchronously, like the rest of this teardown: no source writes into a
    // disposed cache, however long its stop takes.
    _deactivateContexts();

    for (final record in _records.values) {
      record.inflight = null;
      record.run = 0;
    }

    for (final MapEntry(key: controller, value: cancel)
        in _watchers.entries.toList()) {
      cancel();
      controller.closeSync();
    }

    // After the synchronous teardown above, which a caller that does not
    // await dispose still relies on.
    if (_syncSources.isNotEmpty || _storage != null) {
      _scheduleSync(null);
      await _syncTransition;
    }

    await _principalChanges.close();
    await _commits.close();
    await _sessionChanges.close();
  }

  void _ensureOpen() {
    if (_disposed) throw StateError('[forge] this QueryCache was disposed');
  }

  /// Consumes a future nobody awaited, so a refetch failure the subscriber
  /// already sees in its state is not also an unhandled error.
  void _detach(Future<Object?> future) {
    unawaited(future.then<void>((_) {}, onError: (Object _, StackTrace _) {}));
  }
}

/// The typename to normalize an operation's response against: `rootType`,
/// falling back to `entity` for a manifest generated before it existed.
String? _rootTypeOf(OperationMeta meta) => meta.rootType ?? meta.entity;
