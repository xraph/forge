import 'dart:collection';
import 'dart:math' as math;

import 'normalize.dart';
import 'ref.dart';
import 'types.dart';

/// One rehydrated subtree, held so the next read can return the same object.
final class _Memo {
  _Memo(this.value, this.key);

  bool valid = true;
  Object? value;
  List<EntityKey> deps = const [];
  final EntityKey? key;

  /// True while this memo's value is still being filled in.
  bool building = true;

  /// True when this memo sits on a cycle that closes through a plain object.
  bool cyclic = false;
}

/// A normalized value that has not been committed yet. The same shape
/// `normalize` returns: the caller inspects [NormalizeResult.records] before
/// deciding what to commit. See [EntityStore.racedSince].
typedef StagedWrite = NormalizeResult;

/// How many frame-evicted keys keep a stamp. A backstop: see
/// [EntityStore.expireTombstones] for the mechanism.
const int tombstoneLimit = 256;

final Expando<bool> _optimistic = Expando<bool>('forge.optimistic');

/// Whether a materialized record carries an optimistic value.
///
/// The TypeScript runtime stamps a symbol on the record; Dart maps cannot
/// carry hidden properties, so the stamp lives in an [Expando] keyed on the
/// record object. Like the symbol, it is invisible to `keys`, to `==` and to
/// `jsonEncode`.
bool isOptimistic(Object? record) =>
    record is Map && (_optimistic[record] ?? false);

/// The overlay runtime, as the store sees it. `OverlayStack` implements it.
abstract interface class OverlayLayer {
  /// The record with every live overlay folded in, or null when the fold
  /// deletes it.
  EntityRecord? effective(EntityKey key);

  /// Whether any live overlay touches [key].
  bool holds(EntityKey key);

  /// The base for [key] moved and the fold must be recomputed.
  void rebase(EntityKey key);
}

/// What the overlay stack needs of the store. [EntityStore] implements it;
/// tests can drive the stack through a narrower host.
abstract interface class OverlayHost {
  /// The base record for [key], or null.
  EntityRecord? getRecord(EntityKey key);

  /// Drops the memos for [keys] without writing anything.
  void touch(Iterable<EntityKey> keys);

  /// Rebuilds a value from its skeleton.
  Object? read(Object? skeleton);

  /// Merges fields into the base record.
  bool put(EntityKey key, Json data, [int frameAt = 0]);

  /// Drops a base record, leaving a tombstone when [frameAt] is non-zero.
  bool evict(EntityKey key, [int frameAt = 0]);

  /// A fresh frame stamp, for a confirmed delete to be evicted under.
  int nextFrame();
}

/// How a staged write is committed.
final class CommitOptions {
  /// Creates commit options.
  const CommitOptions({this.frameAt = 0, this.skip});

  /// Stamp every record written with this frame-clock reading. Non-zero only
  /// on the stream-frame path.
  final int frameAt;

  /// Entity keys to leave alone, because what the store holds is newer.
  final Set<EntityKey>? skip;
}

/// The normalized entity store: entity key to record, plus the machinery that
/// rebuilds a response out of it without changing the identity of anything
/// that did not change.
///
/// Referential stability is a correctness requirement: every adapter compares
/// with `identical`, so [read] is memoized per subtree and the memos are
/// invalidated by walking the reverse-dependency graph from the key that was
/// written.
final class EntityStore implements OverlayHost {
  final Map<EntityKey, EntityRecord> _records = <EntityKey, EntityRecord>{};
  final Map<EntityKey, _Memo> _memoByKey = <EntityKey, _Memo>{};
  Expando<_Memo> _memoByNode = Expando<_Memo>('forge.memo');
  final Map<EntityKey, Set<_Memo>> _dependents = <EntityKey, Set<_Memo>>{};
  int _writes = 0;

  /// The frame clock. Monotonic for the life of the store and deliberately not
  /// reset by [clear].
  int _frames = 0;

