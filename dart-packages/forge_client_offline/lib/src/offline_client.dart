import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:forge_client/devtools.dart';
import 'package:forge_client/forge_client.dart';
import 'package:uuid/uuid.dart';

import 'outbox/events.dart';
import 'outbox/network_error.dart';
import 'outbox/network_failure.dart';
import 'outbox/outbox_entry.dart';
import 'outbox/outbox_failure.dart';
import 'outbox/outbox_transport.dart';
import 'outbox/overlay_intent.dart';
import 'storage/encrypted_storage.dart';

/// Decides how a queued write is drawn after a restart, when the app's own
/// optimistic callback is gone.
typedef OverlayIntentFor = OverlayIntent Function(
  OperationMeta meta,
  TagContext args,
);

/// Queues the writes a principal makes while the server cannot take them,
/// and replays them in order, each with a stable `Idempotency-Key`.
///
/// The cache owns storage: this reads `cache.session` and follows
/// `cache.sessionChanges`, and never opens or closes a session itself.
///
/// Retry ownership: the transport under the [OutboxTransport] (normally a
/// `RestTransport`) retries by HTTP method only, GET, HEAD, PUT and DELETE,
/// as it always has. Every write this client tracks is retried here instead:
/// a 408, a 429, a 5xx, a 409 that carries `Retry-After` (the idempotency
/// middleware's "same key still in flight" reply) and a request that never
/// left the device all go back in the queue and are resent after a doubling
/// backoff, with the same `Idempotency-Key` on every attempt. A generated
/// `RestClient` stays at one attempt for the writes it does not retry.
///
/// Principals: the supported order is `cache.setPrincipal(next)` first, then
/// swap the credentials the transport sends. The outbox suspends
/// synchronously when the principal starts changing, so no listener sees the
/// previous principal's queue, and resumes when the next principal's session
/// opens. `authPrincipal` (who the transport's credentials currently belong
/// to) is a defence in depth: the outbox checks it immediately before each
/// send and holds a write while the two disagree. It does not fully cover an
/// app that swaps credentials first. A credentials read already in flight
/// can resolve with the new token before `setPrincipal` runs, and that write
/// then goes out under it. Without `authPrincipal`, an app that swaps
/// credentials first can also have a write that was already due go out under
/// the next principal's credentials. Every attempt
/// carries a cancel that the principal change completes, so an attempt still
/// waiting on its credentials, or on a credential refresh after a 401, is
/// never sent under the next principal's (an attempt already on the wire is
/// aborted; its record stays, marked sent, in its own principal's storage,
/// and its caller gets [OutboxSuspended]). A write made
/// while a principal's session or outbox is still opening, and overtaken by
/// a principal change before it could be queued, fails with [OutboxStale]:
/// nothing is sent or kept.
///
/// Persisting: every tracked write is stored before its first request goes
/// out, marked sent, and removed when the server accepts it. A process
/// killed while the request is out therefore finds the write on the next
/// launch: one safe to repeat (PUT, DELETE, or `idempotent`) replays with its
/// key, any other surfaces as [OutboxUncertain]. The cost is one encrypted
/// write and one delete per online mutation.
///
/// Idempotency: a POST or PATCH is safe to replay after a lost reply only
/// because the server remembers its `Idempotency-Key`, which it does for its
/// TTL (24 hours by default, `IdempotencyTTL` on the server) and only as long
/// as its store survives. Such a write whose first unanswered attempt is
/// older than `idempotencyWindow` is not replayed automatically; it surfaces
/// as [OutboxUncertain]. A response carrying `Idempotency-Skipped` (the server
/// did not deduplicate the write, usually because auth runs after its
/// idempotency middleware) is reported through `onError` with the context
/// `outbox.idempotency-skipped`. A replay carrying `Idempotent-Truncated`
/// (the stored response was too large to keep) succeeds with no body, and
/// the write's entity is invalidated so the next read refetches.
///
/// Lanes: writes to one entity type replay one at a time, in order. Lanes are
/// independent, so a backoff armed by one lane does not hold a new write to
/// an idle lane: that write is still sent directly. The backoff only holds
/// replays of writes already queued. A successful `retry()` of a failed write
/// cancels that shared backoff timer, so writes in other lanes that were
/// backing off replay as well.
///
/// A queued write's future stays pending for as long as the write is queued,
/// which can be indefinitely (offline for days, or held by the backoff). The
/// cache shows its optimistic overlay meanwhile; a UI should draw from the
/// cache and never block, or show a spinner, on that future. Use [pending],
/// [pendingCount] and [failures] for outbox status instead.
///
/// Starting: [OfflineClient.open] builds the transport, the cache and this
/// client in one call and is the usual way in. Build the client before you
/// register any `watchPrincipalChanging` listener of your own (open does so
/// before it returns): its listener must run first, so no listener of yours
/// can see the previous principal's queue during a switch.
///
/// Snapshots: the cache is written to the session's snapshot after its
/// commits, debounced, and read back (marked stale, so watched queries
/// refetch) when a session opens. See [flush], [close] and [signOut].
///
/// Devtools: this client is the [OutboxInspector] the devtools Outbox panel
/// drives, and the [OutboxFailureSource] a `ForgeOutboxListener` listens to.
/// Both see only the current principal's writes.
final class OfflineClient implements OutboxInspector, OutboxFailureSource {
  /// Attaches to [transport], which must be the transport [cache] was built
  /// with, and follows [cache]'s sessions. [operations] is the generated
  /// table, used to turn a stored operation id back into its metadata.
  ///
  /// Writes to [excludedEntities] (for example entities a sync source owns)
  /// bypass the outbox.
  ///
  /// [duplicateWindow] is off by default ([Duration.zero]): two identical
  /// writes are two requests, because a deliberate repeat must never be
  /// dropped silently. An app that wants a double tap coalesced opts in with
  /// a short window; an identical write (same operation, same arguments) made
  /// within it while the first is still pending then shares the first one's
  /// request and response.
  ///
  /// [authPrincipal] returns the principal the transport's credentials belong
  /// to right now. Immediately before every send the outbox compares it with
  /// the principal whose write it is, and on a mismatch it holds the write and
  /// checks again after a backoff, on reconnect and on the next session. It is
  /// a defence in depth behind the supported order (`setPrincipal` first,
  /// then swap credentials), not a substitute for it: a credentials read
  /// already in flight when an app swaps credentials first is past this
  /// check.
  ///
  /// Each backoff delay is jittered by up to 20 percent either way, using
  /// [random] (a source in `[0, 1)`, injectable for tests).
  ///
  /// After [maxRetryableFailures] consecutive retryable failures of one write
  /// (a 408, a 429, a 5xx, a request that never left the device), the write
  /// stops retrying on its own and surfaces as an [OutboxUncertain] failure,
  /// so a server that keeps answering 503 does not hide it forever. The
  /// record and its `Idempotency-Key` stay, and `retry` resends with the same
  /// key. The count lives in memory and starts again after a restart.
  ///
  /// Writes to an entity a sync source owns never reach the transport (the
  /// cache hands them to the source), so they are never tracked. A stored
  /// write for such an entity, queued before the source took it over, is
  /// surfaced as an [OutboxGone] with status 0 rather than replayed.
  ///
  /// Snapshots are written [snapshotDebounce] after the last commit, and at
  /// most five debounce periods after the first.
  ///
  /// [idempotencyWindow] is how long after its first unanswered attempt a
  /// write that is safe only because of its `Idempotency-Key` (a POST or
  /// PATCH on an `idempotent` route) is still replayed automatically. Match
  /// it to the server's `IdempotencyTTL` (24 hours by default). Past it the
  /// server may have forgotten the key, so the write surfaces as
  /// [OutboxUncertain] for the app to retry or discard. PUT and DELETE are
  /// safe by method and replay at any age.
  ///
  /// [storageResets] is the storage's `resets` stream
  /// (`EncryptedSqliteStorage.resets`). A reset deletes a database whose key
  /// no longer fits, and any writes queued in it, so pass it whenever the
  /// cache was built with an `EncryptedSqliteStorage`: each reset is then
  /// reported through `onError` (context `storage.reset`), emitted on
  /// [resets] and kept in [currentResets]. [OfflineClient.open] wires it
  /// itself.
  OfflineClient({
    required QueryCache cache,
    required this._operations,
    required ConnectivitySignal connectivity,
    required OutboxTransport transport,
    this._clock = realClock,
    this._duplicateWindow = Duration.zero,
    Duration initialBackoff = const Duration(seconds: 1),
    this._maxBackoff = const Duration(minutes: 5),
    Set<String> excludedEntities = const {},
    this._overlayIntent = deriveOverlayIntent,
    bool initiallyOnline = true,
    this._authPrincipal,
    this._onError,
    double Function()? random,
    this._maxRetryableFailures = 8,
    this._snapshotDebounce = const Duration(seconds: 1),
    Stream<StorageReset>? storageResets,
    this._idempotencyWindow = const Duration(hours: 24),
  }) : assert(_maxRetryableFailures >= 1),
       _random = random ?? math.Random().nextDouble,
       _cache = cache,
       _transport = transport,
       _initialBackoff = initialBackoff,
       _excluded = excludedEntities,
       _online = initiallyOnline,
       _backoff = initialBackoff {
    _control = _Control(this);
    transport.attach(_handle);
    _subscriptions
      ..add(connectivity.online.listen(_setOnline))
      ..add(cache.sessionChanges.listen(_onSession))
      ..add(cache.commits.listen((_) => _onCommit()));
    if (storageResets != null) {
      _subscriptions.add(storageResets.listen(_onStorageReset));
    }
    // Synchronous, before the cache clears: nothing told of the change may
    // see this principal's queue. Only in-memory state moves here.
    _unwatchChanging = cache.watchPrincipalChanging((_) {
      _principalChanges++;
      // Every attempt on the wire belongs to the leaving principal. Their
      // cancels complete here, and the transport checks a cancel after each
      // credential read and before a refresh retry, so none of them goes out
      // under the next principal's credentials.
      final leaving = List.of(_attemptCancels);
      _attemptCancels.clear();
      for (final cancel in leaving) {
        cancel.complete();
      }
      _currentResets.clear();
      _suspend();
    });

    final session = cache.session;
    if (session != null && session.principal == cache.principal) {
      unawaited(_restoreSession(session));
    }
  }

