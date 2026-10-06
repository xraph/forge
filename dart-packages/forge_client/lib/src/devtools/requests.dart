/// A bounded ring of requests, fed by the transport's observer. Port of
/// `client-devtools/src/requests.ts`. No headers and no bodies: the view is
/// about the policy a request was subject to, not its bytes.
///
/// Nothing crosses principals. [RequestLog.purge] drops every request and
/// leaves one marker with no path, query, header, body or principal, and a
/// request that was in flight when it ran is never recorded when it settles.
/// `Devtools` calls it from the same listener that purges the event log. A log
/// used without `Devtools` is not told about principal changes.
library;

import '../operation.dart';
import '../transport.dart';
import '../types.dart';
import 'explain.dart' show argsKey, tagContextJson;
import 'frames.dart' show bounded;
import 'seams.dart';

/// How wide a query value may be before it is cut. Every value goes through
/// `bounded()` before the log keeps it.
const _argWidth = 20;

/// Where a request is.
enum RequestOutcome {
  /// Still in flight.
  pending,

  /// Succeeded.
  ok,

  /// Failed.
  failed,
}

/// One retry that was taken.
final class RequestRetry {
  /// Creates the entry.
  const RequestRetry({
    required this.attempt,
    required this.delayMs,
    required this.status,
  });

  /// The zero-based attempt that failed.
  final int attempt;

  /// The backoff it waited.
  final int delayMs;

  /// The status that failed it.
  final int? status;

  /// The JSON form.
  Json toJson() => {'attempt': attempt, 'delayMs': delayMs, 'status': status};
}

/// What one request did, reduced to what is free to keep.
final class RequestSnapshot {
  /// Creates a snapshot.
  const RequestSnapshot({
    required this.id,
    required this.operation,
    required this.method,
    required this.args,
    required this.at,
    required this.duration,
    required this.attempts,
    required this.limit,
    required this.status,
    required this.outcome,
    required this.retries,
    required this.refreshes,
    required this.joined,
    required this.authMs,
    this.marker = false,
  });

  /// The transport's request id. Negative for a [marker].
  final int id;

  /// `METHOD /path`. Empty-bodied for a [marker].
  final String operation;

  /// Upper-case method.
  final String method;

  /// Path and query as a truncated key. Never headers, never the body.
  final String args;

  /// Clock reading at dispatch.
  final int at;

  /// Null while in flight.
  final int? duration;

  /// Attempts actually made.
  final int attempts;

  /// Attempts the method was allowed.
  final int limit;

  /// The final status.
  final int? status;

  /// Where it is.
  final RequestOutcome outcome;

  /// Each retry, with its delay.
  final List<RequestRetry> retries;

  /// Times a 401 sent it to the refresh.
  final int refreshes;

  /// Whether it only joined a refresh someone else started.
  final bool joined;

  /// Milliseconds waiting on the refresh.
  final int authMs;

  /// Whether this is the principal-change marker rather than a request. A
  /// marker carries no path, query, status or principal.
  final bool marker;

  /// The JSON form.
  Json toJson() => {
    'id': id,
    'operation': operation,
    'method': method,
    'args': args,
    'at': at,
    'duration': duration,
    'attempts': attempts,
    'limit': limit,
    'status': status,
    'outcome': outcome.name,
    'retries': [for (final retry in retries) retry.toJson()],
    'refreshes': refreshes,
    'joined': joined,
    'authMs': authMs,
    'marker': marker,
  };
}

final class _Live {
  _Live({
    required this.id,
    required this.operation,
    required this.method,
    required this.args,
    required this.at,
    required this.limit,
    this.marker = false,
  });

  final int id;
  final String operation;
  final String method;
  final String args;
  final int at;
  final int limit;
  final bool marker;
  int? duration;
  int attempts = 1;
  int? status;
  RequestOutcome outcome = RequestOutcome.pending;
  final List<RequestRetry> retries = [];
  int refreshes = 0;
  bool joined = false;
  int authMs = 0;
  int? authAt;

  RequestSnapshot snapshot() => RequestSnapshot(
    id: id,
    operation: operation,
    method: method,
    args: args,
    at: at,
    duration: duration,
    attempts: attempts,
    limit: limit,
    status: status,
    outcome: outcome,
    retries: [...retries],
    refreshes: refreshes,
    joined: joined,
    authMs: authMs,
    marker: marker,
  );
}