  /// The overlay stack, when one is attached. Null in a store used without a
  /// `QueryCache`.
  OverlayLayer? overlays;

  /// Frame stamps for keys the store no longer holds a record for, in
  /// insertion order so the oldest can be evicted at the cap.
  final LinkedHashMap<EntityKey, int> _graves = LinkedHashMap<EntityKey, int>();

  final List<_Memo> _buildStack = <_Memo>[];
  final List<_Memo> _deferred = <_Memo>[];
  int? _cycleFloor;

  /// Total number of record writes committed. Bumps only on real change.
  int get version => _writes;

  /// The current frame-clock reading. A request records it at dispatch.
  int get frameVersion => _frames;

  /// Opens a new frame: one reading per batch of frames.
  @override
  int nextFrame() => ++_frames;

  /// How many frame-evicted keys currently hold a stamp.
  int get tombstones => _graves.length;

  /// Drops every tombstone no outstanding request could still read.
  ///
  /// [oldestLiveDispatch] is the frame-clock reading of the earliest request
  /// still in flight; pass null when nothing is in flight, which drops all of
  /// them. Returns how many were dropped.
  int expireTombstones(int? oldestLiveDispatch) {
    if (_graves.isEmpty) return 0;

    final drop = [
      for (final MapEntry(:key, value: frameAt) in _graves.entries)
        if (oldestLiveDispatch == null || frameAt <= oldestLiveDispatch) key,
    ];

    drop.forEach(_graves.remove);

    return drop.length;
  }

  /// When a stream frame last wrote [key]. 0 if none ever did.
  int frameStamp(EntityKey key) {
    final record = _records[key];

    if (record != null) return record.frameAt ?? 0;

    return _graves[key] ?? 0;
  }

  /// Which of [keys] a stream frame wrote after [since].
  List<EntityKey> racedSince(Iterable<EntityKey> keys, int since) => [
    for (final key in keys)
      if (frameStamp(key) > since) key,
  ];

  /// How many records the store holds.
  int get size => _records.length;

  /// Whether the store holds a record for [key].
  bool has(EntityKey key) => _records.containsKey(key);

  /// The stored record, or null. Never a copy: treat it as frozen.
  @override
  EntityRecord? getRecord(EntityKey key) => _records[key];

  /// Every key the store holds.
  Iterable<EntityKey> get keys => _records.keys;

  /// Normalizes a response and commits every entity it contained.
  StagedWrite write(
    Object? value,
    EntitySchema schema, [
    String? rootType,
    CommitOptions options = const CommitOptions(),
  ]) {
    final staged = stage(value, schema, rootType);

    commit(staged, options);

    return staged;
  }

  /// Normalizes a value without writing anything.
  StagedWrite stage(Object? value, EntitySchema schema, [String? rootType]) =>
      normalize(value, rootType, schema);

  /// Writes a staged normalization into the store.
  void commit(
    StagedWrite staged, [
    CommitOptions options = const CommitOptions(),
  ]) {
    for (final MapEntry(:key, value: data) in staged.records.entries) {
      if (options.skip?.contains(key) ?? false) continue;

      put(key, data, options.frameAt);
    }
  }

