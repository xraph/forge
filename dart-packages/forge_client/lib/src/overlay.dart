import 'invalidate.dart';
import 'normalize.dart';
import 'operation.dart';
import 'ref.dart';
import 'registry.dart';
import 'store.dart';
import 'tags.dart';
import 'types.dart';

/// Computes a merge patch's fields from the record it lands on. Re-run on
/// every refold, against the base the refold is standing on, which is why two
/// pending increments compose.
typedef MergeSource = Json Function(Json previous);

/// One entity's change, as a patch rather than as a value.
sealed class EntityPatch {
  const EntityPatch();
}

/// Merges fields into the record. Over a record that does not exist it is a
/// no-op rather than a create, which is what lets an evicting stream frame
/// beat a pending local edit.
final class MergePatch extends EntityPatch {
  /// Merges literal [fields].
  const MergePatch(this.fields) : compute = null;

  /// Merges whatever [compute] returns for the record it lands on.
  const MergePatch.computed(MergeSource this.compute) : fields = const {};

  /// The literal fields, when [compute] is null.
  final Json fields;

  /// The computed source, when set.
  final MergeSource? compute;
}

/// Creates a record base never held, under a minted key.
final class CreatePatch extends EntityPatch {
  /// Creates the patch.
  const CreatePatch(this.fields);

  /// The new record's fields, including its identity field.
  final Json fields;
}

/// Deletes the record.
final class DeletePatch extends EntityPatch {
  /// Creates the patch.
  const DeletePatch();
}

/// One pending mutation's whole contribution.
final class OverlayEntry {
  /// Creates an entry.
  const OverlayEntry({
    required this.id,
    required this.patches,
    this.place,
    this.tags = const [],
    this.created,
  });

  /// Monotonic per stack.
  final int id;

  /// The patches, keyed by entity.
  final Map<EntityKey, EntityPatch> patches;

  /// The mutation's placement callbacks, for the membership plane.
  final Map<String, Placement>? place;

  /// Its `invalidates`, already resolved against its arguments.
  final List<String> tags;

  /// The minted key, for a create.
  final EntityKey? created;
}

final class _Projection(
  final int version,
  final Object? base,
  final Object? value,
);

/// The ordered stack of pending optimistic changes.
///
/// Nothing here writes the base store outside [promote]. What a subscriber
/// sees is `fold(base, patches in push order)`, recomputed on demand, so
/// rollback is the removal of an entry and no inverse is recorded anywhere.
final class OverlayStack implements OverlayLayer {
  /// Creates a stack over [_host]. [_report] receives a throwing compute patch
  /// or placement callback, with the context `optimistic`. [_entities] is the
  /// schema a promoted change is normalized against; without it a changed
  /// field is promoted in client shape.
  OverlayStack(this._host, [this._report, this._entities = const {}]);

  final OverlayHost _host;
  final void Function(Object error, String context)? _report;
  final EntitySchema _entities;
  final List<OverlayEntry> _entries = <OverlayEntry>[];
  final Map<EntityKey, EntityRecord?> _folded = <EntityKey, EntityRecord?>{};

  /// The keys whose fold is on the stack right now. A compute's view that
  /// reaches one of them again (an order's customer lists the order) reads
  /// its base rather than folding it a second time.
  final Set<EntityKey> _folding = <EntityKey>{};
  final Map<String, _Projection> _projections = <String, _Projection>{};
  int _ids = 0;
  int _stamp = 0;
  int _temps = 0;

  /// A key for an entity the server has not created yet. The `~opt` prefix
  /// cannot collide with an id a server issues.
  String mint() => '~opt${++_temps}';

  /// Bumps on every push and every drop.
  int get version => _stamp;

  /// The early-out every hot path checks first.
  bool get empty => _entries.isEmpty;

  /// The live overlays, in push order. A copy.
  List<OverlayEntry> list() => List.unmodifiable(_entries);

  /// Every key any live overlay touches.
  Set<EntityKey> keys() => {
    for (final entry in _entries) ...entry.patches.keys,
  };

  @override
  bool holds(EntityKey key) =>
      _entries.any((entry) => entry.patches.containsKey(key));

