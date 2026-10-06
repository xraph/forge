/// The inspector: the recorder that claims `cache.observer`, and the methods a
/// panel calls. Port of `client-devtools/src/devtools.ts`.
library;

import '../cache.dart';
import '../observe.dart';
import '../operation.dart';
import '../tags.dart';
import '../transport.dart';
import 'explain.dart' as ex;
import 'explain.dart' show MissCause, TagsCause, argsKey, causeOf;
import 'frames.dart';
import 'log.dart';
import 'seams.dart';
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
/// Nothing crosses principals: when the cache's principal changes, the log and
/// the frame ring are purged (one marker each is left) before the cache drops
/// the previous principal's records, so none of what was recorded for one user
/// is readable once the next is in charge. A query a component is still
/// watching is re-mounted by the cache for the new principal under the same
/// key, so that key (and a value in it) reappears in the new session's log
/// exactly as it does in the cache's own registry.
Devtools attach(
  QueryCache cache, {
  int limit = 500,
  Clock clock = realClock,
  int argsLimit = 200,
  FrameOptions? frames,
}) => Devtools._(
  cache,
  limit: limit,
  clock: clock,
  argsLimit: argsLimit,
  frames: frames,
);

/// The inspector over one cache.
final class Devtools {
  Devtools._(
    this.cache, {
    required int limit,
    required this._clock,
    required this._argsLimit,
    required FrameOptions? frames,
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

  final EventLog _log;
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

  /// Turns capture on with [options], or off with null. Replacing the ring
  /// discards what the old one held.
  void setCapture(FrameOptions? options) => _ring = _ringFor(options);

  /// Stops observing, stops listening for identity changes, and restores the
  /// previous observer if the slot is still ours.
  ///
  /// Everything recorded is dropped: a disposed inspector is not told when the
  /// identity changes, so anything it kept would outlive the principal it
  /// belongs to. Every read after this returns empty.
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _stopWatching();
    _stopSettled();
    if (identical(cache.observer, _observer)) cache.observer = _previous;

    _log.clear();
    _ring?.clear();
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
  /// capture, and the per-query bookkeeping, which is keyed by the previous
  /// principal's queries, is dropped. Neither marker carries an id, a payload
  /// or the principal's value. Until the cache has emptied itself, nothing is
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
        _log.push(
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
      case DevOutboxReplayed(:final mutationId):
        _log.push(
          (seq, at) => OutboxLog(
            seq: seq,
            at: at,
            session: _session,
            phase: OutboxPhase.replayed,
            mutationId: mutationId,
            operation: null,
            failure: null,
          ),
        );
      case DevOutboxFailed(:final mutationId, :final failure):
        _log.push(
          (seq, at) => OutboxLog(
            seq: seq,
            at: at,
            session: _session,
            phase: OutboxPhase.failed,
            mutationId: mutationId,
            operation: null,
            failure: failure,
          ),
        );
      case DevSyncStatus(
        :final entity,
        :final status,
        :final pending,
        :final error,
      ):
        _log.push(
          (seq, at) => SyncLog(
            seq: seq,
            at: at,
            session: _session,
            entity: entity,
            status: status,
            detail: status == 'pending' ? '$pending' : error,
          ),
        );
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