  /// Merges one entity's fields into the store.
  ///
  /// Merge rather than replace, so a narrower projection never drops fields
  /// another view reads. Returns whether anything actually changed: a write of
  /// identical data keeps the previous data object and version, and therefore
  /// every object identity downstream of it.
  ///
  /// [frameAt] is carried forward by `max`, so a later response merging extra
  /// fields into a frame-written record does not erase its stamp.
  @override
  bool put(EntityKey key, Json data, [int frameAt = 0]) {
    final prev = _records[key];
    final carried = math.max(prev?.frameAt ?? _graves[key] ?? 0, frameAt);

    _graves.remove(key);

    if (prev == null) {
      _records[key] = EntityRecord(data: data, version: 1, frameAt: carried);
      _writes++;
      _invalidate(key);
      overlays?.rebase(key);

      return true;
    }

    final merged = <String, Object?>{...prev.data, ...data};

    if (_equal(prev.data, merged, null)) {
      // No data moved, but a frame touched the record, and the stamp is what
      // stops an older response from moving it.
      if (carried != (prev.frameAt ?? 0)) {
        _records[key] = EntityRecord(
          data: prev.data,
          version: prev.version,
          frameAt: carried,
        );
      }

      return false;
    }

    _records[key] = EntityRecord(
      data: merged,
      version: prev.version + 1,
      frameAt: carried,
    );
    _writes++;
    _invalidate(key);
    overlays?.rebase(key);

    return true;
  }

  /// Drops one record. A reference to it rehydrates to nothing: a list element
  /// is dropped, a map field becomes null.
  ///
  /// A non-zero [frameAt] leaves a tombstone so a response already in flight
  /// cannot put the row back.
  @override
  bool evict(EntityKey key, [int frameAt = 0]) {
    final held = _records.remove(key) != null;

    if (frameAt > 0 && held) _remember(key, frameAt);

    if (!held) return false;

    _writes++;
    _invalidate(key);
    overlays?.rebase(key);

    return true;
  }

  /// Drops the memos for [keys] without writing anything: the overlay stack's
  /// seam back into the store. One version bump for the set, and only when a
  /// memo was actually dropped.
  @override
  void touch(Iterable<EntityKey> keys) {
    var moved = false;

    for (final key in keys) {
      if (_invalidate(key)) moved = true;
    }

    if (moved) _writes++;
  }

  /// Drops everything: the identity-change path.
  void clear() {
    _records.clear();
    _memoByKey.clear();
    _dependents.clear();
    _graves.clear();
    // Replaced rather than cleared: an Expando has no clear, and a surviving
    // node memo would keep serving the previous principal's data.
    _memoByNode = Expando<_Memo>('forge.memo');
    _writes++;
  }

  /// Rebuilds a value from its skeleton.
  ///
  /// [previous] is the value the last read of this query returned; passing it
  /// keeps a container's identity across a refetch that changed nothing.
  /// [collect] receives every entity key the read reached, from the same walk.
  @override
  Object? read(Object? skeleton, [Object? previous, Set<EntityKey>? collect]) =>
      _materialize(skeleton, collect ?? <EntityKey>{}, previous);

  /// Which entity keys [skeleton] currently reaches, transitively.
  Set<EntityKey> dependencies(Object? skeleton) {
    final deps = <EntityKey>{};
    _materialize(skeleton, deps, null);

    return deps;
  }

  Object? _materialize(
    Object? skeleton,
    Set<EntityKey> collect,
    Object? previous,
  ) {
    _buildStack.clear();
    _deferred.clear();
    _cycleFloor = null;

    return _materializeNode(skeleton, collect, previous);
  }