  @override
  EntityRecord? effective(EntityKey key) {
    // A key no layer touches folds to its base record. That is not worth a
    // memo, and a memo of it would outlive the record: only a layer's
    // settling and a store write or eviction (`rebase`) drop one, so a base
    // record memoized here would survive `store.clear()` and read back as
    // the previous principal's data.
    if (!holds(key)) return _host.getRecord(key);

    // Reached from inside another key's fold, through a compute's view. A
    // fold computed there saw its neighbour's base, so it is not memoized.
    if (_folding.isNotEmpty) {
      return _folding.contains(key) ? _host.getRecord(key) : _fold(key);
    }

    if (_folded.containsKey(key)) return _folded[key];

    final record = _fold(key);
    _folded[key] = record;

    return record;
  }

  @override
  void rebase(EntityKey key) {
    _folded.remove(key);
  }

  /// A query's value with every matching pending placement applied, memoized
  /// on the stack version and the base identity.
  Object? project(String key, Object? base, QueryEntry? entry) {
    if (_entries.isEmpty) return base;

    final cached = _projections[key];

    if (cached != null &&
        cached.version == _stamp &&
        identical(cached.base, base)) {
      return cached.value;
    }

    final value = _placeAll(base, entry);
    _projections[key] = _Projection(_stamp, base, value);

    return value;
  }

  /// Whether any live overlay reaches this query. Drives `isOptimistic`.
  bool affects(QueryEntry? entry) {
    if (_entries.isEmpty || entry == null) return false;

    for (final overlay in _entries) {
      if (overlay.patches.keys.any(entry.deps.contains)) return true;

      // A tag match only counts once `_placeAll` would act on it.
      if (overlay.place == null || overlay.created == null) continue;

      if (overlay.tags.any(entry.tags.contains)) return true;
    }

    return false;
  }

  Object? _placeAll(Object? base, QueryEntry? entry) {
    if (entry == null) return base;

    var current = base;

    for (final overlay in _entries) {
      final place = overlay.place;
      final created = overlay.created;

      if (place == null || created == null) continue;

      final matched = overlay.tags.where(entry.tags.contains).toList();

      if (matched.isEmpty) continue;

      // Still loading: nothing to place into yet, and not an error.
      if (current == null) continue;

      // A placement returns a list, so it cannot place into an envelope.
      if (current is! List<Object?>) {
        _report?.call(
          StateError(
            '[forge] optimistic: place cannot be applied to a non-array query value',
          ),
          'optimistic',
        );
        continue;
      }

      final made = _host.read(makeRef(created));
      var next = current;
      var placed = true;

      for (final tag in matched) {
        final callback = place[tag];

        // All or nothing per overlay.
        if (callback == null) {
          placed = false;
          break;
        }

        List<Object?>? result;

        try {
          result = callback(made, next, entry.args);
        } on Object catch (error) {
          _report?.call(error, 'optimistic');
          placed = false;
          break;
        }

        if (result == null) {
          placed = false;
          break;
        }

        next = result;
      }

      if (placed) current = next;
    }

    return current;
  }

  /// Pushes one overlay and returns its id.
  int add(
    Map<EntityKey, EntityPatch> patches, [
    Map<String, Placement>? place,
    List<String> tags = const [],
    EntityKey? created,
  ]) {
    final entry = OverlayEntry(
      id: ++_ids,
      patches: patches,
      place: place,
      tags: tags,
      created: created,
    );

    _entries.add(entry);
    _settle(entry.patches.keys);

    return entry.id;
  }

  /// Removes one overlay and returns it. The whole of rollback.
  OverlayEntry? take(int id) {
    final at = _entries.indexWhere((entry) => entry.id == id);

    if (at < 0) return null;

    final entry = _entries.removeAt(at);
    _settle(entry.patches.keys);

    return entry;
  }

  /// Makes a taken overlay's effects permanent in base and reports the keys
  /// that end up with no record, so the response commit can skip them.
  ///
  /// A create is never promoted: the real entity arrives in the response. A
  /// patch for a key in [overtaken] (a stream frame wrote it while the
  /// mutation was in flight) is discarded rather than promoted.
  List<EntityKey> promote(OverlayEntry entry, [Set<EntityKey>? overtaken]) {
    final buried = <EntityKey>[];

    for (final MapEntry(:key, value: patch) in entry.patches.entries) {
      final raced = overtaken?.contains(key) ?? false;

      switch (patch) {
        case CreatePatch():
          continue;
        case DeletePatch():
          // Under a fresh stamp, as a frame's eviction is, so a read already in
          // flight cannot put the row back.
          if (!raced) _host.evict(key, _host.nextFrame());

          if (_host.getRecord(key) == null) buried.add(key);
        case MergePatch():
          if (raced) continue;

          final base = _host.getRecord(key);

          if (base == null) continue;

          _host.put(key, _apply(key, patch, base.data, promote: true));
      }
    }

    return buried;
  }

