/// A fixed-size ring of events. Port of `client-devtools/src/log.ts`.
///
/// The backing list is allocated once at capacity and never grows. When full
/// the oldest entry is overwritten and `dropped` counts it. Entries are
/// bounded by construction: tags are copied strings, arguments a truncated
/// key, an error a message. The ring also detaches what it is handed: every
/// list an entry carries is copied into an unmodifiable list on the way in, so
/// application code mutating a list later cannot move what was recorded.
library;

import '../transport.dart';
import 'types.dart';

/// The causal event log.
final class EventLog {
  /// Creates a ring holding [capacity] entries (at least one).
  EventLog({int capacity = 500, this._clock = realClock})
    : capacity = capacity < 1 ? 1 : capacity,
      _ring = List<LogEntry?>.filled(capacity < 1 ? 1 : capacity, null);

  /// How many entries the ring holds before it overwrites.
  final int capacity;

  final Clock _clock;
  final List<LogEntry?> _ring;
  final Set<void Function(LogEntry entry)> _listeners = {};

  // Entries recorded while delivery is held, in order. See [hold].
  final List<LogEntry> _queued = [];

  bool _held = false;
  int _cursor = 0;
  int _filled = 0;
  int _overwritten = 0;
  int _next = 1;

  /// Entries overwritten and gone for good.
  int get dropped => _overwritten;

  /// Entries currently held.
  int get size => _filled;

  /// The `seq` the next entry will take.
  int get sequence => _next;

  /// Records one entry, stamping its sequence and clock reading. Listeners run
  /// inside a `try`: a panel that throws must not break the app it inspects.
  ///
  /// The entry that is stored, returned and heard is a detached copy of what
  /// [build] returned.
  T push<T extends LogEntry>(T Function(int seq, int at) build) {
    final entry = _detached(build(_next++, _clock.now())) as T;

    _store(entry);

    if (_held) {
      _queued.add(entry);
    } else {
      _notify(entry);
    }

    return entry;
  }

  /// Every entry held, oldest first. A copy.
  List<LogEntry> entries() {
    final start = _filled == capacity ? _cursor : 0;
    return [for (var i = 0; i < _filled; i++) ?_ring[(start + i) % capacity]];
  }

  /// The entry with this sequence number, if the ring still holds it.
  LogEntry? find(int seq) {
    for (var i = 0; i < _filled; i++) {
      final entry = _ring[i];
      if (entry != null && entry.seq == seq) return entry;
    }
    return null;
  }

  /// The most recent entry satisfying [match], searching backwards.
  LogEntry? last(bool Function(LogEntry entry) match) {
    final start = _filled == capacity ? _cursor : 0;

    for (var i = _filled - 1; i >= 0; i--) {
      final entry = _ring[(start + i) % capacity];
      if (entry != null && match(entry)) return entry;
    }

    return null;
  }

  /// Forgets everything, including entries whose delivery is still held. The
  /// sequence keeps counting; `dropped` resets.
  void clear() {
    _queued.clear();
    _ring.fillRange(0, capacity, null);
    _cursor = 0;
    _filled = 0;
    _overwritten = 0;
  }

  /// Drops every entry and leaves exactly one [PrincipalLog] marker, stamped
  /// with [session] (the identity session that begins now). The marker carries
  /// no id, no payload and no principal value.
  ///
  /// Called when the identity changes, so one user's recorded activity is not
  /// readable by the next. The sequence keeps counting, so a cause that
  /// pointed at a purged entry is recognisably gone rather than pointing at its
  /// replacement. Subscribers hear the marker, never the dropped entries.
  PrincipalLog purge({required int session}) {
    clear();

    return push((seq, at) => PrincipalLog(seq: seq, at: at, session: session));
  }

  /// Stops telling subscribers about new entries. They are still recorded and
  /// readable; delivery is queued, in order, until [release].
  ///
  /// The recorder holds delivery across a principal change, so a subscriber
  /// that reacts to the marker by reading the cache runs after the previous
  /// principal's data is gone, not while the cache still holds it.
  void hold() => _held = true;

  /// Ends [hold]. With [deliver] the queued entries reach the subscribers, in
  /// the order they were recorded; without it they are dropped unheard.
  ///
  /// Delivery stays held while the queue drains, so an entry a subscriber
  /// records in reaction joins the end of the queue instead of overtaking it.
  void release({bool deliver = true}) {
    if (!deliver) _queued.clear();

    while (_queued.isNotEmpty) {
      _notify(_queued.removeAt(0));
    }

    _held = false;
  }

  /// Hears each entry as it is recorded. Returns the unsubscribe.
  void Function() subscribe(void Function(LogEntry entry) listener) {
    _listeners.add(listener);
    return () => _listeners.remove(listener);
  }

  void _store(LogEntry entry) {
    if (_filled == capacity) {
      _overwritten++;
    } else {
      _filled++;
    }

    _ring[_cursor] = entry;
    _cursor = (_cursor + 1) % capacity;
  }

  void _notify(LogEntry entry) {
    for (final listener in [..._listeners]) {
      try {
        listener(entry);
      } catch (_) {
        // Swallowed on purpose: see [push].
      }
    }
  }
}

List<String> _own(List<String> values) => List<String>.unmodifiable(values);

/// A copy of [entry] whose lists belong to the ring. Strings, numbers and
/// enums are immutable, so the lists are the only thing that could alias.
LogEntry _detached(LogEntry entry) => switch (entry) {
  MutationLog() => MutationLog(
    seq: entry.seq,
    at: entry.at,
    session: entry.session,
    operation: entry.operation,
    args: entry.args,
    tags: _own(entry.tags),
    unresolved: _own(entry.unresolved),
  ),
  FramesLog() => FramesLog(
    seq: entry.seq,
    at: entry.at,
    session: entry.session,
    frames: entry.frames,
    tags: _own(entry.tags),
  ),
  InvalidatedLog() => InvalidatedLog(
    seq: entry.seq,
    at: entry.at,
    session: entry.session,
    query: entry.query,
    matched: _own(entry.matched),
    cause: entry.cause,
  ),
  PlacedLog() ||
  FetchLog() ||
  SettleLog() ||
  ErrorLog() ||
  PrincipalLog() ||
  ActionLog() ||
  OutboxLog() ||
  SyncLog() => entry,
};