  Object? _materializeNode(
    Object? node,
    Set<EntityKey> collect,
    Object? previous,
  ) {
    if (node is EntityRef) return _materializeKey(node.key, collect);
    if (node is! List<Object?> && node is! Map<String, Object?>) return node;

    // Not rewritten means no reference beneath it, so the node already is the
    // answer. The only deep comparison in a read.
    if (!isRewritten(node!)) {
      return previous != null && _equal(previous, node, null) ? previous : node;
    }

    final cached = _memoByNode[node];

    if (cached != null && cached.valid) {
      // Re-entering a memo that is still building closes a cycle through a
      // plain object.
      if (cached.building) _markCycle(cached);
      collect.addAll(cached.deps);

      return cached.value;
    }

    final Object out = node is List ? <Object?>[] : <String, Object?>{};
    final memo = _Memo(out, null);

    _memoByNode[node] = memo;
    _buildStack.add(memo);

    final deps = <EntityKey>{};

    if (node is List<Object?>) {
      final target = out as List<Object?>;
      final before = previous is List<Object?> ? previous : null;

      for (var i = 0; i < node.length; i++) {
        final element = node[i];
        final value = _materializeNode(
          element,
          deps,
          before != null && i < before.length ? before[i] : null,
        );

        // A reference whose record is gone is a hole and is dropped. A literal
        // null the server sent is data and is kept.
        if (value == null && element is EntityRef) continue;

        target.add(value);
      }
    } else {
      final source = node as Map<String, Object?>;
      final target = out as Map<String, Object?>;
      final before = previous is Map<String, Object?> ? previous : null;

      for (final MapEntry(key: field, value: child) in source.entries) {
        target[field] = _materializeNode(child, deps, before?[field]);
      }
    }

    _commitMemo(memo, deps, collect);

    // Bottom-up and shallow: every child has already settled its own
    // identity. Not for a cyclic memo, whose interior points at `out`.
    if (previous != null && !memo.cyclic && _sameChildren(out, previous)) {
      memo.value = previous;

      return previous;
    }

    return out;
  }

  Object? _materializeKey(EntityKey key, Set<EntityKey> collect) {
    // Recorded before the lookup, so a skeleton pointing at an entity that has
    // not arrived yet still recomputes when it does.
    collect.add(key);

    final cached = _memoByKey[key];

    if (cached != null && cached.valid) {
      collect.addAll(cached.deps);

      return cached.value;
    }

    final layer = overlays;
    final overlaid = layer?.holds(key) ?? false;
    final record = overlaid ? layer!.effective(key) : _records[key];

    if (record == null) return null;

    final out = <String, Object?>{};
    if (overlaid) _optimistic[out] = true;

    final memo = _Memo(out, key);

    _memoByKey[key] = memo;
    _buildStack.add(memo);

    final deps = <EntityKey>{};

    for (final MapEntry(key: field, value: child) in record.data.entries) {
      out[field] = _materializeNode(child, deps, null);
    }

    _commitMemo(memo, deps, collect);

    return out;
  }

  void _markCycle(_Memo target) {
    for (var i = _buildStack.length - 1; i >= 0; i--) {
      _buildStack[i].cyclic = true;

      if (identical(_buildStack[i], target)) {
        final floor = _cycleFloor;
        if (floor == null || i < floor) _cycleFloor = i;

        return;
      }
    }
  }

  void _commitMemo(_Memo memo, Set<EntityKey> deps, Set<EntityKey> collect) {
    memo.deps = deps.toList(growable: false);
    memo.building = false;

    collect.addAll(memo.deps);

    final depth = _buildStack.length - 1;
    _buildStack.removeLast();

    if (memo.cyclic) {
      _deferred.add(memo);
    } else {
      _link(memo);
    }

    // The outermost frame on the cycle: its deps cover the whole cycle.
    if (depth == _cycleFloor) {
      for (final pending in _deferred) {
        pending.deps = memo.deps;
        _link(pending);
      }

      _deferred.clear();
      _cycleFloor = null;
    }
  }

  void _remember(EntityKey key, int frameAt) {
    // Removed before it is set, so re-tombstoning moves the key to the back.
    _graves.remove(key);
    _graves[key] = frameAt;

    if (_graves.length <= tombstoneLimit) return;

    _graves.remove(_graves.keys.first);
  }

  void _link(_Memo memo) {
    for (final dep in memo.deps) {
      (_dependents[dep] ??= Set<_Memo>.identity()).add(memo);
    }
  }