  /// Drops everything. A pending edit is not portable across identities.
  void clear() {
    // Whatever the stack held, nothing folded for the previous identity may
    // answer for the next one.
    _folded.clear();

    if (_entries.isEmpty) return;

    final touched = keys();
    _entries.clear();
    _settle(touched);
  }

  void _settle(Iterable<EntityKey> keys) {
    final touched = keys.toList();

    touched.forEach(_folded.remove);

    _stamp++;
    _host.touch(touched);
    _projections.clear();
  }

  EntityRecord? _fold(EntityKey key) {
    final base = _host.getRecord(key);
    Json? data = base?.data;
    var touched = false;

    _folding.add(key);

    try {
      for (final entry in _entries) {
        final patch = entry.patches[key];

        if (patch == null) continue;

        touched = true;

        switch (patch) {
          case DeletePatch():
            data = null;
          case CreatePatch(:final fields):
            data = {...?data, ...fields};
          case MergePatch():
            // A merge over a hole patches nothing.
            final current = data;
            if (current != null) {
              data = {...current, ..._apply(key, patch, current)};
            }
        }
      }
    } finally {
      _folding.remove(key);
    }

    if (!touched) return base;
    if (data == null) return null;

    return EntityRecord(
      data: data,
      version: base?.version ?? 1,
      frameAt: base?.frameAt ?? 0,
    );
  }

  /// The fields [patch] sets on [stored], the record of [key].
  ///
  /// A compute sees the record the way a read does, with every reference
  /// resolved: a typed spec decodes it with the model's `fromClient`, which
  /// knows nothing of references. What it returns is compared field by field
  /// with that view, and a field it left as it was is dropped, so the record
  /// keeps its reference there and goes on tracking the embedded entity. A
  /// changed field is kept in client shape for the fold, which shows exactly
  /// what the caller wrote, and normalized for [promote] (see [_promoted]).
  ///
  /// A throwing compute is reported and treated as no change.
  Json _apply(
    EntityKey key,
    MergePatch patch,
    Json stored, {
    bool promote = false,
  }) {
    final compute = patch.compute;

    if (compute == null) return patch.fields;

    final view = _view(key, stored);
    final Json result;

    try {
      result = compute(view);
    } on Object catch (error) {
      _report?.call(error, 'optimistic');

      return const {};
    }

    final changes = <String, Object?>{};

    for (final MapEntry(key: field, :value) in result.entries) {
      if (view.containsKey(field) && _same(value, view[field], null)) continue;

      changes[field] = promote ? _promoted(key, field, value) : value;
    }

    return changes;
  }

  /// [stored] with every reference resolved through the overlay, the value a
  /// read of [key] materializes. Built fresh and memo-free: it runs inside the
  /// store's own walk, whose memos it must not disturb. A cycle closes on the
  /// object already built for that key, as it does in a read.
  Json _view(EntityKey key, Json stored) {
    final built = <EntityKey, Json>{};
    final out = <String, Object?>{};

    built[key] = out;

    for (final MapEntry(key: field, value: child) in stored.entries) {
      out[field] = _resolve(child, built);
    }

    return out;
  }

  Object? _resolve(Object? node, Map<EntityKey, Json> built) {
    if (node is EntityRef) {
      final seen = built[node.key];

      if (seen != null) return seen;

      final record = effective(node.key);

      if (record == null) return null;

      final out = <String, Object?>{};

      built[node.key] = out;

      for (final MapEntry(key: field, value: child) in record.data.entries) {
        out[field] = _resolve(child, built);
      }

      return out;
    }

    // An unmarked container holds no reference, as in a read.
    if (node is List<Object?>) {
      if (!isRewritten(node)) return node;

      return [
        for (final element in node)
          // A reference whose record is gone is a hole and is dropped.
          if (_resolve(element, built) case final value
              when value != null || element is! EntityRef)
            value,
      ];
    }

    if (node is Map<String, Object?>) {
      if (!isRewritten(node)) return node;

      return {
        for (final MapEntry(key: field, value: child) in node.entries)
          field: _resolve(child, built),
      };
    }

    return node;
  }