  final QueryCache _cache;
  final Map<String, OperationMeta> _operations;
  final OutboxTransport _transport;
  final Clock _clock;
  final Duration _duplicateWindow;
  final Duration _initialBackoff;
  final Duration _maxBackoff;
  final Set<String> _excluded;
  final OverlayIntentFor _overlayIntent;
  final String? Function()? _authPrincipal;
  final void Function(Object error, String context)? _onError;
  final double Function() _random;
  final int _maxRetryableFailures;
  final Duration _snapshotDebounce;
  final Duration _idempotencyWindow;

  late final _Control _control;
  late final void Function() _unwatchChanging;
  final List<StreamSubscription<Object?>> _subscriptions = [];
  static const Uuid _uuid = Uuid();

  final List<_Pending> _queue = [];
  final List<OutboxFailure> _currentFailures = [];
  final StreamController<OutboxFailure> _failures =
      StreamController<OutboxFailure>.broadcast();
  final StreamController<int> _pendingCount = StreamController<int>.broadcast();
  final List<StorageReset> _currentResets = [];
  final StreamController<StorageReset> _resets =
      StreamController<StorageReset>.broadcast();

  /// The storage [open] built the cache over; null for a client built with
  /// the constructor, which cannot erase anything.
  StorageAdapter? _storage;

  /// Whether [open] built the cache, so [dispose] disposes it too.
  bool _ownsCache = false;

  StorageSession? _session;
  Future<void>? _restoring;
  bool _restored = false;
  bool _online;
  bool _draining = false;
  bool _disposed = false;
  bool _heldForCredentials = false;
  int _nextSeq = 1;
  Duration _backoff;
  Timer? _retryTimer;
  Timer? _snapshotTimer;
  Timer? _snapshotMaxTimer;

  /// Whether the current session's stored snapshot has been read (restored
  /// or skipped). Until then nothing is written, so an empty cache never
  /// replaces a stored snapshot before it could be read.
  bool _hydrated = false;

  /// A commit arrived before [_hydrated]; a snapshot is owed once it is.
  bool _snapshotOwed = false;

  /// Bumped by every suspension. Work that started under an earlier epoch
  /// belongs to a principal this client no longer serves: it may still
  /// finish against that principal's own storage, but it never touches the
  /// in-memory queue, the failures or the counts again.
  int _epoch = 0;

  /// The cancel of every attempt on the wire, completed when the principal
  /// starts changing. One per attempt, dropped when the attempt ends, so no
  /// listener outlives its request.
  final Set<Completer<void>> _attemptCancels = {};

  /// Counts real principal changes (the cache calls a changing listener
  /// only when the principal differs). A write captures it when it arrives;
  /// unlike `cache.generation`, a plain `clear()` does not move it.
  int _principalChanges = 0;

  /// The cache this client queues writes for.
  QueryCache get cache => _cache;

  /// Failures as they happen, including ones restored from storage.
  /// Broadcast: a screen and a test may both listen.
  @override
  Stream<OutboxFailure> get failures => _failures.stream;

  /// Every storage reset of the current principal as it happens: a database
  /// whose key no longer fits was deleted unread and started fresh, and any
  /// writes queued in it are gone. Broadcast. A reset during [open] happens
  /// before anyone can listen: read [currentResets] for it.
  Stream<StorageReset> get resets => _resets.stream;

  /// The storage resets of the current principal since it was set. Emptied
  /// when the principal changes, so it never shows another principal's.
  List<StorageReset> get currentResets => List.unmodifiable(_currentResets);

  /// Failures waiting for the app to retry, edit or discard them.
  List<OutboxFailure> get currentFailures =>
      List.unmodifiable(_currentFailures);

  /// The stored writes, pending or failed, in replay order.
  List<OutboxEntry> get pending => List.unmodifiable([
    for (final p in _queue)
      if (p.persisted) p.entry,
  ]);

  /// The number of stored writes, after every change. Broadcast, so a new
  /// listener hears nothing until the next change: seed from
  /// [pendingCountNow].
  Stream<int> get pendingCount => _pendingCount.stream;

  /// The number of stored writes right now, the value [pendingCount] last
  /// emitted or is about to.
  int get pendingCountNow => _storedCount();

  /// Whether the client currently believes the network is reachable.
  bool get isOnline => _online;

  /// Completes when the current session's snapshot and outbox have been
  /// restored. Waits for a principal switch in progress first (the cache has
  /// no session while one runs). With no session it completes at once. Safe
  /// to call more than once: a session is restored once.
  Future<void> restore() async {
    await _cache.idle;
    final session = _cache.session;
    if (session == null || session.principal != _cache.principal) return;
    await _restoreSession(session);
  }

