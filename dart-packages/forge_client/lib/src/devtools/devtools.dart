/// The inspector: the recorder that claims `cache.observer`, and the methods a
/// panel calls. Port of `client-devtools/src/devtools.ts`.
library;

import '../cache.dart';
import '../observe.dart';
import '../operation.dart';
import '../storage.dart' show PendingMutationRecord;
import '../tags.dart';
import '../transport.dart';
import '../types.dart' show Json;
import 'actions.dart';
import 'control.dart';
import 'explain.dart' as ex;
import 'explain.dart' show MissCause, TagsCause, argsKey, causeOf;
import 'frames.dart';
import 'inspect.dart' as ins;
import 'inspect.dart' show EntityFilter;
import 'log.dart';
import 'requests.dart';
import 'seams.dart';
import 'sources.dart';
import 'types.dart';

/// Turns frame capture on. Presence is the switch; [limit] defaults to 200.
final class FrameOptions {
  /// Creates the options.
  const FrameOptions({this.limit = 200});

  /// How many frames to keep. Zero or less leaves capture off.
  final int limit;
}

/// Starts observing [cache]. Chains to an observer already in the slot and
/// gives the slot back on [Devtools.dispose] if it is still ours.
///
/// Nothing crosses principals: when the cache's principal changes, the log, the
/// frame ring and the [requests] log are purged (one marker each is left), the
/// outbox and sync mirrors are emptied, and
/// an armed failure on [controls] is disarmed and its delayed requests are
/// aborted, before the cache drops the
/// previous principal's records, so none of what was recorded for one user is
/// readable once the next is in charge. A query a component is still
/// watching is re-mounted by the cache for the new principal under the same
/// key, so that key (and a value in it) reappears in the new session's log
/// exactly as it does in the cache's own registry.
Devtools attach(
  QueryCache cache, {
  int limit = 500,
  Clock clock = realClock,
  int argsLimit = 200,
  FrameOptions? frames,
  RequestLog? requests,
  ControlledTransport? controls,
  Revalidation? revalidation,
  OutboxInspector? outbox,
}) => Devtools._(
  cache,
  limit: limit,
  clock: clock,
  argsLimit: argsLimit,
  frames: frames,
  requestLog: requests,
  controls: controls,
  revalidation: revalidation,
  outboxInspector: outbox,
);

/// The inspector over one cache.
final class Devtools {
  Devtools._(
    this.cache, {
    required int limit,
    required this._clock,
    required this._argsLimit,
    required FrameOptions? frames,
    required this.requestLog,
    required this.controls,
    required this.revalidation,
    required this.outboxInspector,
  }) : _log = EventLog(capacity: limit, clock: _clock),
       _ring = _ringFor(frames) {
    _previous = cache.observer;
    _owner = cache.principal;
    cache.observer = _observer;
    _stopWatching = cache.watchPrincipalChanging(_onChanging);
    _stopSettled = cache.watchPrincipal(_onSettled);
  }

  /// How many query keys the recorder tracks before it prunes.
  static const _trackLimit = 512;

  /// The cache under inspection.
  final QueryCache cache;

  /// The request log the transport reports into, when one is wired.
  ///
  /// Not final: a later registration on a cache that is already attached fills
  /// an empty slot, and never replaces a filled one.
  RequestLog? requestLog;

  /// The network conditions, when wired. Absent means the panel shows no rail.
  /// An armed failure is cleared when the principal changes; the mode and the
  /// latency are the developer's and stay.
  ///
  /// Not final: a later registration fills an empty slot only.
  ControlledTransport? controls;

  /// The revalidation toggles, when the application registered any.
  ///
  /// Not final: a later registration fills an empty slot only.
  Revalidation? revalidation;

  /// The offline client's outbox, when the application wired one. Replaying
  /// and discarding go through it; listing does not need it. Not final: a
  /// later registration on a cache that is already attached sets it here.
  OutboxInspector? outboxInspector;

  final EventLog _log;
  final OutboxMirror _outbox = OutboxMirror();
  final SyncMirror _sync = SyncMirror();
  final Clock _clock;
  final int _argsLimit;
  FrameRing? _ring;
  CacheObserver? _previous;
  String? _owner;
  late final void Function() _stopWatching;
  late final void Function() _stopSettled;
  late final CacheObserver _observer = _observe;