  /// A changed field on its way into base. When the schema says the field
  /// holds entities and base holds every one it names, the field is written
  /// as references, the shape a server response would leave. Otherwise it is
  /// written as the caller gave it: a reference to a record base does not
  /// hold would read back as a hole, where the inline value still reads back
  /// as what was shown. The embedded records themselves are never written:
  /// this patch targets [key] alone, and the response committed straight
  /// after carries the server's version of them.
  Object? _promoted(EntityKey key, String field, Object? value) {
    final colon = key.indexOf(':');
    final type = colon <= 0 ? null : key.substring(0, colon);
    final fieldType = type == null ? null : _entities[type]?.fields[field];

    if (fieldType == null) return value;

    final normalized = normalize(value, fieldType, _entities);

    if (normalized.records.keys.any((held) => _host.getRecord(held) == null)) {
      return value;
    }

    return normalized.skeleton;
  }
}

/// Deep equality between a compute's result and its view, seeing through
/// references and terminating on the cycles a view can hold. [route] holds the
/// containers on the path from the root, as in the store's own comparison.
bool _same(Object? a, Object? b, Set<Object>? route) {
  if (sameValue(a, b)) return true;
  if (a == null || b == null) return false;
  if (a is EntityRef || b is EntityRef) return false;

  if (a is List<Object?> && b is List<Object?>) {
    if (a.length != b.length) return false;

    final path = route ?? Set<Object>.identity();

    if (!path.add(a)) return true;

    try {
      for (var i = 0; i < a.length; i++) {
        if (!_same(a[i], b[i], path)) return false;
      }
    } finally {
      path.remove(a);
    }

    return true;
  }

  if (a is Map<String, Object?> && b is Map<String, Object?>) {
    if (a.length != b.length) return false;

    final path = route ?? Set<Object>.identity();

    if (!path.add(a)) return true;

    try {
      for (final MapEntry(:key, :value) in a.entries) {
        if (!b.containsKey(key) || !_same(value, b[key], path)) return false;
      }
    } finally {
      path.remove(a);
    }

    return true;
  }

  return a == b;
}

/// A mutation declares more than one entity, so the one it changes cannot be
/// derived. Pass explicit keys instead.
final class AmbiguousTargetError implements Exception {
  /// Creates the error.
  const AmbiguousTargetError(this.operation, this.keys);

  /// `PATCH /orders/{id}`.
  final String operation;

  /// Every entity key the mutation's tags named.
  final List<EntityKey> keys;

  @override
  String toString() =>
      '[forge] optimistic: $operation invalidates more than one entity (${keys.join(', ')}), '
      'so its target cannot be derived. Pass explicit keys instead.';
}

/// The single entity a mutation changes, read out of what it invalidates.
///
/// A tag names an entity key when it has a `Type:` head that is not a
/// collection (`Order[]:{req.archived}` is a collection). Returns the key,
/// null when no entity is named (a create), or throws
/// [AmbiguousTargetError] when more than one is.
EntityKey? targetOf(OperationMeta meta, TagContext args) {
  final keys = <EntityKey>[];

  for (final template in meta.invalidates) {
    final colon = template.indexOf(':');

    if (colon <= 0) continue;
    if (template.substring(0, colon).endsWith('[]')) continue;

    final tag = resolveTag(template, args);

    if (tag == null || keys.contains(tag)) continue;

    keys.add(tag);
  }

  if (keys.length > 1) throw AmbiguousTargetError(operationName(meta), keys);

  return keys.isEmpty ? null : keys.single;
}

/// What a mutation shows immediately and reconciles on settle.
///
/// A patch, never a value: it is re-applied on every refold, so two
/// concurrent mutations against one entity compose. At the cache level
/// (`MutateOptions.optimistic`) every spec is client-shaped, `E` is
/// `Object?` and the value is a `Json`; `MutationBinding` converts typed specs
/// with the binding's codecs before calling the cache.
sealed class Optimistic<E> {
  const Optimistic();
}

/// Replaces the target's fields with what [update] returns for the current
/// value. [key] null derives the target with [targetOf].
final class OptimisticUpdate<E> extends Optimistic<E> {
  /// Creates the spec.
  const OptimisticUpdate(this.update, {this.key});

  /// Computes the new value from the current one. Re-run on every refold.
  final E Function(E previous) update;

  /// The entity, or null to derive it.
  final EntityKey? key;