  /// Detaches from the transport and stops listening. Parked callers get
  /// [OutboxSuspended]; stored writes are kept. A cache built by [open] is
  /// disposed too, which closes its session.
  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    _unwatchChanging();
    for (final subscription in _subscriptions) {
      await subscription.cancel();
    }
    _suspend();
    _transport.attach(null);
    await _failures.close();
    await _pendingCount.close();
    await _resets.close();
    if (_ownsCache) await _cache.dispose();
  }

  /// Builds an [OutboxTransport] over [transport], a [QueryCache] that owns
  /// [storage], and an [OfflineClient] over both; sets [principal], waits for
  /// its session to open and restores it. App code then uses [cache] like
  /// any other cache, and the writes it makes while offline are queued.
  ///
  /// The client is built, with its principal listener, before the principal
  /// is set and before this returns, so it runs ahead of any
  /// `watchPrincipalChanging` listener the app registers later.
  ///
  /// [connectivity] null means "assume online": a write is still queued when
  /// its request fails before reaching the server. Entities owned by
  /// [syncSources] bypass the outbox. [authPrincipal] and
  /// [idempotencyWindow] are passed to the client; see the constructor.
  ///
  /// When [storage] cannot open the principal (`KeyUnavailable`, `WrongKey`,
  /// `UnsupportedSchemaVersion`, `EncryptionUnavailable` and so on) this
  /// throws that error, after reporting it to [onError] with the context
  /// `storage`, and disposes what it built. A storage that neither opens nor
  /// fails throws [TimeoutException] after [sessionTimeout]. Nothing is
  /// erased: after repeated `KeyUnavailable` while the device is unlocked,
  /// the app may offer `EncryptedSqliteStorage.resetOfflineData`.
  ///
  /// When [storage] is an `EncryptedSqliteStorage`, its resets are wired to
  /// [resets] and [currentResets]. Use one storage adapter per directory per
  /// isolate.
  ///
  /// After the session opens, this restores, and restoring waits for
  /// `cache.idle`, which includes every sync source's `start`. A source whose
  /// `start` never returns therefore keeps this from returning, and
  /// [sessionTimeout] does not cover it (it bounds the storage open only).
  /// `SyncSource.start` must return promptly and do network work in the
  /// background.
  ///
  /// [devtools] attaches the forge devtools to the cache, in debug and profile
  /// builds only (it does nothing when `kForgeDevtools` is false, and a release
  /// build compiles it out). `configureClient` does this on its own, but this
  /// method never goes through it, so it is opt-in here. A [RestTransport] is
  /// wrapped in the devtools' offline and latency simulator, placed beneath
  /// the outbox, so a write made while the panel says offline is queued like
  /// one made on a lost network, and a replay feels the simulated network. The
  /// simulator's connectivity is merged into [connectivity] for you
  /// (`withSimulatedConnectivity`). The client is registered as the panel's
  /// `OutboxInspector`, so replay and discard work, and [operations] lets the
  /// panel preview any operation. Any other transport is used as it is, with
  /// no simulator and no request log. Attaching happens before the principal
  /// is set, so the panel sees the whole session.
  static Future<OfflineClient> open({
    required Transport transport,
    required EntitySchema entities,
    required Map<String, OperationMeta> operations,
    required StorageAdapter storage,
    required String principal,
    ConnectivitySignal? connectivity,
    CommitScheduler? commitScheduler,
    Clock clock = realClock,
    Set<String> excludedEntities = const {},
    List<SyncSource> syncSources = const [],
    Duration sessionTimeout = const Duration(seconds: 30),
    Duration snapshotDebounce = const Duration(seconds: 1),
    String? Function()? authPrincipal,
    void Function(Object error, String context)? onError,
    Duration idempotencyWindow = const Duration(hours: 24),
    bool devtools = false,
  }) async {
    // The cache reports a session that failed to open to onError and never
    // emits it, so the wait below listens there too.
    Completer<void>? opening;
    void report(Object error, String context) {
      final waiting = opening;
      if (context == 'storage' && waiting != null && !waiting.isCompleted) {
        waiting.completeError(error);
      }
      onError?.call(error, context);
    }

    // The simulator sits directly on the wire, beneath the outbox: outside it,
    // offline would fail a write before the outbox could queue it.
    final controls = kForgeDevtools && devtools && transport is RestTransport
        ? ControlledTransport(transport)
        : null;
    final signal = connectivity ?? const _AssumeOnline();
    final outbox = OutboxTransport(controls ?? transport);
    final cache = QueryCache(
      transport: outbox,
      entities: entities,
      commitScheduler: commitScheduler,
      clock: clock,
      onError: report,
      syncSources: syncSources,
      storage: storage,
    );
    final client =
        OfflineClient(
            cache: cache,
            operations: operations,
            connectivity: controls == null
                ? signal
                : withSimulatedConnectivity(signal, controls),
            transport: outbox,
            clock: clock,
            excludedEntities: {
              ...excludedEntities,
              for (final source in syncSources) ...source.entities,
            },
            authPrincipal: authPrincipal,
            onError: onError,
            snapshotDebounce: snapshotDebounce,
            idempotencyWindow: idempotencyWindow,
            storageResets: storage is EncryptedSqliteStorage
                ? storage.resets
                : null,
          )
          .._storage = storage
          .._ownsCache = true;

    if (kForgeDevtools && devtools) {
      registerForgeServiceExtensions(
        cache,
        transport: transport is RestTransport ? transport : null,
        controls: controls,
        operations: operations,
        outbox: client,
      );
    }

    final waiting = opening = Completer<void>();
    final subscription = cache.sessionChanges.listen((session) {
      if (session?.principal == principal && !waiting.isCompleted) {
        waiting.complete();
      }
    });
    try {
      cache.setPrincipal(principal);
      await waiting.future.timeout(sessionTimeout);
      opening = null;
      await client.restore();
    } on Object {
      opening = null;
      await subscription.cancel();
      await client.dispose();
      rethrow;
    }
    await subscription.cancel();
    return client;
  }

  /// Writes the snapshot now. Call it when the app pauses. Waits for a
  /// restore in progress first, so the stored snapshot is read before it is
  /// replaced.
  Future<void> flush() async {
    final restoring = _restoring;
    if (restoring != null) await restoring;
    _cancelSnapshot();
    await _writeSnapshot();
  }

  /// Flushes the snapshot and disposes the client (and the cache when [open]
  /// built it, which closes its session). Data is kept.
  Future<void> close() async {
    await flush();
    await dispose();
  }

  /// Signs the current principal out: sets the cache's principal to null and
  /// waits until its sources have stopped and its session has closed. With
  /// [erase] (the default) it then destroys the principal's storage (the key
  /// first, then the files), queued writes included. Without it the
  /// snapshot is flushed first and everything is kept for the principal's
  /// return.
  ///
  /// Erasing needs the storage given to [open]. A client built with the
  /// constructor throws [StateError] for it, and the app runs
  /// `cache.setPrincipal(null)`, `await cache.idle` and
  /// `storage.destroy(principal)` itself.
  Future<void> signOut({bool erase = true}) async {
    final storage = _storage;
    if (erase && storage == null) {
      throw StateError(
        'signOut(erase: true) needs the storage given to OfflineClient.open; '
        'otherwise call cache.setPrincipal(null), await cache.idle, then '
        'storage.destroy(principal)',
      );
    }

    final principal = _cache.principal;
    if (!erase) await flush();
    await _leave();
    if (storage != null && erase && principal != null) {
      await storage.destroy(principal);
    }
  }

  /// Erases everything this package keeps on the device, for every
  /// principal, and leaves the cache signed out. See
  /// `EncryptedSqliteStorage.resetOfflineData`: queued writes that never
  /// reached the server are lost.
  ///
  /// Never called automatically. Call it after the user confirms, or after
  /// [open] keeps throwing `KeyUnavailable` while the device is unlocked (a
  /// failed [open] disposed its client, so call
  /// `EncryptedSqliteStorage.resetOfflineData` on the storage directly in
  /// that case). It signs out first, without erasing per principal, then
  /// runs the storage reset. Needs the `EncryptedSqliteStorage` given to
  /// [open]; anything else throws [StateError].
  Future<void> resetOfflineData() async {
    final storage = _storage;
    if (storage is! EncryptedSqliteStorage) {
      throw StateError(
        'resetOfflineData needs the EncryptedSqliteStorage given to '
        'OfflineClient.open; otherwise call cache.setPrincipal(null), await '
        'cache.idle, then storage.resetOfflineData()',
      );
    }
    await _leave();
    await storage.resetOfflineData();
  }

  /// Sets the principal to null and waits for the transition: the sources
  /// stopped and the session closed. `sessionChanges` emits null before
  /// either, so it is not what this waits for.
  Future<void> _leave() async {
    _cache.setPrincipal(null);
    await _cache.idle;
  }

  /// Sends the stored write [mutationId] now, out of its turn, ignoring the
  /// backoff and the connectivity flag. Completes when the server accepted
  /// it. Throws its [OutboxFailure] when the server refused it, and
  /// [OutboxOffline] when it could not go (it stays queued; see below). A
  /// failed write is cleared and redrawn first. Waits for an attempt already
  /// on the wire, and for the write to be re-issued through the cache,
  /// rather than sending beside them. The devtools Outbox panel drives this.
  ///
  /// Out of its turn means ahead of earlier writes in its own lane too: a
  /// forced replay of an update can reach the server before the queued
  /// create it depends on, and fail (a 404, say) where the normal order
  /// would have succeeded. The failure is then reported like any other and
  /// can be retried once the earlier write has gone.
  ///
  /// [OutboxOffline.cause] says why a write stayed queued: no network, a
  /// retryable status (with [OutboxOffline.status]), or credentials that
  /// belong to another principal. A storage error throws
  /// [OutboxUnavailable].
  ///
  /// Throws [StateError] for an id that is not a stored write of the current
  /// principal, and for a write no replay can send (its operation is not in
  /// the table, or a sync source owns its entity): discard those.
  @override
  Future<void> replay(String mutationId) async {
    final p = _inspected(mutationId);
    final session = _session!;
    final meta = p.meta;
    if (meta == null) {
      throw StateError(
        'write $mutationId names an operation that is not in the operations '
        'table; discard it',
      );
    }
    if (_ownedBySource(meta)) {
      throw StateError(
        'write $mutationId is for ${meta.entity}, which a sync source owns and '
        'sends itself; discard it',
      );
    }

    final failure = p.failure;
    if (failure != null) {
      final queued = _requeued(p.entry, failure);
      final error = await _mark(session, queued);
      if (error != null) throw OutboxUnavailable(error);
      if (p.epoch != _epoch) throw const OutboxSuspended();
      if (p.failure != null && _queue.contains(p)) {
        p
          ..entry = queued
          ..retryableFailures = 0;
        _clearFailure(p);
        p.completer = _parked();
        _reissue(p);
        _notifyPending();
      }
    }

    // What is already under way finishes first: the re-issue through the
    // cache (so the cache commits the response), then any attempt on the
    // wire. Each is a completion, not a count of event-loop turns.
    final reissued = p.reissued;
    if (reissued != null) await reissued.future;
    for (var attempt = p.sending; attempt != null; attempt = p.sending) {
      await attempt.future;
    }

    if (p.sent) return;
    if (p.epoch == _epoch && p.failure == null && _queue.contains(p)) {
      // The drain sends it next, ahead of its lane and past the backoff and
      // the offline flag, one request at a time like every other replay.
      final forced = p.forced ??= Completer<void>();
      _scheduleDrain();
      await forced.future;
    }
    _throwUnlessSent(p);
  }

  /// Removes the stored write [mutationId] and rolls back its overlay (its
  /// parked caller gets [OutboxDiscarded]). The devtools Outbox panel drives
  /// this; [OutboxFailure.discard] does the same for a failure.
  ///
  /// Throws [StateError] for an id that is not a stored write of the current
  /// principal or that is on the wire, and [OutboxUnavailable] when storage
  /// refuses the removal.
  @override
  Future<void> discard(String mutationId) async {
    final p = _inspected(mutationId);
    if (p.inFlight) {
      throw StateError(
        'write $mutationId is being sent; discard it after its answer',
      );
    }
    final error = await _discard(mutationId);
    if (error != null) throw OutboxUnavailable(error);
  }

  /// The stored write [mutationId] of the current principal. Reads only the
  /// in-memory queue, which a principal change empties synchronously, so
  /// another principal's writes are never found.
  _Pending _inspected(String mutationId) {
    final p = _byId(mutationId);
    if (_session == null || p == null || !p.persisted || p.epoch != _epoch) {
      throw StateError('no stored write $mutationId for the current principal');
    }
    return p;
  }

  /// After a forced replay of [p]: returns when it was sent, and otherwise
  /// throws why not.
  void _throwUnlessSent(_Pending p) {
    if (p.sent) return;
    if (p.epoch != _epoch) throw const OutboxSuspended();
    final failure = p.failure;
    if (failure != null) throw failure;
    if (_queue.contains(p)) {
      final storageError = p.storageError;
      if (storageError != null) throw OutboxUnavailable(storageError);
      throw p.held ?? OutboxOffline(p.entry.id);
    }
    throw const OutboxDiscarded();
  }

  // ---------------------------------------------------------------- snapshots

  void _onCommit() {
    if (_session == null || _disposed) return;
    if (!_hydrated) {
      _snapshotOwed = true;
      return;
    }
    _scheduleSnapshot();
  }

  void _scheduleSnapshot() {
    _snapshotTimer?.cancel();
    _snapshotTimer = Timer(_snapshotDebounce, _snapshotDue);
    _snapshotMaxTimer ??= Timer(_snapshotDebounce * 5, _snapshotDue);
  }

  void _snapshotDue() {
    _cancelSnapshot();
    unawaited(_writeSnapshot());
  }

  void _cancelSnapshot() {
    _snapshotTimer?.cancel();
    _snapshotTimer = null;
    _snapshotMaxTimer?.cancel();
    _snapshotMaxTimer = null;
  }

  /// Writes the cache, normalized (owned rows are omitted), to the current
  /// session. Only while that session is still the cache's: after a switch
  /// the cache holds another principal's data, which must never land in
  /// this file. The dehydrate runs synchronously under that check.
  Future<void> _writeSnapshot() async {
    final session = _session;
    if (session == null ||
        !_hydrated ||
        !identical(_cache.session, session) ||
        _cache.principal != session.principal) {
      return;
    }
    try {
      await session.writeSnapshot(
        dehydrate(_cache, principal: session.principal),
      );
    } on Object catch (error) {
      _report(error, 'snapshot.write');
    }
  }

  /// Whether the cache holds none of what a snapshot carries: no settled
  /// query and no record of an entity the cache does not own. Rows a sync
  /// source projects are owned; a snapshot never holds or restores them, so
  /// a source that projects while the snapshot is read does not stop it.
  bool _holdsNoSnapshotData() {
    if (_cache.queries.isNotEmpty) return false;
    for (final key in _cache.store.keys) {
      // An entity key is `<typename>:<id...>`; the typename ends at the
      // first colon (the rule core's snapshot and frame paths share).
      final colon = key.indexOf(':');
      final typename = colon == -1 ? key : key.substring(0, colon);
      if (!_cache.owns(typename)) return false;
    }
    return true;
  }

  void _onStorageReset(StorageReset reset) {
    // Only the principal now being served; never kept for anyone else.
    if (_disposed || reset.principal != _cache.principal) return;
    _currentResets.add(reset);
    if (!_resets.isClosed) _resets.add(reset);
    _report(reset, 'storage.reset');
  }

  // ---------------------------------------------------------------- sessions

  void _onSession(StorageSession? session) {
    if (session == null) {
      _suspend();
    } else if (!identical(session, _session)) {
      unawaited(_restoreSession(session));
    }
  }

  Future<void> _restoreSession(StorageSession session) {
    if (identical(_session, session)) {
      return _restoring ?? Future<void>.value();
    }
    _suspend();
    _session = session;
    return _restoring = _restore(session);
  }

  Future<void> _restore(StorageSession session) async {
    await _hydrate(session);
    await _loadOutbox(session);
    _scheduleDrain();
  }

  /// Reads the session's snapshot into the cache, marked stale so a watched
  /// query refetches. Only into a cache that holds nothing a snapshot
  /// carries yet: data that arrived while the snapshot was read is newer,
  /// and an older snapshot is never merged over it.
  Future<void> _hydrate(StorageSession session) async {
    try {
      final snapshot = await session.readSnapshot();
      if (!identical(_session, session)) return;
      if (snapshot != null) {
        if (_holdsNoSnapshotData()) {
          hydrate(
            _cache,
            snapshot,
            principal: session.principal,
            operations: _operations,
            stale: true,
          );
        } else {
          _report(
            StateError(
              'the stored snapshot was not restored: the cache already holds '
              'newer data',
            ),
            'snapshot.skipped',
          );
        }
      }
    } on Object catch (error) {
      _report(error, 'snapshot.hydrate');
    }
    if (!identical(_session, session)) return;
    _hydrated = true;
    if (_snapshotOwed) {
      _snapshotOwed = false;
      _scheduleSnapshot();
    }
  }

  /// Forgets everything in memory about the current principal. Synchronous
  /// and storage-free, so it is safe inside `watchPrincipalChanging`: the
  /// records stay in that principal's storage. Parked callers get
  /// [OutboxSuspended]. A request already on the wire is left to finish
  /// here; on a principal change the changing listener has cancelled it
  /// first, and otherwise (dispose, a closed session) its caller gets the
  /// server's answer.
  void _suspend() {
    _epoch++;
    _retryTimer?.cancel();
    _retryTimer = null;
    _cancelSnapshot();
    _hydrated = false;
    _snapshotOwed = false;
    _draining = false;
    _heldForCredentials = false;
    _backoff = _initialBackoff;

    final parked = List<_Pending>.of(_queue);
    _queue.clear();
    _currentFailures.clear();
    for (final p in parked) {
      // A replay waiting on either finds the epoch moved.
      _signal(p.reissued);
      _settleForced(p);
      if (!p.inFlight) _reject(p, const OutboxSuspended());
    }

    _session = null;
    _restoring = null;
    _restored = false;
    _nextSeq = 1;
    _notifyPending();
  }

  Future<void> _loadOutbox(StorageSession session) async {
    final List<PendingMutationRecord> records;
    try {
      records = await session.readOutbox();
    } on Object catch (error) {
      _report(error, 'outbox.read');
      if (identical(_session, session)) _restored = true;
      return;
    }
    if (!identical(_session, session)) return;

    final entries = <OutboxEntry>[];
    for (final record in records) {
      try {
        entries.add(OutboxEntry.fromRecord(record));
      } on FormatException catch (error) {
        _report(error, 'outbox.decode');
      }
    }

    // List.sort is not stable: break ties by storage order explicitly.
    final ordered = entries.indexed.toList()
      ..sort((a, b) {
        final bySeq = a.$2.seq.compareTo(b.$2.seq);
        return bySeq != 0 ? bySeq : a.$1.compareTo(b.$1);
      });

    for (final (_, entry) in ordered) {
      if (entry.seq >= _nextSeq) _nextSeq = entry.seq + 1;
    }
    for (final (_, entry) in ordered) {
      await _admit(session, entry);
      if (!identical(_session, session)) return;
    }

    _restored = true;
    _notifyPending();
  }

  Future<void> _admit(StorageSession session, OutboxEntry entry) async {
    final meta = _operations[entry.operationId];
    final p = _Pending(
      entry: entry,
      meta: meta,
      lane: meta == null ? 'op:${entry.operationId}' : _laneOf(meta),
      epoch: _epoch,
      persisted: true,
    );
    _queue.add(p);

    final failureJson = entry.failureJson;
    if (failureJson != null) {
      final failure = _decodeFailure(entry, failureJson);
      p.failure = failure;
      _currentFailures.add(failure);
      if (!_failures.isClosed) _failures.add(failure);
      return;
    }

    if (meta == null) {
      await _fail(
        session,
        p,
        OutboxGone(
          mutationId: entry.id,
          operationId: entry.operationId,
          control: _control,
          status: 0,
          body: 'operation ${entry.operationId} is not in the operations table',
        ),
      );
      return;
    }

    if (_ownedBySource(meta)) {
      await _fail(
        session,
        p,
        OutboxGone(
          mutationId: entry.id,
          operationId: entry.operationId,
          control: _control,
          status: 0,
          body:
              '${meta.entity} is owned by a sync source, which sends its own '
              'writes',
        ),
      );
      return;
    }

    if (entry.sentAt != null && !isSafeToRepeat(meta)) {
      await _fail(
        session,
        p,
        OutboxUncertain(
          mutationId: entry.id,
          operationId: entry.operationId,
          control: _control,
          reason:
              'the app stopped after this write was sent and before its '
              'response arrived',
        ),
      );
      return;
    }

    if (_pastWindow(entry, meta)) {
      await _fail(session, p, _expired(p));
      return;
    }

    p.completer = _parked();
    _reissue(p);
  }

  OutboxFailure _decodeFailure(OutboxEntry entry, String failureJson) {
    try {
      final json = jsonDecode(failureJson);
      if (json is Map<String, Object?>) {
        return OutboxFailure.fromJson(
          json,
          mutationId: entry.id,
          operationId: entry.operationId,
          control: _control,
        );
      }
    } on FormatException catch (error) {
      _report(error, 'outbox.failure');
    }
    return OutboxUncertain(
      mutationId: entry.id,
      operationId: entry.operationId,
      control: _control,
      reason: 'the stored failure could not be read',
    );
  }

  /// Puts a stored write back through the cache so it is drawn with its
  /// overlay and its response is committed when the replay succeeds.
  void _reissue(_Pending p) {
    _signal(p.reissued);
    p
      ..awaitingReissue = true
      ..reissued = Completer<void>();
    final future = _cache.mutate(
      p.meta!,
      p.entry.args,
      options: MutateOptions(
        headers: {outboxReplayHeader: p.entry.id},
        optimistic: p.entry.intent.toOptimistic(),
      ),
    );
    unawaited(
      future.then<void>(
        (_) {},
        onError: (Object error) {
          if (p.awaitingReissue) {
            _reissueArrived(p);
            _report(error, 'outbox.reissue');
            _scheduleDrain();
          }
        },
      ),
    );
  }

  // ---------------------------------------------------------------- intake

  Future<Object?> _handle(TransportRequest request) {
    final marker = request.headers[outboxReplayHeader];
    if (marker != null) {
      final p = _byId(marker);
      final parked = p?.completer;
      if (p != null && parked != null) {
        _reissueArrived(p);
        _scheduleDrain();
        return parked.future;
      }
      // No queued write carries this id, so there is no Idempotency-Key to
      // send it with. Refused rather than sent unprotected.
      return Future<Object?>.error(
        StateError(
          'an outbox replay marker names no queued write; it was not sent',
        ),
      );
    }

    final meta = request.meta;
    final method = meta.method.toUpperCase();
    if (method == 'GET' ||
        method == 'HEAD' ||
        _operations[meta.id] == null ||
        _excluded.contains(meta.entity) ||
        _ownedBySource(meta)) {
      return _transport.inner.execute(request);
    }

    // How many principal changes had happened when the write was made. A
    // write that waits below and finds the count moved was made for someone
    // who has left, even if they have since come back.
    return _track(
      request,
      principalChanges: _principalChanges,
      waitedForSwitch: false,
    );
  }

  Future<Object?> _track(
    TransportRequest request, {
    required int principalChanges,
    required bool waitedForSwitch,
  }) {
    // Resumed after a wait, and the principal changed meanwhile: dispatching
    // now would send this write under whoever is signed in next.
    if (_principalChanges != principalChanges) {
      return Future<Object?>.error(const OutboxStale());
    }

    // A principal is set and its session is not open yet (or the cache still
    // holds the previous one): a switch is in progress. The write belongs to
    // the next principal, so it waits for their session rather than going
    // out untracked.
    final principal = _cache.principal;
    final current = _cache.session;
    if (!waitedForSwitch &&
        principal != null &&
        (current == null || current.principal != principal)) {
      return _cache.idle.then(
        (_) => _track(
          request,
          principalChanges: principalChanges,
          waitedForSwitch: true,
        ),
      );
    }

    // No storage, no principal, or a session that failed to open.
    if (current == null || current.principal != principal) {
      return _transport.inner.execute(request);
    }

    if (!identical(_session, current)) unawaited(_restoreSession(current));

    final restoring = _restoring;
    if (!_restored && restoring != null) {
      return restoring.then(
        (_) => _track(
          request,
          principalChanges: principalChanges,
          waitedForSwitch: waitedForSwitch,
        ),
      );
    }

    return _submit(current, request);
  }

  Future<Object?> _submit(StorageSession session, TransportRequest request) {
    final meta = request.meta;

    // Encoded once up front: a write whose arguments or overlay cannot be
    // stored is refused here, before anything is sent or persisted, so a
    // broken record never reaches the queue.
    final OutboxEntry entry;
    try {
      final made = OutboxEntry(
        id: _uuid.v4(),
        operationId: meta.id,
        args: request.args,
        requestHeaders: persistableHeaders(request.headers),
        intent: _overlayIntent(meta, request.args),
        idempotencyKey:
            headerValue(request.headers, idempotencyKeyHeader) ?? _uuid.v4(),
        createdAt: _now(),
        seq: _nextSeq,
      );
      // Every attempt, the first included, sends the arguments as they read
      // back from storage: the same bytes a replay after a restart sends, and
      // never the caller's live objects, which the app may go on changing.
      entry = made.copyWith(args: OutboxEntry.fromRecord(made.toRecord()).args);
    } on Object catch (error, stack) {
      _report(error, 'outbox.encode');
      return Future<Object?>.error(OutboxUnavailable(error), stack);
    }

    final duplicate = _duplicateOf(entry);
    if (duplicate != null) return duplicate.completer!.future;

    _nextSeq++;
    final lane = _laneOf(meta);
    final laneBusy = _queue.any((p) => p.lane == lane);
    final p = _Pending(
      entry: entry,
      meta: meta,
      lane: lane,
      epoch: _epoch,
      completer: _parked(),
      liveHeaders: request.headers,
    );
    _queue.add(p);
    // Taken first: an attempt that fails synchronously settles the
    // completer before this returns.
    final result = p.completer!.future;

    // An idle lane sends directly even while another lane's backoff is
    // armed: lanes are independent, and the backoff only holds replays.
    if (_online && !laneBusy) {
      unawaited(_attemptDirect(session, p, request.cancel));
    } else {
      unawaited(_park(session, p));
    }
    return result;
  }

  /// The pending write [entry] repeats within the opt-in duplicate window.
  _Pending? _duplicateOf(OutboxEntry entry) {
    if (_duplicateWindow <= Duration.zero) return null;
    final canonical = entry.canonicalArgs;
    final now = _clock.now();

    for (final p in _queue.reversed) {
      if (p.failure != null ||
          p.completer == null ||
          p.entry.operationId != entry.operationId) {
        continue;
      }
      if (now - p.entry.createdAt.millisecondsSinceEpoch >
          _duplicateWindow.inMilliseconds) {
        continue;
      }
      if (p.entry.canonicalArgs == canonical) return p;
    }
    return null;
  }

  /// Sends [p] at once. The record is stored first, marked sent, so a
  /// process killed while the request is out finds it on the next launch:
  /// a write safe to repeat replays with its key, any other surfaces as
  /// [OutboxUncertain]. It is removed when the server accepts the write.
  Future<void> _attemptDirect(
    StorageSession session,
    _Pending p,
    Future<void>? cancel,
  ) async {
    if (!_credentialsMatch(session)) {
      await _park(session, p, drain: false);
      if (p.epoch == _epoch) _scheduleRetry();
      return;
    }

    final queued = p.entry;
    final sending = queued.copyWith(sentAt: _now());
    final storeError = await _insert(session, sending);
    if (storeError != null) {
      // Nothing was sent: a write that could not be stored is not sent
      // unprotected, exactly as a queued one is not.
      if (p.epoch == _epoch) {
        _queue.remove(p);
        _scheduleDrain();
      }
      _reject(p, OutboxUnavailable(storeError));
      return;
    }
    p
      ..entry = sending
      ..recorded = true;

    // The principal, or the credentials, may have changed while the record
    // was written. It was not sent: put it back as queued, where it waits in
    // its own principal's storage.
    if (p.epoch != _epoch ||
        !identical(_cache.session, session) ||
        !_credentialsMatch(session)) {
      p.entry = queued;
      await _park(session, p, drain: false);
      if (p.epoch == _epoch) _scheduleRetry();
      return;
    }

    p.inFlight = true;
    final Object? response;
    try {
      response = await _transport.inner.execute(
        _requestFor(p, cancel: cancel, live: true),
      );
    } on Object catch (error, stack) {
      p.inFlight = false;
      _attemptEnded(p);
      await _directFailed(session, p, error, stack);
      return;
    }
    _attemptEnded(p);
    p
      ..inFlight = false
      ..sent = true;
    await _forget(session, p);
    final result = _accepted(p, response);
    if (p.epoch != _epoch) {
      _complete(p, result);
      return;
    }
    _queue.remove(p);
    _complete(p, result);
    _scheduleDrain();
  }

  /// Removes [p]'s record once the server has accepted or refused it for
  /// good. A failure leaves it to replay on the next launch with the same
  /// key, which the server's idempotency store answers.
  Future<void> _forget(StorageSession session, _Pending p) async {
    if (!p.recorded) return;
    try {
      await session.remove(p.entry.id);
      p.recorded = false;
    } on Object catch (error) {
      _report(error, 'outbox.remove');
    }
  }

  Future<void> _directFailed(
    StorageSession session,
    _Pending p,
    Object error,
    StackTrace stack,
  ) async {
    final status = statusOf(error);
    final kind = status == null ? classifyNetworkError(error) : null;

    // The caller cancelled, or the server answered with a status no retry
    // can change, or this is not a network failure at all: the caller gets
    // the error as it is and nothing is stored.
    // Cancelled because the principal changed (the client completes every
    // attempt's cancel then): the request may have left, so the record
    // stays, marked sent, in that principal's storage.
    if (kind == NetworkFailure.cancelled && p.epoch != _epoch) {
      _reject(p, const OutboxSuspended());
      return;
    }

    final retryableStatus = status != null && _retryableStatus(error);
    if ((status != null && !retryableStatus) ||
        (status == null &&
            (kind == null || kind == NetworkFailure.cancelled))) {
      await _forget(session, p);
      if (p.epoch == _epoch) _queue.remove(p);
      _reject(p, error, stack);
      if (p.epoch == _epoch) _scheduleDrain();
      return;
    }

    if (kind != NetworkFailure.uncertain) {
      // Never reached the server, or the server gave the key back (408, 429,
      // 5xx): no attempt is outstanding.
      p.entry = p.entry.copyWith(clearSentAt: true);
    } else {
      if (!isSafeToRepeat(p.meta!)) {
        await _fail(
          session,
          p,
          OutboxUncertain(
            mutationId: p.entry.id,
            operationId: p.entry.operationId,
            control: _control,
            reason: 'the request was sent and no response arrived ($error)',
          ),
          originalError: error,
        );
        return;
      }
    }

    // Stored, and retried after the backoff only: the network just refused
    // this write, so sending it again at once would only fail again.
    await _park(session, p, drain: false);
    if (p.epoch == _epoch && p.persisted) {
      await _backOff(session, p, error, atLeast: _retryAfter(error));
    }
  }

  Future<void> _park(
    StorageSession session,
    _Pending p, {
    bool drain = true,
  }) async {
    final error = p.recorded
        ? await _mark(session, p.entry)
        : await _insert(session, p.entry);
    if (error == null) p.recorded = true;
    if (p.epoch != _epoch) {
      // The principal changed while the record was written. It stays in
      // that principal's storage and replays when they return.
      _reject(
        p,
        error == null ? const OutboxSuspended() : OutboxUnavailable(error),
      );
      return;
    }
    if (error != null) {
      _queue.remove(p);
      _reject(p, OutboxUnavailable(error));
      _scheduleDrain();
      return;
    }
    p.persisted = true;
    _emit(outboxEnqueued(p.entry));
    _notifyPending();
    if (drain) _scheduleDrain();
  }

  // ---------------------------------------------------------------- replay

  void _setOnline(bool online) {
    _online = online;
    if (!online) return;
    _backoff = _initialBackoff;
    _retryTimer?.cancel();
    _retryTimer = null;
    _scheduleDrain();
  }

  /// Arms the backoff timer. While it is armed nothing replays: the network
  /// or the server just refused a write. Reconnecting cancels it.
  void _scheduleRetry({Duration? atLeast}) {
    if (_retryTimer != null || _disposed || _session == null) return;
    // Up to 20 percent either way, so clients that lost the same server do
    // not all come back at once. Retry-After is never undercut.
    final jitter = 1 + (_random() - 0.5) * 0.4;
    var delay = Duration(
      microseconds: (_backoff.inMicroseconds * jitter).round(),
    );
    if (atLeast != null && atLeast > delay) delay = atLeast;
    if (delay > _maxBackoff) delay = _maxBackoff;
    final doubled = _backoff * 2;
    _backoff = doubled > _maxBackoff ? _maxBackoff : doubled;
    _retryTimer = Timer(delay, () {
      _retryTimer = null;
      _scheduleDrain();
    });
  }

  /// Schedules a drain. Offline or during the backoff only a forced replay
  /// (see [replay]) can run.
  void _scheduleDrain() {
    if (_session == null || _disposed) return;
    if ((!_online || _retryTimer != null) && _nextForced() == null) return;
    scheduleMicrotask(() => unawaited(_drain()));
  }

  /// Sends stored writes one at a time: a forced replay first, then, while
  /// online and not backing off, the next write of each lane in order.
  Future<void> _drain() async {
    if (_draining) return;
    _draining = true;
    final epoch = _epoch;
    try {
      while (_session != null && !_disposed && epoch == _epoch) {
        final next =
            _nextForced() ??
            (_online && _retryTimer == null ? _nextReplayable() : null);
        if (next == null) break;
        final bool sent;
        try {
          sent = await _replay(next);
        } finally {
          // Attempted once, whatever came of it; replay reads the outcome.
          _settleForced(next);
        }
        if (!sent && _nextForced() == null) break;
      }
    } finally {
      // A suspension reset the flag for the next principal's drain.
      if (epoch == _epoch) {
        _draining = false;
        // A replay forced while this drain was finishing.
        if (_nextForced() != null) _scheduleDrain();
      }
    }
  }

  /// The first write a [replay] forced that can be sent now.
  _Pending? _nextForced() {
    for (final p in _queue) {
      if (p.forced != null &&
          p.failure == null &&
          !p.inFlight &&
          p.persisted &&
          !p.awaitingReissue &&
          p.meta != null) {
        return p;
      }
    }
    return null;
  }

  _Pending? _nextReplayable() {
    final blocked = <String>{};
    for (final p in _queue) {
      if (blocked.contains(p.lane)) continue;
      if (p.failure != null ||
          p.inFlight ||
          !p.persisted ||
          p.awaitingReissue ||
          p.meta == null) {
        blocked.add(p.lane);
        continue;
      }
      return p;
    }
    return null;
  }

  Future<bool> _replay(_Pending p) async {
    final session = _session;
    final meta = p.meta;
    if (session == null ||
        meta == null ||
        p.epoch != _epoch ||
        !identical(_cache.session, session)) {
      return false;
    }
    p
      ..held = null
      ..storageError = null;
    if (!_credentialsMatch(session)) {
      p.held = _held(p, OutboxOfflineCause.credentialsHeld);
      _scheduleRetry();
      return false;
    }

    if (_pastWindow(p.entry, meta)) {
      await _fail(session, p, _expired(p));
      return true;
    }

    final queued = p.entry;
    // An outstanding attempt keeps its first send time: the server's memory
    // of the key dates from that one.
    final sending = queued.copyWith(sentAt: queued.sentAt ?? _now());
    final markError = await _mark(session, sending);
    if (markError != null) {
      p.storageError = markError;
      if (p.epoch == _epoch) _scheduleRetry();
      return false;
    }

    // The principal, or the credentials, may have changed while that write
    // was in progress. Never send one principal's write under another's
    // credentials; put the record back as it was.
    if (p.epoch != _epoch ||
        !identical(_cache.session, session) ||
        !_credentialsMatch(session)) {
      p.held = _held(p, OutboxOfflineCause.credentialsHeld);
      await _mark(session, queued);
      if (p.epoch == _epoch) _scheduleRetry();
      return false;
    }

    // Completed once the outcome is recorded, not when the response
    // arrives, so a replay waiting on it reads the final state.
    final attempt = Completer<void>();
    p
      ..entry = sending
      ..inFlight = true
      ..sending = attempt;
    try {
      final Object? response;
      try {
        response = await _transport.inner.execute(_requestFor(p));
      } on Object catch (error) {
        p.inFlight = false;
        _attemptEnded(p);
        return await _replayFailed(session, p, meta, error);
      }
      p.inFlight = false;
      _attemptEnded(p);

      await _succeed(session, p, _accepted(p, response));
      return true;
    } finally {
      if (identical(p.sending, attempt)) p.sending = null;
      attempt.complete();
    }
  }

  Future<bool> _replayFailed(
    StorageSession session,
    _Pending p,
    OperationMeta meta,
    Object error,
  ) async {
    final status = statusOf(error);
    if (status != null) {
      if (_retryableStatus(error)) {
        p.held = _held(p, OutboxOfflineCause.retryableStatus, status: status);
        await _unsend(session, p);
        return _backOff(session, p, error, atLeast: _retryAfter(error));
      }
      await _fail(
        session,
        p,
        _failureForStatus(
          p,
          status,
          error is HttpStatusError ? error.body : null,
        ),
      );
      return true;
    }

    final kind = classifyNetworkError(error);
    switch (kind) {
      case null:
        await _fail(
          session,
          p,
          OutboxValidation(
            mutationId: p.entry.id,
            operationId: p.entry.operationId,
            control: _control,
            status: 0,
            body: error.toString(),
          ),
        );
        return true;
      // The principal changed while it was out: the client cancelled it.
      // It may have left, so it stays marked sent in that principal's
      // storage.
      case NetworkFailure.cancelled when p.epoch != _epoch:
        _reject(p, const OutboxSuspended());
        return false;
      // The only cancel a replay carries is the principal change above, so
      // any other abort came from below and the request never got its
      // answer: treated like one never sent.
      case NetworkFailure.notSent || NetworkFailure.cancelled:
        p.held = _held(p, OutboxOfflineCause.offline);
        await _unsend(session, p);
        return _backOff(session, p, error);
      case NetworkFailure.uncertain
          when isSafeToRepeat(meta) && !_pastWindow(p.entry, meta):
        p.held = _held(p, OutboxOfflineCause.offline);
        return _backOff(session, p, error);
      case NetworkFailure.uncertain:
        await _fail(
          session,
          p,
          OutboxUncertain(
            mutationId: p.entry.id,
            operationId: p.entry.operationId,
            control: _control,
            reason: 'the request was sent and no response arrived ($error)',
          ),
        );
        return true;
    }
  }

  /// After a retryable failure of [p]: backs off before the next attempt,
  /// or, once [p] has failed retryably [_maxRetryableFailures] times in a
  /// row, surfaces it as [OutboxUncertain] with its record and key kept.
  /// When the principal changed while the request was out, releases its
  /// caller instead: the record is in that principal's storage. Always false:
  /// the drain stops until the backoff ends.
  Future<bool> _backOff(
    StorageSession session,
    _Pending p,
    Object error, {
    Duration? atLeast,
  }) async {
    if (p.epoch != _epoch) {
      _reject(p, const OutboxSuspended());
      return false;
    }
    p.retryableFailures++;
    if (p.retryableFailures >= _maxRetryableFailures) {
      await _fail(
        session,
        p,
        OutboxUncertain(
          mutationId: p.entry.id,
          operationId: p.entry.operationId,
          control: _control,
          reason:
              'gave up after ${p.retryableFailures} attempts that each failed '
              'retryably; the last: $error',
        ),
      );
    }
    // Armed either way, so other lanes' writes still replay after it.
    if (p.epoch == _epoch) _scheduleRetry(atLeast: atLeast);
    return false;
  }

  /// 408, 429 and 5xx: the idempotency middleware released the key, so the
  /// same key runs the handler again. A 409 that carries `Retry-After` is
  /// the middleware saying the same key is still in flight, unless it is a
  /// replay (`Idempotent-Replayed`): then it is the handler's own 409, stored
  /// with whatever headers the handler set, and final.
  static bool _retryableStatus(Object error) {
    final status = statusOf(error);
    if (status == null) return false;
    if (status == 408 || status == 429 || status >= 500) return true;
    return status == 409 &&
        error is HttpStatusError &&
        headerValue(error.headers, 'retry-after') != null &&
        headerValue(error.headers, idempotentReplayedHeader) == null;
  }

  /// The `Retry-After` delay, in seconds, when the error carries one. An
  /// HTTP-date value is not parsed: the backoff applies instead.
  static Duration? _retryAfter(Object error) {
    if (error is! HttpStatusError) return null;
    final raw = headerValue(error.headers, 'retry-after');
    final seconds = raw == null ? null : int.tryParse(raw.trim());
    if (seconds == null || seconds < 0) return null;
    return Duration(seconds: seconds);
  }

  OutboxFailure _failureForStatus(_Pending p, int status, Object? body) {
    final id = p.entry.id;
    final op = p.entry.operationId;
    return switch (status) {
      409 || 412 => OutboxConflict(
        mutationId: id,
        operationId: op,
        control: _control,
        status: status,
        body: body,
      ),
      401 || 403 => OutboxUnauthorized(
        mutationId: id,
        operationId: op,
        control: _control,
        status: status,
      ),
      404 || 410 => OutboxGone(
        mutationId: id,
        operationId: op,
        control: _control,
        status: status,
        body: body,
      ),
      _ => OutboxValidation(
        mutationId: id,
        operationId: op,
        control: _control,
        status: status,
        body: body,
      ),
    };
  }

  Future<void> _succeed(
    StorageSession session,
    _Pending p,
    Object? response,
  ) async {
    try {
      await session.remove(p.entry.id);
    } on Object catch (error) {
      // The record stays and replays next start with the same key; the
      // server's idempotency store answers it.
      _report(error, 'outbox.remove');
    }
    p.sent = true;
    if (p.epoch != _epoch) {
      _complete(p, response);
      return;
    }
    _backoff = _initialBackoff;
    _queue.remove(p);
    _complete(p, response);
    _emit(outboxReplayed(p.entry));
    _notifyPending();
  }

  Future<void> _unsend(StorageSession session, _Pending p) async {
    final queued = p.entry.copyWith(clearSentAt: true);
    if (await _mark(session, queued) == null && p.epoch == _epoch) {
      p.entry = queued;
    }
  }

  Future<void> _fail(
    StorageSession session,
    _Pending p,
    OutboxFailure failure, {
    Object? originalError,
  }) async {
    final failed = p.entry.copyWith(
      failureJson: jsonEncode(
        failure.toJson(),
        toEncodable: (Object? o) => o.toString(),
      ),
    );
    final error = p.persisted || p.recorded
        ? await _mark(session, failed)
        : await _insert(session, failed);
    if (error == null) p.recorded = true;

    if (p.epoch != _epoch) {
      // Stored in the previous principal's outbox, where they will find it.
      // Nothing about it reaches the current principal's failures.
      _reject(
        p,
        error == null ? const OutboxSuspended() : (originalError ?? failure),
      );
      return;
    }

    if (error != null) {
      _queue.remove(p);
      _reject(p, originalError ?? failure);
      _notifyPending();
      return;
    }

    p
      ..entry = failed
      ..persisted = true
      ..failure = failure;
    _currentFailures.add(failure);
    _reject(p, failure);
    _emit(outboxFailed(p.entry, failure));
    if (!_failures.isClosed) _failures.add(failure);
    _notifyPending();
  }

  // ---------------------------------------------------------------- actions

  Future<void> _retry(String id) async {
    final p = _byId(id);
    final session = _session;
    final meta = p?.meta;
    if (p == null || p.failure == null || session == null || meta == null) {
      return;
    }
    if (_refuseOwned(meta, 'retry')) return;

    final queued = _requeued(p.entry, p.failure!);
    if (await _mark(session, queued) != null || p.epoch != _epoch) return;

    p
      ..entry = queued
      ..retryableFailures = 0;
    _clearFailure(p);
    // An explicit user action: it does not wait out the backoff. The level
    // is kept, so a repeat failure re-arms where the backoff had reached.
    _retryTimer?.cancel();
    _retryTimer = null;
    p.completer = _parked();
    _reissue(p);
    _notifyPending();
  }

  /// Removes write [id] and releases its caller. Returns the storage error
  /// when the record could not be removed (the write then stays).
  Future<Object?> _discard(String id) async {
    final p = _byId(id);
    final session = _session;
    if (p == null || session == null || p.inFlight) return null;

    try {
      await session.remove(id);
    } on Object catch (error) {
      _report(error, 'outbox.discard');
      return error;
    }
    if (p.epoch != _epoch) return null;

    _queue.remove(p);
    _clearFailure(p);
    _settleForced(p);
    _reject(p, const OutboxDiscarded());
    _notifyPending();
    _scheduleDrain();
    return null;
  }

  Future<void> _edit(String id, TagContext args) async {
    final p = _byId(id);
    final session = _session;
    final meta = p?.meta;
    if (p == null || p.failure == null || session == null || meta == null) {
      return;
    }
    if (_refuseOwned(meta, 'edit')) return;

    // A new record first, then the old one goes: a crash between the two
    // leaves the old failure visible, never a lost write.
    final OutboxEntry replacement;
    try {
      replacement = p.entry.copyWith(
        id: _uuid.v4(),
        args: args,
        intent: _overlayIntent(meta, args),
        idempotencyKey: _uuid.v4(),
        clearFailure: true,
        clearSentAt: true,
      );
    } on Object catch (error) {
      throw OutboxUnavailable(error);
    }
    final error = await _insert(session, replacement);
    if (error != null) throw OutboxUnavailable(error);
    try {
      await session.remove(id);
    } on Object catch (error) {
      _report(error, 'outbox.edit');
    }
    if (p.epoch != _epoch) return;

    final next = _Pending(
      entry: replacement,
      meta: meta,
      lane: p.lane,
      epoch: _epoch,
      persisted: true,
      completer: _parked(),
    );
    final index = _queue.indexOf(p);
    if (index >= 0) {
      _queue[index] = next;
    } else {
      _queue.add(next);
    }
    _clearFailure(p);
    _emit(outboxEnqueued(replacement));
    _reissue(next);
    _notifyPending();
  }

  /// The re-issued mutation of [p] reached the transport, or failed before
  /// it could: either way it is no longer awaited.
  static void _reissueArrived(_Pending p) {
    p.awaitingReissue = false;
    _signal(p.reissued);
  }

  /// Why [p]'s last replay attempt left it queued.
  static OutboxOffline _held(
    _Pending p,
    OutboxOfflineCause cause, {
    int? status,
  }) => OutboxOffline(p.entry.id, cause: cause, status: status);

  static void _settleForced(_Pending p) {
    final forced = p.forced;
    p.forced = null;
    _signal(forced);
  }

  static void _signal(Completer<void>? completer) {
    if (completer != null && !completer.isCompleted) completer.complete();
  }

  void _clearFailure(_Pending p) {
    final failure = p.failure;
    if (failure != null) _currentFailures.remove(failure);
    p.failure = null;
  }

  /// [entry], cleared of [failure] and ready to send again. When the server
  /// answered with a final status (a conflict, a validation error, an auth
  /// refusal, a gone resource), it stored that answer under the key and
  /// would replay it for the whole TTL, so the retry goes out under a new
  /// key. An uncertain failure, or one with status 0 that the server never
  /// gave, keeps the key: the server may hold the write's real outcome.
  OutboxEntry _requeued(OutboxEntry entry, OutboxFailure failure) {
    final queued = entry.copyWith(clearFailure: true, clearSentAt: true);
    final answered = switch (failure) {
      OutboxConflict(:final status) ||
      OutboxValidation(:final status) ||
      OutboxUnauthorized(:final status) ||
      OutboxGone(:final status) => status != 0,
      OutboxUncertain() => false,
    };
    return answered ? queued.withRotatedKey(_uuid.v4()) : queued;
  }

  /// Whether [entry] was sent longer ago than the idempotency window and is
  /// safe to repeat only because the server remembers its key: that memory
  /// may be gone (the TTL passed, or an in-memory store restarted), so a
  /// replay could apply it twice.
  bool _pastWindow(OutboxEntry entry, OperationMeta meta) {
    final sentAt = entry.sentAt;
    return sentAt != null &&
        isSafeOnlyByKey(meta) &&
        _now().difference(sentAt) > _idempotencyWindow;
  }

  OutboxUncertain _expired(_Pending p) => OutboxUncertain(
    mutationId: p.entry.id,
    operationId: p.entry.operationId,
    control: _control,
    reason:
        'this write was sent more than ${_idempotencyWindow.inHours} hours '
        'ago and its response never arrived; the server may no longer '
        'remember its Idempotency-Key, so it is not replayed automatically',
  );

  /// Drops [p]'s attempt cancel once its request has ended.
  void _attemptEnded(_Pending p) {
    final cancel = p.attemptCancel;
    p.attemptCancel = null;
    if (cancel != null) _attemptCancels.remove(cancel);
  }

  /// Reads the idempotency middleware's headers on every response to [p].
  void _onResponse(_Pending p, Map<String, String> headers) {
    final skipped = headerValue(headers, idempotencySkippedHeader);
    if (skipped != null) {
      // No principal names here: this text may reach logs.
      _report(
        StateError(
          '${p.entry.operationId}: the server did not deduplicate this write '
          '($idempotencySkippedHeader: $skipped). The route is marked idempotent, but '
          'the request reached the idempotency middleware without a '
          'principal; register auth before it. A replay may run twice.',
        ),
        'outbox.idempotency-skipped',
      );
    }
    p.truncated = headerValue(headers, idempotentTruncatedHeader) == 'true';
  }

  /// What a successful attempt of [p] completes with. A truncated replay
  /// (the stored response was over the server's size limit) has no body:
  /// it is not decoded, and the entity is invalidated so the next read
  /// refetches what the write did.
  Object? _accepted(_Pending p, Object? response) {
    if (!p.truncated) return response;
    p.truncated = false;
    final meta = p.meta;
    final entity = meta?.entity;
    if (entity != null && p.epoch == _epoch) {
      final tags = <String>[
        '$entity[]',
        ?resolveTag('$entity:{id}', p.entry.args),
      ];
      // After the cache has committed the (empty) response, so the refetch
      // replaces whatever the optimistic overlay left behind.
      Timer.run(() {
        if (!_disposed && p.epoch == _epoch) _cache.invalidate(tags);
      });
    }
    return null;
  }

  // ---------------------------------------------------------------- helpers

  /// Whether the credentials the transport holds belong to [session]'s
  /// principal. Always true without an `authPrincipal`.
  bool _credentialsMatch(StorageSession session) {
    final auth = _authPrincipal;
    if (auth == null) return true;

    String? current;
    try {
      current = auth();
    } on Object catch (error) {
      _report(error, 'outbox.authPrincipal');
      return false;
    }
    if (current == session.principal) {
      _heldForCredentials = false;
      return true;
    }

    if (!_heldForCredentials) {
      _heldForCredentials = true;
      // No principal names here: this text may reach logs.
      _report(
        StateError(
          'the transport credentials belong to a different principal than '
          'the outbox; replay is held until they agree. Call setPrincipal '
          'before swapping credentials.',
        ),
        'outbox.principal',
      );
    }
    return false;
  }

  /// True, after reporting it, when [meta]'s entity is owned by a sync
  /// source: the cache would hand a re-issued write to that source, so
  /// `retry` and `edit` leave the failure and its record as they are. Only
  /// `discard` applies to such a write.
  bool _refuseOwned(OperationMeta meta, String action) {
    if (!_ownedBySource(meta)) return false;
    _report(
      StateError(
        '$action refused: ${meta.entity} is owned by a sync source, which '
        'sends its own writes; discard this one instead',
      ),
      'outbox.$action',
    );
    return true;
  }

  /// Whether a sync source owns [meta]'s entity, so the cache sends its
  /// writes to that source and never to the transport.
  bool _ownedBySource(OperationMeta meta) {
    final entity = meta.entity;
    return entity != null && _cache.owns(entity);
  }

  String _laneOf(OperationMeta meta) => meta.entity ?? 'op:${meta.id}';

  _Pending? _byId(String id) {
    for (final p in _queue) {
      if (p.entry.id == id) return p;
    }
    return null;
  }

  DateTime _now() =>
      DateTime.fromMillisecondsSinceEpoch(_clock.now(), isUtc: true);

  /// The request for one attempt. Every attempt carries the entry's stable
  /// `Idempotency-Key`: stored headers never hold it (see
  /// [persistableHeaders]), so it is added here each time.
  ///
  /// Its cancel completes when the principal changes (and when [cancel], the
  /// caller's own, does). Its responses are read for the idempotency
  /// headers.
  TransportRequest _requestFor(
    _Pending p, {
    Future<void>? cancel,
    bool live = false,
  }) {
    final base = live ? p.liveHeaders : p.entry.requestHeaders;
    final leaving = Completer<void>();
    _attemptCancels.add(leaving);
    p
      ..attemptCancel = leaving
      ..truncated = false;
    return TransportRequest(
      meta: p.meta!,
      args: p.entry.args,
      headers: {
        for (final MapEntry(:key, :value) in base.entries)
          if (key.toLowerCase() != idempotencyKeyHeader.toLowerCase() &&
              key.toLowerCase() != outboxReplayHeader)
            key: value,
        idempotencyKeyHeader: p.entry.idempotencyKey,
      },
      cancel: cancel == null
          ? leaving.future
          : Future.any([cancel, leaving.future]),
      onResponse: (_, headers) => _onResponse(p, headers),
    );
  }

  Future<Object?> _insert(StorageSession session, OutboxEntry entry) async {
    try {
      await session.enqueue(entry.toRecord());
      return null;
    } on Object catch (error) {
      _report(error, 'outbox.enqueue');
      return error;
    }
  }

  Future<Object?> _mark(StorageSession session, OutboxEntry entry) async {
    try {
      await session.updateState(entry.id, entry.stateJson);
      return null;
    } on Object catch (error) {
      _report(error, 'outbox.state');
      return error;
    }
  }

  /// A completer whose errors never count as unhandled: until the cache
  /// receives its future, nobody else is listening.
  static Completer<Object?> _parked() {
    final completer = Completer<Object?>();
    completer.future.ignore();
    return completer;
  }

  static void _complete(_Pending p, Object? value) {
    final completer = p.completer;
    p.completer = null;
    if (completer != null && !completer.isCompleted) completer.complete(value);
  }

  static void _reject(_Pending p, Object error, [StackTrace? stack]) {
    final completer = p.completer;
    p.completer = null;
    if (completer != null && !completer.isCompleted) {
      completer.completeError(error, stack);
    }
  }

  void _emit(CacheEvent event) {
    final observer = _cache.observer;
    if (observer != null) observer(event);
  }

  int _storedCount() => _queue.where((p) => p.persisted).length;

  void _notifyPending() {
    if (_pendingCount.isClosed) return;
    _pendingCount.add(_storedCount());
  }

  void _report(Object error, String context) =>
      _onError?.call(error, 'forge_client_offline: $context');
}