  final _fetching = <String, bool>{};
  final _seen = <String>{};
  final _pending = <String, int>{};
  int _session = 0;
  bool _disposed = false;
  int? _cause;

  // A principal change is in progress from the changing notification, when the
  // recorder lets go of the previous principal's data, until the cache has
  // emptied itself. In that window the cache still holds the previous
  // principal's records, so nothing is answered from it and subscribers hear
  // nothing. `_fence` is the cache generation at the start of the window: the
  // clear bumps it first, so an event under the same generation came from the
  // previous principal and an event under a later one is the new principal's.
  bool _changing = false;
  int _fence = 0;

  static FrameRing? _ringFor(FrameOptions? options) =>
      options == null || options.limit <= 0 ? null : FrameRing(options.limit);

  DevCache get _dev => DevCache(cache);

  /// The causal log itself, for the action layer and the service host.
  /// Subscribers belong on [subscribe], which every identity change delays the
  /// same way.
  EventLog get eventLog {
    _check();
    return _log;
  }

  /// Entries the log holds before overwriting.
  int get capacity => _log.capacity;

  /// Entries overwritten.
  int get dropped => _log.dropped;

  /// Identity changes since attaching.
  int get session => _session;

  /// The log, oldest first. Empty once the inspector is disposed.
  List<LogEntry> log() {
    _check();
    return _log.entries();
  }

  /// Drops every recorded event and every captured frame. The cache is untouched.
  void clear() {
    _log.clear();
    _ring?.clear();
  }

  /// Hears each entry as it is recorded.
  ///
  /// When the identity changes, the [PrincipalLog] marker (and anything the new
  /// principal's re-mounted queries log before the change is over) arrives
  /// after the cache has emptied the previous principal's data, never while it
  /// still holds it. A subscriber that reacts to the marker by reading the
  /// cache or this inspector therefore sees nothing of the previous principal.
  void Function() subscribe(void Function(LogEntry entry) listener) =>
      _log.subscribe(listener);

  /// Why did this query refetch? Null when the log has nothing, which is also
  /// the answer while an identity change is in progress.
  RefetchReport? whyRefetched(String key) =>
      _answerable() ? ex.whyRefetched(_log, key) : null;

  /// Why did this query not refetch? With no [cause], the most recent recorded
  /// mutation or frame batch is used. While an identity change is in progress,
  /// or once the inspector is disposed, the answer is an empty report that
  /// says the query is not tracked.
  MissReport whyNotRefetched(String key, [MissCause? cause]) => _answerable()
      ? ex.whyNotRefetched(_dev, key, _missCause(cause), _log)
      : _unanswerable(key);

  /// The refetch story when it refetched, the miss story otherwise.
  Explanation explain(String key) {
    final report = whyNotRefetched(key);
    if (report.outcome == MissOutcome.refetched) {
      return whyRefetched(key) ?? report;
    }
    return report;
  }

  /// What would [meta] invalidate, and who would it reach? While an identity
  /// change is in progress, or once the inspector is disposed, no query is
  /// reached.
  InvalidationPreview wouldInvalidate(
    OperationMeta meta, [
    TagContext args = TagContext.empty,
    Object? response,
  ]) {
    if (_answerable()) return ex.wouldInvalidate(_dev, meta, args, response);

    final resolved = resolveTags(meta.invalidates, args, response);

    return InvalidationPreview(
      operation: operationName(meta),
      templates: [...meta.invalidates],
      tags: [...resolved.tags],
      unresolved: [...resolved.unresolved],
      hits: [
        for (final tag in resolved.tags) TagHit(tag: tag, queries: const []),
      ],
      missed: [...resolved.tags],
    );
  }

  /// The most recent mutation or frame batch the log still holds.
  LogEntry? lastCause() {
    _check();
    return _log.last((entry) => entry is MutationLog || entry is FramesLog);
  }

  /// The mutating half. Every other member is a read.
  ///
  /// An action throws a [StateError] while the cache is changing principal
  /// and once the inspector is disposed, and refuses sync-owned entities; see
  /// [DevtoolsActions].
  late final DevtoolsActions actions = DevtoolsActions(
    _dev,
    _log,
    () => _session,
    _answerable,
  );