  /// Runs [update] over a client-shaped [previous]: decoded with [decode],
  /// checked against [E], and encoded back with [encode].
  ///
  /// For `MutationBinding`. Reading [update] through a wider view (an
  /// `OptimisticUpdate<Json>` held as `Optimistic<Object?>`) fails Dart's
  /// covariance check, so the function is only ever called from in here,
  /// where its type is exact. The codecs take `Object?` for the same reason:
  /// a parameter typed with [E] would be checked against the runtime [E].
  Object? applyClient(
    Object? previous,
    Object? Function(Object? client) decode,
    Object? Function(Object? model) encode,
  ) => encode(update(decode(previous) as E));
}

/// Removes the target. [key] null derives it with [targetOf].
final class OptimisticDelete<E> extends Optimistic<E> {
  /// Creates the spec.
  const OptimisticDelete({this.key});

  /// The entity, or null to derive it.
  final EntityKey? key;
}

/// Shows [value] as a new record of the mutation's entity under a minted
/// `~opt` key until the server answers. Pair it with `place` to put the row in
/// a list.
final class OptimisticCreate<E> extends Optimistic<E> {
  /// Creates the spec.
  const OptimisticCreate(this.value);

  /// The record to show. Its identity field is overwritten with the minted id.
  final E value;

  /// [value] in client shape, encoded with [encode]. For `MutationBinding`,
  /// alongside [OptimisticUpdate.applyClient].
  Object? encodeClient(Object? Function(Object? model) encode) => encode(value);
}

/// Several explicitly keyed patches: the escape hatch for a multi-entity
/// write. Every patch must be an [OptimisticUpdate] or [OptimisticDelete]
/// that names its key.
final class OptimisticMany extends Optimistic<Object?> {
  /// Creates the spec.
  const OptimisticMany(this.patches);

  /// The patches, client-shaped.
  final List<Optimistic<Object?>> patches;
}

/// Keyed patches, plus the minted key when the spec was a create.
typedef ResolvedPatches = ({
  Map<EntityKey, EntityPatch> patches,
  EntityKey? created,
});

/// Translates what the caller declared into keyed patches.
///
/// Returns null when the target cannot be decided, having reported why under
/// the context `optimistic`. Reporting rather than throwing is deliberate: a
/// throw would reject the mutation before it was dispatched, and not being
/// optimistic is a far smaller failure than not writing.
ResolvedPatches? specToPatches(
  Optimistic<Object?> spec,
  OperationMeta meta,
  TagContext args,
  EntitySchema entities,
  String Function() mintId,
  void Function(Object error, String context)? report,
) {
  void refuse(String why) => report?.call(
    StateError('[forge] optimistic: ${operationName(meta)} $why'),
    'optimistic',
  );

  MergePatch merge(Object? Function(Object? previous) update) =>
      MergePatch.computed((previous) => update(previous)! as Json);

  switch (spec) {
    case OptimisticMany(:final patches):
      final keyed = <EntityKey, EntityPatch>{};

      for (final one in patches) {
        switch (one) {
          case OptimisticUpdate(:final update, key: final key?):
            keyed[key] = merge(update);
          case OptimisticDelete(key: final key?):
            keyed[key] = const DeletePatch();
          default:
            refuse('has a patch in OptimisticMany that names no key.');

            return null;
        }
      }

      return (patches: keyed, created: null);

    case OptimisticCreate(:final value):
      final type = meta.entity;
      final idField = type == null ? null : entities[type]?.idField;

      if (type == null || idField == null) {
        refuse(
          'names no entity to create. Pass OptimisticMany with explicit keys instead.',
        );

        return null;
      }

      final id = mintId();
      final key = '$type:$id';

      return (
        patches: {
          key: CreatePatch({...?(value as Json?), idField: id}),
        },
        created: key,
      );

    case OptimisticUpdate(:final key) || OptimisticDelete(:final key):
      EntityKey? target;

      try {
        target = key ?? targetOf(meta, args);
      } on AmbiguousTargetError catch (error) {
        report?.call(error, 'optimistic');

        return null;
      }

      if (target == null) {
        refuse(
          'names no entity to patch. Pass OptimisticMany with explicit keys instead.',
        );

        return null;
      }

      final EntityPatch patch = switch (spec) {
        OptimisticUpdate(:final update) => merge(update),
        _ => const DeletePatch(),
      };

      return (patches: {target: patch}, created: null);
  }
}
