/// Bounded copies and the frame ring. Port of `client-devtools/src/frames.ts`.
library;

import '../ref.dart';
import 'types.dart';

const _maxDepth = 6;
const _frameWidth = 50;
const _more = '[more]';

/// Most values one [bounded] call visits, leaves and containers alike. A shared
/// subtree counts every time it is visited, so a value that is a DAG on paper
/// and a tree of millions of nodes in practice is cut off, not walked.
const _nodeBudget = 5000;

/// The longest string kept, whether the value was a string or only has a string
/// form. A cut string ends in `...`.
const _maxText = 1000;

/// Containers [bounded] has built, with the width each was capped at. Output is
/// unmodifiable and bounded, so handing one back to [bounded] returns it
/// untouched instead of truncating a truncated level a second time.
final Expando<int> _built = Expando<int>('forge.devtools.bounded');

/// A copy of [value] capped in depth, in width (a list's length and a map's key
/// count alike), in total size and in string length. A truncated level carries
/// an `[N more]` marker, filed under `[more]` for a map. A container that
/// contains itself is cut at the repeat and shown as `[cycle]`. The shared
/// walker behind every bounded copy in the devtools; callers differ only in
/// [width].
///
/// The result shares nothing with [value] and cannot be written to: every list
/// and map is rebuilt and unmodifiable, and everything else is a string, a
/// number, a bool or null. A non-finite number is null, as JSON cannot carry
/// it. [budget] is the most nodes the walk may visit.
Object? bounded(
  Object? value,
  int width, [
  int depth = 0,
  int budget = _nodeBudget,
]) => _walk(value, width, depth, _Walk(budget));

final class _Walk {
  _Walk(this.budget);

  int budget;

  /// The containers on the way down to the current node, by identity. A
  /// container seen again while still on the path is a cycle; one seen again
  /// after the walk left it is just shared.
  final Set<Object> path = Set<Object>.identity();
}

Object? _walk(Object? value, int width, int depth, _Walk walk) {
  if (walk.budget <= 0) return '[truncated]';

  walk.budget--;

  if (depth > _maxDepth) return '[deeper]';

  if (value == null || value is bool) return value;
  if (value is num) return value.isFinite ? value : null;
  if (value is String) return _text(value);
  if (value is EntityRef) {
    return Map<String, Object?>.unmodifiable({'__ref': value.key});
  }

  if (value is List<Object?>) {
    if (_reusable(value, width)) return value;
    if (!walk.path.add(value)) return '[cycle]';

    final out = <Object?>[];

    for (final element in value) {
      if (out.length >= width || walk.budget <= 0) break;
      out.add(_walk(element, width, depth + 1, walk));
    }

    walk.path.remove(value);

    if (value.length > out.length) {
      out.add('[${value.length - out.length} more]');
    }

    return _remember(List<Object?>.unmodifiable(out), width);
  }

  if (value is Map<Object?, Object?>) {
    if (_reusable(value, width)) return value;
    if (!walk.path.add(value)) return '[cycle]';

    final out = <String, Object?>{};
    var seen = 0;

    for (final MapEntry(:key, value: field) in value.entries) {
      if (seen >= width || walk.budget <= 0) break;
      out[_text(key)] = _walk(field, width, depth + 1, walk);
      seen++;
    }

    walk.path.remove(value);

    if (value.length > seen) out[_more] = '[${value.length - seen} more]';

    return _remember(Map<String, Object?>.unmodifiable(out), width);
  }

  return _text(value);
}

bool _reusable(Object container, int width) {
  final built = _built[container];

  return built != null && built <= width;
}

T _remember<T extends Object>(T container, int width) {
  _built[container] = width;
  return container;
}

/// The string form of [value], cut at [_maxText] so a string or a value with an
/// enormous `toString` cannot make a capture enormous.
String _text(Object? value) {
  final text = '$value';

  return text.length <= _maxText ? text : '${text.substring(0, _maxText)}...';
}

/// A frame payload, bounded at width 50.
Object? capture(Object? value) => bounded(value, _frameWidth);

/// The `intent` of the marker [FrameRing.purge] leaves behind.
const _principalIntent = 'principal';

/// A fixed ring of captured frames, allocated once. Opt in: it is the only
/// structure in the devtools that holds payloads.
final class FrameRing {
  /// Creates a ring holding [capacity] frames (at least one).
  FrameRing(int capacity)
    : capacity = capacity < 1 ? 1 : capacity,
      _ring = List<FrameCapture?>.filled(capacity < 1 ? 1 : capacity, null);

  /// Frames held before the oldest is overwritten.
  final int capacity;

  final List<FrameCapture?> _ring;
  int _cursor = 0;
  int _filled = 0;
  int _dropped = 0;

  /// Frames overwritten since the last [clear] or [purge].
  int get dropped => _dropped;

  /// Adds one frame, overwriting the oldest when full.
  ///
  /// The frame that is kept carries a [capture] of the payload, whatever the
  /// caller passed, so a payload that still aliases application data cannot be
  /// held here. A payload that is already a capture is kept as it is.
  void push(FrameCapture entry) {
    _put(
      FrameCapture(
        seq: entry.seq,
        at: entry.at,
        channel: entry.channel,
        message: entry.message,
        intent: entry.intent,
        entity: entry.entity,
        payload: capture(entry.payload),
      ),
    );
  }

  /// Everything held, oldest first.
  List<FrameCapture> entries() {
    final start = _filled == capacity ? _cursor : 0;
    return [for (var i = 0; i < _filled; i++) ?_ring[(start + i) % capacity]];
  }

  /// Empties the ring and resets [dropped].
  void clear() {
    _ring.fillRange(0, capacity, null);
    _cursor = 0;
    _filled = 0;
    _dropped = 0;
  }

  /// Drops every frame and leaves exactly one marker: a capture whose `intent`
  /// is `principal` and whose channel, message, entity and payload are empty.
  /// It carries [seq] and [at] (the principal-change entry in the event log)
  /// and nothing of what came before.
  ///
  /// Called when the identity changes, so one user's frames are not readable by
  /// the next. Like an event log entry, the marker counts towards [dropped] when
  /// it is overwritten.
  void purge({required int seq, required int at}) {
    clear();

    _put(
      FrameCapture(
        seq: seq,
        at: at,
        channel: '',
        message: '',
        intent: _principalIntent,
        entity: '',
        payload: null,
      ),
    );
  }

  void _put(FrameCapture entry) {
    if (_filled < capacity) {
      _filled++;
    } else {
      _dropped++;
    }
    _ring[_cursor] = entry;
    _cursor = (_cursor + 1) % capacity;
  }
}
