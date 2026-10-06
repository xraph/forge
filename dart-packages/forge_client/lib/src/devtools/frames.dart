/// Bounded copies and the frame ring. Port of `client-devtools/src/frames.ts`.
library;

import '../ref.dart';
import 'types.dart';

const _maxDepth = 6;
const _frameWidth = 50;
const _more = '[more]';

/// The longest string form kept for a value that is not JSON.
const _maxFallback = 1000;

/// A copy of [value] capped in depth and in width, on a list's length and a
/// map's key count alike. A truncated level carries an `[N more]` marker,
/// filed under `[more]` for a map. The shared walker behind every bounded copy
/// in the devtools; callers differ only in [width].
///
/// The result shares nothing mutable with [value]: every list and map is
/// rebuilt, and everything else is a string, a number, a bool or null.
Object? bounded(Object? value, int width, [int depth = 0]) {
  if (depth > _maxDepth) return '[deeper]';
  if (value == null || value is num || value is String || value is bool) {
    return value;
  }
  if (value is EntityRef) return {'__ref': value.key};

  if (value is List<Object?>) {
    final out = <Object?>[
      for (final element in value.take(width))
        bounded(element, width, depth + 1),
    ];
    if (value.length > width) out.add('[${value.length - width} more]');
    return out;
  }

  if (value is Map<Object?, Object?>) {
    final out = <String, Object?>{};
    for (final MapEntry(:key, value: field) in value.entries.take(width)) {
      out[_text(key)] = bounded(field, width, depth + 1);
    }
    if (value.length > width) out[_more] = '[${value.length - width} more]';
    return out;
  }

  return _text(value);
}

/// The string form of something JSON cannot carry, capped so a value with an
/// enormous `toString` cannot make a capture enormous.
String _text(Object? value) {
  final text = '$value';

  return text.length <= _maxFallback
      ? text
      : '${text.substring(0, _maxFallback)}...';
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
  /// The payload must already be a [capture]: the ring keeps what it is given
  /// and does not copy again, because a captured value is not a fixed point of
  /// [capture] (a truncated level would be truncated twice and lose its
  /// marker). `Devtools` is the only caller and always captures first.
  void push(FrameCapture entry) => _put(entry);

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
  /// the next. The marker is not a frame, so overwriting it does not count
  /// towards [dropped].
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
    } else if (_ring[_cursor] case final old?
        when old.intent != _principalIntent) {
      _dropped++;
    }
    _ring[_cursor] = entry;
    _cursor = (_cursor + 1) % capacity;
  }
}