  // Every read below answers from the cache as it is now and moves nothing.
  // While an identity change is in progress, and once the inspector is
  // disposed, the cache still holds the previous principal's records (or the
  // inspector is no longer entitled to say anything), so each one answers as
  // an empty cache would.
  T _ask<T>(T Function(DevCache cache) read, T empty) =>
      _answerable() ? read(_dev) : empty;

  static const _noStore = StoreSnapshot(
    records: 0,
    version: 0,
    frameVersion: 0,
    tombstones: 0,
    tracked: 0,
    remembered: 0,
    mounted: 0,
    indexedTags: 0,
    stampedTags: 0,
  );

  /// How many tracked records are in each status, how many of those are
  /// fetching, and how many remembered queries are stale or unmounted. Counters
  /// only: nothing is copied out of the cache.
  Map<String, int> statusCounts() => _ask((c) {
    final counts = <String, int>{
      'idle': 0,
      'pending': 0,
      'success': 0,
      'error': 0,
      'fetching': 0,
      'stale': 0,
      'unmounted': 0,
    };

    for (final record in c.trackedRecords()) {
      counts[record.status] = (counts[record.status] ?? 0) + 1;
      if (record.fetching) counts['fetching'] = counts['fetching']! + 1;
    }

    for (final query in c.queries()) {
      if (query.stale) counts['stale'] = counts['stale']! + 1;
      if (query.mounts == 0) counts['unmounted'] = counts['unmounted']! + 1;
    }

    return counts;
  }, const {});

  /// One light row per remembered query, in registry order: its key, operation,
  /// mount count, freshness, status and how many tags and dependencies it
  /// carries. Never the lists themselves, so a row's size does not grow with
  /// the cache.
  List<Json> querySummaries() => _ask((c) {
    final records = {
      for (final record in c.trackedRecords()) record.key: record,
    };

    return [
      for (final query in c.queries())
        {
          'key': query.key,
          'operation': query.operation,
          'mounts': query.mounts,
          'stale': query.stale,
          'settled': query.settled,
          'settledAt': query.settledAt,
          'status': records[query.key]?.status ?? 'idle',
          'fetching': records[query.key]?.fetching ?? false,
          'tagCount': query.tags.length,
          'depCount': query.deps.length,
        },
    ];
  }, const []);

  /// Counters, queries and the tag graph.
  CacheSnapshot snapshot() => _ask(
    ins.snapshot,
    const CacheSnapshot(store: _noStore, queries: [], tags: []),
  );

  /// The counters.
  StoreSnapshot store() => _ask(ins.store, _noStore);

  /// Every remembered query.
  List<QuerySnapshot> queries() => _ask(ins.queries, const []);

  /// One query.
  QuerySnapshot? query(String key) => _ask((c) => ins.query(c, key), null);

  /// One query joined to its record.
  QueryDetail? detail(String key) => _ask((c) => ins.detail(c, key), null);

  /// Every tracked record's cheap fields.
  List<RecordSnapshot> records() => _ask(ins.records, const []);

  /// The overlay stack.
  List<OverlaySnapshot> overlays() => _ask(ins.overlays, const []);

  /// One record before overlays. A bounded, read-only copy.
  Map<String, Object?>? baseRecord(String key) =>
      _ask((c) => ins.baseRecord(c, key), null);

  /// One record with overlays folded in. A bounded, read-only copy.
  Map<String, Object?>? foldedRecord(String key) =>
      _ask((c) => ins.foldedRecord(c, key), null);

  /// Pushes a hand-written field change; returns the layer id. Throws a
  /// [StateError] for a sync-owned entity.
  int patchEntity(String key, Map<String, Object?> fields) =>
      actions.patchEntity(key, fields);

  /// One entity and its dependents.
  EntitySnapshot? entity(String key) => _ask((c) => ins.entity(c, key), null);

  /// Entities matching [filter].
  List<EntitySnapshot> entities([EntityFilter filter = const EntityFilter()]) =>
      _ask((c) => ins.entities(c, filter), const []);

  /// How many entities match [filter].
  int countEntities([EntityFilter filter = const EntityFilter()]) =>
      _ask((c) => ins.countEntities(c, filter), 0);

  /// Queries that reached an entity.
  List<QuerySnapshot> dependents(String key) =>
      _ask((c) => ins.dependents(c, key), const []);