/// One write the client is tracking.
final class _Pending {
  _Pending({
    required this.entry,
    required this.meta,
    required this.lane,
    required this.epoch,
    this.completer,
    this.persisted = false,
    this.liveHeaders = const {},
  });

  OutboxEntry entry;
  final OperationMeta? meta;
  final String lane;

  /// The client's epoch when this write was admitted.
  final int epoch;
  Completer<Object?>? completer;

  /// Whether the write is shown as queued ([OfflineClient.pending]).
  bool persisted;

  /// Whether a record for the write exists in storage. A direct send stores
  /// one before it goes out, without showing it as queued.
  bool recorded = false;

  /// Whether the latest response was a truncated idempotent replay.
  bool truncated = false;

  /// The cancel of the attempt on the wire, completed by a principal change.
  Completer<void>? attemptCancel;
  bool inFlight = false;
  bool awaitingReissue = false;

  /// Consecutive retryable failures since the write was admitted or retried.
  int retryableFailures = 0;

  /// Whether the server accepted this write.
  bool sent = false;

  /// Completes when the current re-issue through the cache reaches the
  /// transport (or fails before it could).
  Completer<void>? reissued;

  /// Completes when the replay attempt on the wire has its outcome recorded.
  Completer<void>? sending;

  /// Set by [OfflineClient.replay]: the drain sends this write next and
  /// completes it after the attempt.
  Completer<void>? forced;

  /// Why the last replay attempt left this write queued, when it did.
  OutboxOffline? held;

  /// The storage error that stopped the last replay attempt, when one did.
  Object? storageError;
  OutboxFailure? failure;
  final Map<String, String> liveHeaders;
}

final class _Control implements OutboxControl {
  _Control(this._client);

  final OfflineClient _client;

  @override
  Future<void> retry(String mutationId) => _client._retry(mutationId);

  @override
  Future<void> discard(String mutationId) async {
    await _client._discard(mutationId);
  }

  @override
  Future<void> edit(String mutationId, TagContext args) =>
      _client._edit(mutationId, args);
}

/// The connectivity [OfflineClient.open] assumes when given none: always
/// online, so writes are attempted and queued only when a request fails.
final class _AssumeOnline implements ConnectivitySignal {
  const _AssumeOnline();

  @override
  Stream<bool> get online => const Stream<bool>.empty();
}