  /// Invalidates every memo that reaches [root], transitively. Returns whether
  /// any memo was actually dropped.
  bool _invalidate(EntityKey root) {
    final queue = <EntityKey>[root];
    final done = <EntityKey>{};
    var dropped = false;

    while (queue.isNotEmpty) {
      final key = queue.removeLast();

      if (!done.add(key)) continue;

      final own = _memoByKey.remove(key);

      if (own != null) {
        own.valid = false;
        _dropMemo(own);
        dropped = true;
      }

      final set = _dependents.remove(key);

      if (set == null) continue;

      for (final memo in set) {
        if (!memo.valid) continue;

        memo.valid = false;
        _dropMemo(memo);
        dropped = true;

        final memoKey = memo.key;
        if (memoKey != null) queue.add(memoKey);
      }
    }

    return dropped;
  }

  void _dropMemo(_Memo memo) {
    for (final dep in memo.deps) {
      _dependents[dep]?.remove(memo);
    }
  }
}

/// Rebuilds the original value from a skeleton. See [EntityStore.read].
///
/// Caller contract: treat the result as immutable. A subtree containing no
/// entity is never copied, so the result, the skeleton and the original
/// response can be the same object.
Object? denormalize(Object? skeleton, EntityStore store) =>
    store.read(skeleton);

/// Compares two scalar values for sameness, matching JS `!==` semantics.
///
/// JavaScript compares strings and numbers by value in `!==`, but Dart's
/// `identical` does not guarantee identity for strings from separate
/// `jsonDecode` calls or for doubles. This function compares strings and
/// doubles by value while still using identity for containers and other
/// objects, so a refetch returning the same bytes yields the same child
/// identity for scalar fields.
bool _sameScalar(Object? a, Object? b) {
  if (identical(a, b)) return true;
  if (a is String && b is String) return a == b;
  if (a is double && b is double) return a == b;
  return false;
}

/// Whether a freshly built container has the same children, by identity, as
/// the one the previous read returned. Key order is not compared.
bool _sameChildren(Object built, Object? previous) {
  if (built is List<Object?>) {
    if (previous is! List<Object?> || previous.length != built.length) {
      return false;
    }

    for (var i = 0; i < built.length; i++) {
      if (!_sameScalar(built[i], previous[i])) return false;
    }

    return true;
  }

  final left = built as Map<String, Object?>;

  if (previous is! Map<String, Object?> || previous.length != left.length) {
    return false;
  }

  for (final MapEntry(:key, :value) in left.entries) {
    if (!previous.containsKey(key)) return false;
    if (!_sameScalar(value, previous[key])) return false;
  }

  return true;
}

/// Deep equality that sees through references and terminates on cycles.
///
/// [route] holds the objects on the path from the root of this comparison,
/// not every object seen: an object reachable twice through different
/// branches is a DAG, and treating the second encounter as already equal
/// would miss a real change.
bool _equal(Object? a, Object? b, Set<Object>? route) {
  if (sameValue(a, b)) return true;

  if (a == null || b == null) return false;
  if (a is EntityRef || b is EntityRef) return false;

  final isContainer =
      (a is List<Object?> || a is Map<String, Object?>) &&
      (b is List<Object?> || b is Map<String, Object?>);
  if (!isContainer) return a == b;

  final path = route ?? Set<Object>.identity();

  // Already on this route: a genuine cycle the enclosing comparison is in the
  // middle of deciding.
  if (path.contains(a)) return true;

  path.add(a);
  final same = _equalChildren(a, b, path);
  path.remove(a);

  return same;
}

bool _equalChildren(Object a, Object b, Set<Object> route) {
  if (a is List<Object?>) {
    if (b is! List<Object?> || a.length != b.length) return false;

    for (var i = 0; i < a.length; i++) {
      if (!_equal(a[i], b[i], route)) return false;
    }

    return true;
  }

  if (b is List<Object?>) return false;

  final left = a as Map<String, Object?>;
  final right = b as Map<String, Object?>;

  if (left.length != right.length) return false;

  for (final MapEntry(:key, :value) in left.entries) {
    if (!right.containsKey(key)) return false;
    if (!_equal(value, right[key], route)) return false;
  }

  return true;
}