  /// The tag graph.
  List<TagSnapshot> tags() => _ask(ins.tags, const []);

  /// Whether frame capture is on.
  bool get capturing => _ring != null;

  /// Captured frames, oldest first. Empty once the inspector is disposed.
  List<FrameCapture> frames() {
    _check();
    return _ring?.entries() ?? const [];
  }

  /// Frames the ring overwrote.
  int get framesDropped => _ring?.dropped ?? 0;

  /// Frames the ring holds before overwriting, 0 when off.
  int get framesCapacity => _ring?.capacity ?? 0;

  /// What the transport did, oldest first. Empty when nothing is wired, and
  /// once the inspector is disposed.
  List<RequestSnapshot> requests() {
    _check();
    return _disposed ? const [] : requestLog?.entries() ?? const [];
  }

  /// The difference between "no requests" and "nothing is recording them".
  bool get watchingRequests => requestLog != null && !_disposed;

  /// Requests the ring overwrote.
  int get requestsDropped => _disposed ? 0 : requestLog?.dropped ?? 0;

  static const _noInspector =
      '[forge] no OutboxInspector is wired; pass one to '
      'registerForgeServiceExtensions to replay or discard';

  // A result built across an await belongs to the identity session that asked.
  // It is dropped when the cache began changing principal, or the inspector was
  // detached, in the meantime: the principal that is in charge now must never
  // be handed what was read for the previous one. This is the recorder's own
  // signal, the identity session counter it bumps in the changing listener.
  bool _moved(int session) => !_answerable() || _session != session;

  Map<String, Object?> _staleOutbox() => {
    'wired': outboxInspector != null,
    'source': 'events',
    'entries': const <Object?>[],
    'stale': true,
  };

  /// The Outbox panel's rows. With a storage session, its queued writes are
  /// the truth and the event mirror adds recent replays; without one, the
  /// mirror alone. `wired` says whether replay and discard are available.
  ///
  /// Only ids, operation names, states and a reduced failure leave: never a
  /// write's arguments or idempotency key. While the cache is changing
  /// principal, once the inspector is disposed, and when either happens while
  /// the session is being read, the answer is empty and `stale` is true.
  Future<Map<String, Object?>> outbox() async {
    if (!_answerable()) return _staleOutbox();

    final epoch = _session;
    final wired = outboxInspector != null;
    final session = _dev.session;

    if (session == null) {
      return {
        'wired': wired,
        'source': 'events',
        'entries': [for (final row in _outbox.entries()) row.toJson()],
      };
    }

    final List<PendingMutationRecord> records;

    try {
      records = await session.readOutbox();
    } on Object {
      // A session closing under the read is the principal changing.
      if (_moved(epoch)) return _staleOutbox();
      rethrow;
    }

    if (_moved(epoch)) return _staleOutbox();

    final ids = {for (final record in records) record.id};

    // Writes the event mirror knows and the session no longer holds: a replay
    // that finished, or a failure whose record is gone. They carry the same
    // body-free failure text as the stored ones.
    final remembered = [
      for (final row in _outbox.entries())
        if (!ids.contains(row.id) &&
            (row.state == 'replayed' || row.state == 'failed'))
          row,
    ];
    final total = records.length + remembered.length;

    final rows = <Map<String, Object?>>[
      for (final record in records.take(_outboxLimit))
        if (outboxStateOf(record.stateJson ?? _queuedState) case (
          :final state,
          :final failure,
          :final since,
        ))
          {
            'id': shortMessage(record.id),
            'operation': shortMessage(record.operationId),
            'createdAt': record.createdAt.millisecondsSinceEpoch,
            'state': state,
            'failure': failure,
            'since': since,
            'at': null,
          },
      for (final row in remembered.take(
        _outboxLimit - records.length.clamp(0, _outboxLimit),
      ))
        row.toJson(),
    ];

    return {
      'wired': wired,
      'source': 'session',
      // A backlog is capped like every other list the devtools send, and each
      // row is bounded, so the response cannot grow with the queue.
      'entries': [for (final row in rows) bounded(row, _outboxWidth)],
      'total': total,
      'truncated': total > rows.length,
    };
  }

  /// Rows the Outbox panel is sent from the session, as many as the event
  /// mirror holds.
  static const _outboxLimit = 200;

