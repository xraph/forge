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
/// is readable once the next is in charge.
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
    _stopWatching = cache.watchPrincipalChanging(_adopt);
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
  late final CacheObserver _observer = _observe;

  final _fetching = <String, bool>{};
  final _seen = <String>{};
  final _pending = <String, int>{};
  int _session = 0;
  bool _disposed = false;
  int? _cause;

  static FrameRing? _ringFor(FrameOptions? options) =>
      options == null || options.limit <= 0 ? null : FrameRing(options.limit);

  DevCache get _dev => DevCache(cache);

  /// The causal log itself, for the action layer and the service host.
  EventLog get eventLog => _log;

  /// Entries the log holds before overwriting.
  int get capacity => _log.capacity;

  /// Entries overwritten.
  int get dropped => _log.dropped;

  /// Identity changes since attaching.
  int get session => _session;

  /// The log, oldest first.
  List<LogEntry> log() => _log.entries();

  /// Drops every recorded event and every captured frame. The cache is untouched.
  void clear() {
    _log.clear();
    _ring?.clear();
  }

  /// Hears each entry as it is recorded.
  void Function() subscribe(void Function(LogEntry entry) listener) =>
      _log.subscribe(listener);

  /// Why did this query refetch?
  RefetchReport? whyRefetched(String key) => ex.whyRefetched(_log, key);

  /// Why did this query not refetch? With no [cause], the most recent recorded
  /// mutation or frame batch is used.
  MissReport whyNotRefetched(String key, [MissCause? cause]) =>
      ex.whyNotRefetched(_dev, key, _missCause(cause), _log);

  /// The refetch story when it refetched, the miss story otherwise.
  Explanation explain(String key) {
    final report = whyNotRefetched(key);
    if (report.outcome == MissOutcome.refetched) {
      return whyRefetched(key) ?? report;
    }
    return report;
  }

  /// What would [meta] invalidate, and who would it reach?
  InvalidationPreview wouldInvalidate(
    OperationMeta meta, [
    TagContext args = TagContext.empty,
    Object? response,
  ]) => ex.wouldInvalidate(_dev, meta, args, response);

  /// The most recent mutation or frame batch the log still holds.
  LogEntry? lastCause() =>
      _log.last((entry) => entry is MutationLog || entry is FramesLog);

  /// Whether frame capture is on.
  bool get capturing => _ring != null;

  /// Captured frames, oldest first.
  List<FrameCapture> frames() => _ring?.entries() ?? const [];

  /// Frames the ring overwrote.
  int get framesDropped => _ring?.dropped ?? 0;

  /// Frames the ring holds before overwriting, 0 when off.
  int get framesCapacity => _ring?.capacity ?? 0;

  /// Turns capture on with [options], or off with null. Replacing the ring
  /// discards what the old one held.
  void setCapture(FrameOptions? options) => _ring = _ringFor(options);

  /// Stops observing, stops listening for identity changes, and restores the
  /// previous observer if the slot is still ours. What was recorded stays
  /// readable.
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _stopWatching();
    if (identical(cache.observer, _observer)) cache.observer = _previous;
  }

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

  /// The backstop: an event that arrives under a principal the recorder has
  /// not been told about yet.
  void _checkPrincipal() {
    if (_disposed) return;

    _adopt(cache.principal);
  }

  /// Starts a new identity session under [principal]. Registered as the
  /// cache's principal-changing listener, so it runs synchronously, before the
  /// cache drops the previous principal's records and before any watcher hears
  /// of it. It therefore never reads the store or calls back into the cache.
  ///
  /// Everything recorded for the previous principal goes: the log keeps one
  /// [PrincipalLog] marker, the frame ring (when capture is on) keeps one marker
  /// capture, and the per-query bookkeeping, which is keyed by the previous
  /// principal's queries, is dropped. Neither marker carries an id, a payload
  /// or the principal's value.
  void _adopt(String? principal) {
    if (principal == _owner) return;

    _owner = principal;
    _session++;
    _fetching.clear();
    _seen.clear();
    _pending.clear();
    _cause = null;

    final marker = _log.purge(session: _session);

    _ring?.purge(seq: marker.seq, at: marker.at);
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

    _checkPrincipal();

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