/// The request ring. Hand [observer] to `RestTransport`, or let
/// `registerForgeServiceExtensions` wire it through the debug slot.
final class RequestLog {
  /// Creates a ring of [capacity] requests (at least one).
  RequestLog({int capacity = 200, this._clock = realClock})
    : capacity = capacity < 1 ? 1 : capacity,
      _ring = List<_Live?>.filled(capacity < 1 ? 1 : capacity, null);

  /// Requests held before the oldest is overwritten.
  final int capacity;

  final Clock _clock;
  final List<_Live?> _ring;

  // Requests still waiting on their settle. Emptied by [purge] and [clear]: a
  // request that began before either one is not in this map when it settles,
  // so it is never recorded. That is the fence for a request in flight across
  // a principal change.
  final Map<int, _Live> _byId = {};
  int _cursor = 0;
  int _filled = 0;
  int _overwritten = 0;

  /// Requests overwritten by newer ones.
  int get dropped => _overwritten;

  /// Records one transport event.
  void observe(RequestEvent event) {
    final mapped = devRequestEvent(event);
    if (mapped != null) _record(mapped);
  }

  /// [observe] as a value, for `RestTransport(observer: ...)`.
  RequestObserver get observer => observe;

  /// In dispatch order, oldest first.
  List<RequestSnapshot> entries() => [
    for (var i = 0; i < _filled; i++)
      if (_ring[(_cursor + capacity - _filled + i) % capacity] case final live?)
        live.snapshot(),
  ];

  /// Forgets everything, including the request ids still waiting to settle.
  void clear() {
    _ring.fillRange(0, capacity, null);
    _byId.clear();
    _cursor = 0;
    _filled = 0;
    _overwritten = 0;
  }

  /// Drops every request and leaves one marker with no path, query, header,
  /// body or principal. A request that was in flight is not recorded when it
  /// settles. Called when the identity changes, so one user's requests are not
  /// readable by the next.
  void purge() {
    clear();
    _push(
      _Live(
        id: -1,
        operation: 'principal changed',
        method: '',
        args: '',
        at: _clock.now(),
        limit: 0,
        marker: true,
      )..outcome = RequestOutcome.ok,
    );
  }

  void _record(DevRequestEvent event) {
    if (event case DevRequestStarted(
      :final id,
      :final method,
      :final path,
      :final args,
      :final limit,
    )) {
      _push(
        _Live(
          id: id,
          operation: '$method $path',
          method: method,
          // Path and query only, every value through `bounded()`. Headers
          // carry credentials and the body may be a login: neither is read.
          args: argsKey(
            bounded(
              tagContextJson(TagContext(path: args.path, query: args.query)),
              _argWidth,
            ),
          ),
          at: _clock.now(),
          limit: limit,
        ),
      );
      return;
    }

    // Its slot was overwritten by newer traffic, or the log was purged while
    // it was in flight: nothing to update, and nothing to record.
    final live = _byId[event.id];
    if (live == null) return;

    switch (event) {
      case DevRequestStarted():
        break;
      case DevRequestRetried(:final attempt, :final delayMs, :final status):
        live.retries.add(
          RequestRetry(attempt: attempt, delayMs: delayMs, status: status),
        );
        live.attempts = attempt + 2;
      case DevRequestRefresh(:final joined):
        live.refreshes += 1;
        live.authAt = _clock.now();
        // Only ever waited: a request that started a flight is not a joiner.
        if (live.refreshes == 1) {
          live.joined = joined;
        } else if (!joined) {
          live.joined = false;
        }
      case DevRequestRefreshed():
        final started = live.authAt;
        if (started != null) {
          live.authMs += _clock.now() - started;
          live.authAt = null;
        }
      case DevRequestSettled(:final ok, :final status):
        live.outcome = ok ? RequestOutcome.ok : RequestOutcome.failed;
        live.status = status;
        live.duration = _clock.now() - live.at;
        _byId.remove(live.id);
    }
  }

  void _push(_Live live) {
    final evicted = _ring[_cursor];
    if (evicted != null) _byId.remove(evicted.id);

    if (_filled == capacity) {
      _overwritten++;
    } else {
      _filled++;
    }

    _ring[_cursor] = live;
    if (!live.marker) _byId[live.id] = live;
    _cursor = (_cursor + 1) % capacity;
  }
}