  /// How many fields `bounded()` keeps of a row; an id or operation is cut to
  /// 200 characters before that.
  static const _outboxWidth = 20;

  static const _queuedState = '{"kind":"queued"}';

  /// Replays one queued write through the inspector. The attempt is logged
  /// before it is made, and whatever the inspector throws (an offline
  /// failure, or a refusal because the write belongs to another principal)
  /// reaches the caller unchanged.
  Future<void> replayOutbox(String id) => _outboxAction(ActionKind.replay, id);

  /// Discards one queued write through the inspector. See [replayOutbox].
  Future<void> discardOutbox(String id) =>
      _outboxAction(ActionKind.discard, id);

  Future<void> _outboxAction(ActionKind action, String id) async {
    if (!_answerable()) {
      throw StateError(
        '[forge] the outbox is unavailable right now: the cache is changing '
        'principal, or the inspector was detached. Nothing was changed.',
      );
    }

    final inspector = outboxInspector;

    if (inspector == null) throw StateError(_noInspector);

    _log.push(
      (seq, at) => ActionLog(
        seq: seq,
        at: at,
        session: _session,
        action: action,
        target: id,
      ),
    );

    return switch (action) {
      ActionKind.replay => inspector.replay(id),
      _ => inspector.discard(id),
    };
  }

  Map<String, Object?> _staleSync() => {
    'sources': const <Object?>[],
    'entities': const <Object?>[],
    'stale': true,
  };

  /// The Sync panel: each source's entities and self-description, and the
  /// latest status each entity reported.
  ///
  /// A source describes itself only if it implements `DevtoolsInspectable`.
  /// While the cache is changing principal, once the inspector is disposed,
  /// and when either happens while a source is being asked, the answer is
  /// empty and `stale` is true: nothing a source said for the previous
  /// principal is returned.
  Future<Map<String, Object?>> sync() async {
    if (!_answerable()) return _staleSync();

    final epoch = _session;
    final sources = <Map<String, Object?>>[];

    for (final source in _dev.syncSources) {
      Object? detail;

      if (source case final DevtoolsInspectable inspectable) {
        try {
          final described = await inspectable.describeForDevtools();

          if (_moved(epoch)) return _staleSync();

          detail = bounded(described, 50);
        } on Object catch (error) {
          if (_moved(epoch)) return _staleSync();

          detail = {'error': shortMessage('$error')};
        }
      }

      sources.add({
        'type': source.runtimeType.toString(),
        'entities': [...source.entities]..sort(),
        'detail': detail,
      });
    }

    return {
      'sources': sources,
      'entities': [for (final entry in _sync.entries()) entry.toJson()],
    };
  }

  /// Turns capture on with [options], or off with null. Replacing the ring
  /// discards what the old one held.
  void setCapture(FrameOptions? options) => _ring = _ringFor(options);

  /// Stops observing, stops listening for identity changes, and restores the
  /// previous observer if the slot is still ours.
  ///
  /// Everything recorded, requests included, is dropped: a disposed inspector
  /// is not told when the identity changes, so anything it kept would outlive
  /// the principal it belongs to. Every read after this returns empty. The
  /// controls are released, not aborted: the principal is unchanged, so a
  /// request waiting out simulated latency is sent and the transport passes
  /// through from then on.
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _stopWatching();
    _stopSettled();
    if (identical(cache.observer, _observer)) cache.observer = _previous;

