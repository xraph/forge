import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:uuid/uuid.dart';

import 'outbox/events.dart';
import 'outbox/network_error.dart';
import 'outbox/network_failure.dart';
import 'outbox/outbox_entry.dart';
import 'outbox/outbox_failure.dart';
import 'outbox/outbox_transport.dart';
import 'outbox/overlay_intent.dart';

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
/// Principals: call `cache.setPrincipal(next)` before swapping the
/// credentials the transport sends. The outbox suspends synchronously when
/// the principal starts changing, so no listener sees the previous
/// principal's queue, and resumes when the next principal's session opens.
/// Pass `authPrincipal` (who the transport's credentials currently belong
/// to) and the outbox also refuses to send a write while the two disagree,
/// which covers an app that swaps credentials first. Without it, an app that
/// swaps credentials before calling `setPrincipal` can have a write that was
/// already due go out under the next principal's credentials.
final class OfflineClient {
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
  /// to right now. Before every send the outbox compares it with the
  /// principal whose write it is, and on a mismatch it holds the write and
  /// checks again after a backoff, on reconnect and on the next session.
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
  }) : _cache = cache,
       _transport = transport,
       _initialBackoff = initialBackoff,
       _excluded = excludedEntities,
       _online = initiallyOnline,
       _backoff = initialBackoff {
    _control = _Control(this);
    transport.attach(_handle);
    _subscriptions
      ..add(connectivity.online.listen(_setOnline))
      ..add(cache.sessionChanges.listen(_onSession));
    // Synchronous, before the cache clears: nothing told of the change may
    // see this principal's queue. Only in-memory state moves here.
    _unwatchChanging = cache.watchPrincipalChanging((_) => _suspend());

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

  late final _Control _control;
  late final void Function() _unwatchChanging;
  final List<StreamSubscription<Object?>> _subscriptions = [];
  static const Uuid _uuid = Uuid();

  final List<_Pending> _queue = [];
  final List<OutboxFailure> _currentFailures = [];
  final StreamController<OutboxFailure> _failures =
      StreamController<OutboxFailure>.broadcast();
  final StreamController<int> _pendingCount = StreamController<int>.broadcast();

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

  /// Bumped by every suspension. Work that started under an earlier epoch
  /// belongs to a principal this client no longer serves: it may still
  /// finish against that principal's own storage, but it never touches the
  /// in-memory queue, the failures or the counts again.
  int _epoch = 0;

  /// The cache this client queues writes for.
  QueryCache get cache => _cache;

  /// Failures as they happen, including ones restored from storage.
  /// Broadcast: a screen and a test may both listen.
  Stream<OutboxFailure> get failures => _failures.stream;

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

  /// Completes when the current session's outbox has been restored. With no
  /// session it completes at once. Safe to call more than once.
  Future<void> restore() {
    final session = _cache.session;
    if (session == null || session.principal != _cache.principal) {
      return Future<void>.value();
    }
    return _restoreSession(session);
  }

  /// Detaches from the transport and stops listening. Parked callers get
  /// [OutboxSuspended]; stored writes are kept.
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
    await _loadOutbox(session);
    _scheduleDrain();
  }

  /// Forgets everything in memory about the current principal. Synchronous
  /// and storage-free, so it is safe inside `watchPrincipalChanging`: the
  /// records stay in that principal's storage. Parked callers get
  /// [OutboxSuspended]; a request already on the wire finishes, and its
  /// caller gets the server's answer.
  void _suspend() {
    _epoch++;
    _retryTimer?.cancel();
    _retryTimer = null;
    _draining = false;
    _heldForCredentials = false;
    _backoff = _initialBackoff;

    final parked = List<_Pending>.of(_queue);
    _queue.clear();
    _currentFailures.clear();
    for (final p in parked) {
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
    p.awaitingReissue = true;
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
            p.awaitingReissue = false;
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
        p.awaitingReissue = false;
        _scheduleDrain();
        return parked.future;
      }
      return _transport.inner.execute(withoutReplayMarker(request));
    }

    final meta = request.meta;
    final method = meta.method.toUpperCase();
    if (method == 'GET' ||
        method == 'HEAD' ||
        _operations[meta.id] == null ||
        _excluded.contains(meta.entity)) {
      return _transport.inner.execute(request);
    }

    return _track(request, waitedForSwitch: false);
  }

  Future<Object?> _track(
    TransportRequest request, {
    required bool waitedForSwitch,
  }) {
    // A principal is set and its session is not open yet (or the cache still
    // holds the previous one): a switch is in progress. The write belongs to
    // the next principal, so it waits for their session rather than going
    // out untracked.
    final principal = _cache.principal;
    final current = _cache.session;
    if (!waitedForSwitch &&
        principal != null &&
        (current == null || current.principal != principal)) {
      return _cache.idle.then((_) => _track(request, waitedForSwitch: true));
    }

    // No storage, no principal, or a session that failed to open.
    if (current == null || current.principal != principal) {
      return _transport.inner.execute(request);
    }

    if (!identical(_session, current)) unawaited(_restoreSession(current));

    final restoring = _restoring;
    if (!_restored && restoring != null) {
      return restoring.then(
        (_) => _track(request, waitedForSwitch: waitedForSwitch),
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
      entry = OutboxEntry(
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
      entry.toRecord();
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

    p.inFlight = true;
    final Object? response;
    try {
      response = await _transport.inner.execute(
        _requestFor(p, cancel: cancel, live: true),
      );
    } on Object catch (error, stack) {
      p.inFlight = false;
      await _directFailed(session, p, error, stack);
      return;
    }
    p.inFlight = false;
    if (p.epoch != _epoch) {
      _complete(p, response);
      return;
    }
    _queue.remove(p);
    _complete(p, response);
    _scheduleDrain();
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
    final retryableStatus = status != null && _retryableStatus(error);
    if ((status != null && !retryableStatus) ||
        (status == null &&
            (kind == null || kind == NetworkFailure.cancelled))) {
      if (p.epoch == _epoch) _queue.remove(p);
      _reject(p, error, stack);
      if (p.epoch == _epoch) _scheduleDrain();
      return;
    }

    if (kind == NetworkFailure.uncertain) {
      p.entry = p.entry.copyWith(sentAt: _now());
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
      _scheduleRetry(atLeast: _retryAfter(error));
    }
  }

  Future<void> _park(
    StorageSession session,
    _Pending p, {
    bool drain = true,
  }) async {
    final error = await _insert(session, p.entry);
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
    var delay = _backoff;
    if (atLeast != null && atLeast > delay) delay = atLeast;
    if (delay > _maxBackoff) delay = _maxBackoff;
    final doubled = _backoff * 2;
    _backoff = doubled > _maxBackoff ? _maxBackoff : doubled;
    _retryTimer = Timer(delay, () {
      _retryTimer = null;
      _scheduleDrain();
    });
  }

  void _scheduleDrain() {
    if (!_online || _session == null || _disposed || _retryTimer != null) {
      return;
    }
    scheduleMicrotask(() => unawaited(_drain()));
  }

  Future<void> _drain() async {
    if (_draining) return;
    _draining = true;
    final epoch = _epoch;
    try {
      while (_online &&
          _session != null &&
          !_disposed &&
          _retryTimer == null &&
          epoch == _epoch) {
        final next = _nextReplayable();
        if (next == null) break;
        if (!await _replay(next)) break;
      }
    } finally {
      // A suspension reset the flag for the next principal's drain.
      if (epoch == _epoch) _draining = false;
    }
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
    if (!_credentialsMatch(session)) {
      _scheduleRetry();
      return false;
    }

    final queued = p.entry;
    final sending = queued.copyWith(sentAt: _now());
    if (await _mark(session, sending) != null) {
      if (p.epoch == _epoch) _scheduleRetry();
      return false;
    }

    // The principal, or the credentials, may have changed while that write
    // was in progress. Never send one principal's write under another's
    // credentials; put the record back as it was.
    if (p.epoch != _epoch ||
        !identical(_cache.session, session) ||
        !_credentialsMatch(session)) {
      await _mark(session, queued);
      if (p.epoch == _epoch) _scheduleRetry();
      return false;
    }

    p
      ..entry = sending
      ..inFlight = true;
    final Object? response;
    try {
      response = await _transport.inner.execute(_requestFor(p));
    } on Object catch (error) {
      p.inFlight = false;
      return _replayFailed(session, p, meta, error);
    }
    p.inFlight = false;

    await _succeed(session, p, response);
    return true;
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
        await _unsend(session, p);
        _retryLater(p, atLeast: _retryAfter(error));
        return false;
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
      // A replay carries no cancel signal, so an abort here came from below
      // and the request never got its answer: treated like one never sent.
      case NetworkFailure.notSent || NetworkFailure.cancelled:
        await _unsend(session, p);
        _retryLater(p);
        return false;
      case NetworkFailure.uncertain when isSafeToRepeat(meta):
        _retryLater(p);
        return false;
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

  /// Backs off before the next attempt, or, when the principal changed while
  /// the request was out, releases its caller: the record is in that
  /// principal's storage.
  void _retryLater(_Pending p, {Duration? atLeast}) {
    if (p.epoch == _epoch) {
      _scheduleRetry(atLeast: atLeast);
    } else {
      _reject(p, const OutboxSuspended());
    }
  }

  /// 408, 429 and 5xx: the idempotency middleware released the key, so the
  /// same key runs the handler again. A 409 that carries `Retry-After` is
  /// the middleware saying the same key is still in flight.
  static bool _retryableStatus(Object error) {
    final status = statusOf(error);
    if (status == null) return false;
    if (status == 408 || status == 429 || status >= 500) return true;
    return status == 409 &&
        error is HttpStatusError &&
        headerValue(error.headers, 'retry-after') != null;
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
    final error = p.persisted
        ? await _mark(session, failed)
        : await _insert(session, failed);

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
    if (p == null || p.failure == null || session == null || p.meta == null) {
      return;
    }

    final queued = p.entry.copyWith(clearFailure: true, clearSentAt: true);
    if (await _mark(session, queued) != null || p.epoch != _epoch) return;

    p.entry = queued;
    _clearFailure(p);
    p.completer = _parked();
    _reissue(p);
    _notifyPending();
  }

  Future<void> _discard(String id) async {
    final p = _byId(id);
    final session = _session;
    if (p == null || session == null || p.inFlight) return;

    try {
      await session.remove(id);
    } on Object catch (error) {
      _report(error, 'outbox.discard');
      return;
    }
    if (p.epoch != _epoch) return;

    _queue.remove(p);
    _clearFailure(p);
    _reject(p, const OutboxDiscarded());
    _notifyPending();
    _scheduleDrain();
  }

  Future<void> _edit(String id, TagContext args) async {
    final p = _byId(id);
    final session = _session;
    final meta = p?.meta;
    if (p == null || p.failure == null || session == null || meta == null) {
      return;
    }

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

  void _clearFailure(_Pending p) {
    final failure = p.failure;
    if (failure != null) _currentFailures.remove(failure);
    p.failure = null;
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
  TransportRequest _requestFor(
    _Pending p, {
    Future<void>? cancel,
    bool live = false,
  }) {
    final base = live ? p.liveHeaders : p.entry.requestHeaders;
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
      cancel: cancel,
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
  bool persisted;
  bool inFlight = false;
  bool awaitingReissue = false;
  OutboxFailure? failure;
  final Map<String, String> liveHeaders;
}

final class _Control implements OutboxControl {
  _Control(this._client);

  final OfflineClient _client;

  @override
  Future<void> retry(String mutationId) => _client._retry(mutationId);

  @override
  Future<void> discard(String mutationId) => _client._discard(mutationId);

  @override
  Future<void> edit(String mutationId, TagContext args) =>
      _client._edit(mutationId, args);
}