    _log.clear();
    _ring?.clear();
    requestLog?.clear();
    _outbox.clear();
    _sync.clear();
    controls?.release();
    _fetching.clear();
    _seen.clear();
    _pending.clear();
    _cause = null;
    // Inside a change window the queue is already empty, and nothing should
    // reach a subscriber from here.
    _log.release(deliver: false);
    _changing = false;
  }

  MissReport _unanswerable(String key) => MissReport(
    query: key,
    outcome: MissOutcome.notTracked,
    reason:
        'the inspector has nothing to say about `$key` right now: the cache is '
        'changing principal, or the inspector was detached.',
    cause: const CauseSummary(
      label: 'nothing (no mutation or frame batch is in the log)',
      seq: null,
      tags: [],
      unresolved: [],
    ),
    mounts: 0,
    settled: false,
    invalidated: const [],
    carried: const [],
    matched: const [],
    nearest: const [],
    suggestions: const [],
  );

  MissCause _missCause(MissCause? given) {
    if (given != null) return given;

    final entry = lastCause();
    final summary = entry == null ? null : causeOf(entry);

    if (summary == null) {
      return const TagsCause(
        [],
        label: 'nothing (no mutation or frame batch is in the log)',
      );
    }

    return TagsCause(
      summary.tags,
      unresolved: summary.unresolved,
      label: summary.label,
      seq: summary.seq,
    );
  }

  /// Makes sure the recorder's state belongs to the cache's current principal
  /// before a read answers. The changing listener normally got there first;
  /// this is the backstop for a notification that did not reach it.
  void _check() {
    if (_disposed) return;

    final principal = cache.principal;

    if (principal != _owner) {
      // The changing listener runs before the clear and nothing else changes
      // the principal, so a recorder that has not heard of this change is
      // being asked inside its window: the cache has not emptied yet. The
      // settle listener ends the window. The cache's generation is no guide
      // here: a plain `clear()` moves it without a principal change.
      _adopt(principal, cleared: false);
    }
  }

  /// Whether the cache can be asked a question now: not disposed, and not
  /// between the previous principal being let go of and the cache emptying.
  bool _answerable() {
    _check();
    return !_disposed && !_changing;
  }

  /// The cache's principal-changing listener. It runs synchronously, before
  /// the cache drops the previous principal's records and before any watcher
  /// hears of it, so it never reads the store or calls back into the cache.
  void _onChanging(String? next) => _adopt(next, cleared: false);

  /// The cache's after-the-clear listener: the previous principal's data is
  /// gone, so the window closes and subscribers are told.
  void _onSettled(String? principal) {
    if (principal != _owner) {
      // The changing notification never reached the recorder.
      _adopt(principal, cleared: true);
      return;
    }

    if (_changing) _settle();
  }

  void _settle() {
    _changing = false;
    _log.release();
  }

  /// Starts a new identity session under [principal]. [cleared] says whether
  /// the cache has already emptied itself.
  ///
  /// Everything recorded for the previous principal goes: the log keeps one
  /// [PrincipalLog] marker, the frame ring (when capture is on) keeps one marker
  /// capture, the request log (when wired) keeps one marker request, an armed
  /// failure on the controls is disarmed (and its delayed requests aborted), and
  /// the per-query bookkeeping, which
  /// is keyed by the previous principal's queries, is dropped. No marker carries
  /// an id, a payload or the principal's value. Until the cache has emptied itself, nothing is
  /// delivered to subscribers and nothing is answered from the cache.
  void _adopt(String? principal, {required bool cleared}) {
    if (principal == _owner) return;

    _owner = principal;
    _session++;
    _fetching.clear();
    _seen.clear();
    _pending.clear();
    _cause = null;

    _log.hold();

    final marker = _log.purge(session: _session);

    _ring?.purge(seq: marker.seq, at: marker.at);
    requestLog?.purge();
    _outbox.clear();
    _sync.clear();
    controls?.principalChanged();

    if (cleared) {
      _settle();
    } else {
      _changing = true;
      _fence = cache.generation;
    }
  }

  void _prune() {
    if (_fetching.length <= _trackLimit) return;

    for (final key in [..._fetching.keys]) {
      if (_dev.query(key) == null) {
        _fetching.remove(key);
        _seen.remove(key);
        _pending.remove(key);
      }
    }

    // Still over: start again rather than grow.
    if (_fetching.length > _trackLimit) {
      _fetching.clear();
      _seen.clear();
      _pending.clear();
    }
  }

  void _observe(CacheEvent raw) {
    if (_disposed) {
      _previous?.call(raw);
      return;
    }

    _check();

    // Between the changing notification and the clear, the cache is still
    // the previous principal's. What it reports in that window is not
    // recorded; what it reports once the clear has begun (the new principal's
    // queries mounting) is.
    if (_changing && cache.generation == _fence) {
      _previous?.call(raw);
      return;
    }

    final event = devEvent(raw);

    switch (event) {
      case DevMutationEvent(:final meta, :final args, :final response):
        final resolved = resolveTags(meta.invalidates, args, response);
        _cause = _log
            .push(
              (seq, at) => MutationLog(
                seq: seq,
                at: at,
                session: _session,
                operation: operationName(meta),
                args: argsKey(args, limit: _argsLimit),
                tags: [...resolved.tags],
                unresolved: [...resolved.unresolved],
              ),
            )
            .seq;
      case DevFramesEvent(:final count, :final tags, :final frames):
        final ring = _ring;
        if (ring != null) {
          for (final frame in frames) {
            ring.push(
              FrameCapture(
                seq: _log.sequence,
                at: _clock.now(),
                channel: frame.channel,
                message: frame.message,
                intent: frame.intent,
                entity: frame.entity,
                payload: capture(frame.payload),
              ),
            );
          }
        }
        _cause = _log
            .push(
              (seq, at) => FramesLog(
                seq: seq,
                at: at,
                session: _session,
                frames: count,
                tags: tags,
              ),
            )
            .seq;
      case DevInvalidatedEvent(:final key, :final matched):
        final cause = _cause;
        _log.push(
          (seq, at) => InvalidatedLog(
            seq: seq,
            at: at,
            session: _session,
            query: key,
            matched: matched,
            cause: cause,
          ),
        );
        if (cause != null) _pending[key] = cause;
      case DevPlacedEvent(:final key):
        final cause = _cause;
        _log.push(
          (seq, at) => PlacedLog(
            seq: seq,
            at: at,
            session: _session,
            query: key,
            cause: cause,
          ),
        );
        // Placement answers instead of a refetch, so the attribution is spent.
        _pending.remove(key);
      case DevQueryEvent(:final key, :final status, :final fetching):
        _onQuery(key, status, fetching);
      case DevOutboxEnqueued(:final mutationId, :final operation):
        final entry = _log.push(
          (seq, at) => OutboxLog(
            seq: seq,
            at: at,
            session: _session,
            phase: OutboxPhase.enqueued,
            mutationId: mutationId,
            operation: operation,
            failure: null,
          ),
        );
        _outbox.enqueued(mutationId, operation, entry.at);
      case DevOutboxReplayed(:final mutationId, :final operation):
        final entry = _log.push(
          (seq, at) => OutboxLog(
            seq: seq,
            at: at,
            session: _session,
            phase: OutboxPhase.replayed,
            mutationId: mutationId,
            operation: operation,
            failure: null,
          ),
        );
        _outbox.replayed(mutationId, operation, entry.at);
      case DevOutboxFailed(:final mutationId, :final operation, :final failure):
        final entry = _log.push(
          (seq, at) => OutboxLog(
            seq: seq,
            at: at,
            session: _session,
            phase: OutboxPhase.failed,
            mutationId: mutationId,
            operation: operation,
            failure: failure,
          ),
        );
        _outbox.failed(mutationId, operation, failure, entry.at);
      case final DevSyncStatus status:
        final entry = _log.push(
          (seq, at) => SyncLog(
            seq: seq,
            at: at,
            session: _session,
            entity: status.entity,
            status: status.status,
            detail: status.status == 'pending'
                ? '${status.pending}'
                : status.error,
          ),
        );
        _sync.apply(status, entry.at);
    }

    _previous?.call(raw);
  }

  void _onQuery(String key, String status, bool fetching) {
    final before = _fetching[key] ?? false;
    _fetching[key] = fetching;

    // A request going out or coming back closes the cause. A notification
    // with `fetching` unchanged does not: frame application notifies every
    // moved query before a single tag is applied.
    if (before != fetching) _cause = null;

    if (!before && fetching) {
      final attributed = _pending.remove(key);
      final reason = attributed != null
          ? FetchReason.invalidation
          : _seen.contains(key)
          ? FetchReason.manual
          : FetchReason.mount;

      _log.push(
        (seq, at) => FetchLog(
          seq: seq,
          at: at,
          session: _session,
          query: key,
          reason: reason,
          cause: attributed,
        ),
      );
      _seen.add(key);
      _prune();
      return;
    }

    if (before && !fetching) {
      if (status == 'error') {
        _log.push(
          (seq, at) => ErrorLog(
            seq: seq,
            at: at,
            session: _session,
            query: key,
            message: 'request failed',
          ),
        );
      } else {
        _log.push(
          (seq, at) => SettleLog(
            seq: seq,
            at: at,
            session: _session,
            query: key,
            version: _dev.version,
          ),
        );
      }
    }
  }
}
