import 'dart:collection';

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
    this.pushedAt = 0,
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

  /// The store's frame-clock reading when the entry was pushed. A record a
  /// frame wrote after it is newer than anything this entry could promote.
  final int pushedAt;
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
  /// field is promoted in client shape. [_frames] reads the store's frame
  /// clock, stamped on every entry as [OverlayEntry.pushedAt].
  OverlayStack(
    this._host, [
    this._report,
    this._entities = const {},
    this._frames,
  ]);

  final int Function()? _frames;

  final OverlayHost _host;
  final void Function(Object error, String context)? _report;
  final EntitySchema _entities;
  final List<OverlayEntry> _entries = <OverlayEntry>[];
  final Map<EntityKey, EntityRecord?> _folded = <EntityKey, EntityRecord?>{};

  /// The keys whose fold is on the stack right now. A compute's view that
  /// reaches one of them again (an order's customer lists the order) reads
  /// its base rather than folding it a second time.
  final Set<EntityKey> _folding = <EntityKey>{};

  /// Neighbours folded while the outermost fold builds its views, each once.
  /// Dropped when that fold returns: a fold made here saw the keys on the
  /// path as their base, so it cannot answer for a top-level read.
  final Map<EntityKey, EntityRecord?> _nested = <EntityKey, EntityRecord?>{};

  /// Every key the outermost fold's views resolved, while one runs.
  Set<EntityKey>? _reads;

  /// For each key, the folded keys whose computes saw it. A write to the key
  /// drops those folds, so a compute that read an embedded entity runs again
  /// when the entity changes.
  final Map<EntityKey, Set<EntityKey>> _readers = <EntityKey, Set<EntityKey>>{};
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
    // key already on the path reads its base, which is what ends a cycle.
    // Any other key is folded once for the whole outermost fold, which keeps
    // a fold linear in the records it reaches.
    if (_folding.isNotEmpty) {
      if (_folding.contains(key)) return _host.getRecord(key);
      if (_nested.containsKey(key)) return _nested[key];

      final record = _fold(key);
      _nested[key] = record;

      return record;
    }

    if (_folded.containsKey(key)) return _folded[key];

    final record = _fold(key);
    _folded[key] = record;

    return record;
  }

  @override
  void rebase(EntityKey key) {
    _folded.remove(key);

    final readers = _readers.remove(key);

    if (readers == null || readers.isEmpty) return;

    readers.forEach(_folded.remove);
    _host.touch(readers);
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
      pushedAt: _frames?.call() ?? 0,
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
  ///
  /// A merge that changed an embedded entity writes that entity too (see
  /// [_promoted]), except where [held] says another writer owns the key now:
  /// a frame that wrote it in flight, or a sync source.
  List<EntityKey> promote(
    OverlayEntry entry, [
    Set<EntityKey>? overtaken,
    bool Function(EntityKey key)? held,
  ]) {
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

          _host.put(
            key,
            _apply(key, patch, base.data, promote: held ?? _never),
          );
      }
    }

    return buried;
  }

  /// Drops everything. A pending edit is not portable across identities.
  void clear() {
    // Whatever the stack held, nothing folded for the previous identity may
    // answer for the next one.
    _folded.clear();
    _readers.clear();

    if (_entries.isEmpty) return;

    final touched = keys();
    _entries.clear();
    _settle(touched);
  }

  void _settle(Iterable<EntityKey> keys) {
    final touched = keys.toList();

    // A fold whose compute read a settling key is as stale as the key's own.
    final readers = <EntityKey>{
      for (final key in touched) ...?_readers.remove(key),
    };

    touched.forEach(_folded.remove);
    readers.forEach(_folded.remove);

    if (_entries.isEmpty) _readers.clear();

    _stamp++;
    _host.touch({...touched, ...readers});
    _projections.clear();
  }

  EntityRecord? _fold(EntityKey key) {
    final base = _host.getRecord(key);
    Json? data = base?.data;
    var touched = false;
    final outermost = _folding.isEmpty;

    if (outermost) _reads = <EntityKey>{};

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

      if (outermost) {
        final reads = _reads!;

        _reads = null;
        _nested.clear();

        for (final read in reads) {
          (_readers[read] ??= <EntityKey>{}).add(key);
        }
      }
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
  /// keeps its reference there and goes on tracking the embedded entity.
  ///
  /// A changed field is relinked: any entity it still holds exactly as the
  /// view resolved it (the caller kept the object) goes back to a reference.
  /// Whatever the caller built is kept in client shape in the fold, which
  /// shows exactly what was written. On promotion ([promote] non-null) the
  /// view is built from base alone, so no other mutation's pending patch can
  /// reach what is written, and the field is normalized (see [_promoted]).
  ///
  /// A throwing compute is reported and treated as no change. Only the fold
  /// a read asked for reports it: a neighbour folded for a view is folded,
  /// and reported, by its own read.
  Json _apply(
    EntityKey key,
    MergePatch patch,
    Json stored, {
    bool Function(EntityKey key)? promote,
  }) {
    final compute = patch.compute;

    if (compute == null) return patch.fields;

    final origins = Map<Object, EntityKey>.identity();
    final scope = _Scope(this, origins, promote != null);
    final view = _view(key, stored, scope);

    try {
      final Json result;

      try {
        result = compute(view);
      } on Object catch (error) {
        if (_folding.length <= 1) _report?.call(error, 'optimistic');

        return const {};
      }

      final changes = <String, Object?>{};

      for (final MapEntry(key: field, :value) in _fieldsOf(result)) {
        if (view.containsKey(field) && _same(value, view[field], null)) {
          continue;
        }

        final relinked = _relink(
          value,
          origins,
          Map<Object, Object?>.identity(),
        );

        // Back to exactly what the record stores (a kept `previous` returned
        // as it was, its references never read): nothing changed.
        if (stored.containsKey(field) && _same(relinked, stored[field], null)) {
          continue;
        }

        if (promote == null) {
          changes[field] = relinked;
          continue;
        }

        final promoted = _promoted(key, field, relinked, promote);

        if (!identical(promoted, _unpromoted)) changes[field] = promoted;
      }

      return changes;
    } finally {
      // A `previous` the compute kept must never look anything up again: by
      // the time it is read the store may hold another principal's records.
      scope.sealed = true;
    }
  }

  /// [stored] with every reference resolved, the value a read of [key]
  /// materializes: through the overlay, or through base alone when the
  /// [scope] is a promotion's. The scope's `origins` receives every entity
  /// view made, with its key.
  ///
  /// Lazy: a field is resolved the first time something reads it, so a
  /// compute that spreads `previous` and sets one field never folds a
  /// neighbour, and only a compute that reads into an embedded entity pays
  /// for it. Memo-free with respect to the store: it runs inside the store's
  /// own walk, whose memos it must not disturb. A cycle closes on the view
  /// already made for that key, as it does in a read.
  Json _view(EntityKey key, Json stored, _Scope scope) {
    final root = _View(scope, stored);

    scope.built[key] = root;
    scope.origins[root] = key;

    return root;
  }

  /// One reference resolved for a view in [scope].
  Object? _resolveRef(EntityRef ref, _Scope scope) {
    final seen = scope.built[ref.key];

    if (seen != null) return seen;

    _reads?.add(ref.key);

    final record = scope.fromBase
        ? _host.getRecord(ref.key)
        : effective(ref.key);

    if (record == null) return null;

    final view = _View(scope, record.data);

    scope.built[ref.key] = view;
    scope.origins[view] = ref.key;

    return view;
  }

  /// [value] with every entity object a view built put back as a reference,
  /// and every container that gained one rebuilt and marked, as `normalize`
  /// marks a skeleton. Objects the caller made are kept; [done] ends a cycle
  /// among them.
  ///
  /// A view from any compute is recognised, including a sealed one an
  /// earlier compute kept and this one returned (an undo). An entity view
  /// becomes its reference. Any other view is rebuilt from what it resolved
  /// and, for the fields it never read, from the stored skeleton, so a
  /// reference it never followed stays a reference rather than reading back
  /// as the null its seal answers. Nothing is looked up to do this.
  Object? _relink(
    Object? value,
    Map<Object, EntityKey> origins,
    Map<Object, Object?> done,
  ) {
    if (value is! List<Object?> && value is! Map<String, Object?>) {
      return value;
    }

    final origin =
        origins[value!] ??
        (value is _View ? value._scope.origins[value] : null);

    if (origin != null) return makeRef(origin);
    if (done.containsKey(value)) return done[value];

    var changed = false;

    if (value is List<Object?>) {
      final out = <Object?>[];

      done[value] = out;

      for (final element in value) {
        final linked = _relink(element, origins, done);

        if (!identical(linked, element)) changed = true;

        out.add(linked);
      }

      if (!changed) return done[value] = value;

      return markRewritten(out);
    }

    final source = value as Map<String, Object?>;
    final out = <String, Object?>{};

    // A view never leaves its compute: it resolves lazily, against whatever
    // the overlay holds when it is read.
    if (source is _View) changed = true;

    done[value] = out;

    for (final MapEntry(key: field, value: child) in _fieldsOf(source)) {
      final linked = _relink(child, origins, done);

      if (!identical(linked, child)) changed = true;

      out[field] = linked;
    }

    if (!changed) return done[value] = value;

    return markRewritten(out);
  }

  /// A changed, relinked field on its way into base, normalized against the
  /// schema so it is written as references, the shape a response leaves. No
  /// embedded entity is ever written inline, so nothing written can hold a
  /// cycle, and a later write to the entity shows through.
  ///
  /// Each embedded entity is written too, as what was shown while the write
  /// was pending: a record base did not hold is written whole, and a held one
  /// gets the fields that differ from base. The view was built from base, so
  /// an entity the caller did not edit matches base and writes nothing. A key
  /// [held] claims (overtaken by a frame, or owned by a sync source) and the
  /// target itself are left alone.
  Object? _promoted(
    EntityKey key,
    String field,
    Object? value,
    bool Function(EntityKey key) held,
  ) {
    final colon = key.indexOf(':');
    final type = colon <= 0 ? null : key.substring(0, colon);
    final fieldType = type == null ? null : _entities[type]?.fields[field];

    // A key or an id the stack minted belongs to a create that has not been
    // answered. It is never written, and nothing written may name it: a list
    // element that does is dropped from the list, and a field that does
    // anywhere else is left for the response, which carries the server's
    // own answer (see [_withoutMinted]).
    if (fieldType == null) return _withoutMinted(value);

    final normalized = normalize(value, fieldType, _entities);
    final skeleton = _withoutMinted(normalized.skeleton);

    if (identical(skeleton, _unpromoted)) return _unpromoted;

    for (final MapEntry(key: entity, value: fields)
        in normalized.records.entries) {
      if (_isMinted(entity) || entity == key || held(entity)) continue;

      final kept = <String, Object?>{
        for (final MapEntry(key: name, :value) in fields.entries)
          if (_withoutMinted(value) case final clean
              when !identical(clean, _unpromoted))
            name: clean,
      };
      final base = _host.getRecord(entity)?.data;

      if (base == null) {
        _host.put(entity, kept);
        continue;
      }

      final changed = <String, Object?>{
        for (final MapEntry(key: name, :value) in kept.entries)
          if (!base.containsKey(name) || !_same(value, base[name], null))
            name: value,
      };

      if (changed.isNotEmpty) _host.put(entity, changed);
    }

    return skeleton;
  }

  /// [node] with every list element that names a minted key or id dropped,
  /// or [_unpromoted] when one sits anywhere a list element does not hold
  /// it: a field, or the whole of [node]. Dropping an element writes the rest
  /// of the list's change, which is what the caller asked for apart from the
  /// record the server has not made yet.
  Object? _withoutMinted(Object? node) {
    final clean = _stripMinted(node, Set<Object>.identity());

    return identical(clean, _minted) ? _unpromoted : clean;
  }

  /// [_withoutMinted]'s walk: [_minted] when [node] is itself minted,
  /// [_unpromoted] when a map beneath it names a minted key, otherwise [node]
  /// or its copy without the minted list elements. [route] ends a cycle a
  /// caller built.
  Object? _stripMinted(Object? node, Set<Object> route) {
    switch (node) {
      case EntityRef(:final key):
        return _isMinted(key) ? _minted : node;
      case String():
        return _isMintedId(node) ? _minted : node;
      case List<Object?>():
        if (!route.add(node)) return node;

        try {
          final out = <Object?>[];
          var changed = false;

          for (final element in node) {
            final clean = _stripMinted(element, route);

            if (identical(clean, _minted) || identical(clean, _unpromoted)) {
              changed = true;
              continue;
            }

            if (!identical(clean, element)) changed = true;

            out.add(clean);
          }

          if (!changed) return node;

          return isRewritten(node) ? markRewritten(out) : out;
        } finally {
          route.remove(node);
        }
      case Map<String, Object?>():
        if (!route.add(node)) return node;

        try {
          final out = <String, Object?>{};
          var changed = false;

          for (final MapEntry(:key, :value) in node.entries) {
            final clean = _stripMinted(value, route);

            if (identical(clean, _minted) || identical(clean, _unpromoted)) {
              return _unpromoted;
            }

            if (!identical(clean, value)) changed = true;

            out[key] = clean;
          }

          if (!changed) return node;

          return isRewritten(node) ? markRewritten(out) : out;
        } finally {
          route.remove(node);
        }
      default:
        return node;
    }
  }

  /// Whether [value] is an id this stack minted, `~opt3`, as a scalar foreign
  /// key next to its entity (`customerId`) would hold it.
  bool _isMintedId(String value) {
    if (!value.startsWith('~opt')) return false;

    final n = int.tryParse(value.substring(4));

    return n != null && n >= 1 && n <= _temps && value == '~opt$n';
  }
}

bool _never(EntityKey _) => false;

/// What [OverlayStack._promoted] returns for a field it leaves alone.
const Object _unpromoted = Object();

/// What [OverlayStack._stripMinted] returns for a node that is itself minted.
const Object _minted = Object();

/// Whether [key] is one [OverlayStack.mint] made: `Type:~opt1`.
bool _isMinted(EntityKey key) => key.contains(':~opt');

/// The fields of [map] as a diff or a relink must see them. A view answers
/// with what it resolved and, for a field it never read, with the stored
/// value itself. Reading a view here therefore looks nothing up, and a
/// sealed view's null never stands in for a reference it did not follow.
Iterable<MapEntry<String, Object?>> _fieldsOf(Map<String, Object?> map) {
  if (map is! _View) return map.entries;

  return [
    for (final key in map.keys)
      MapEntry(
        key,
        map._local.containsKey(key) ? map._local[key] : map._source[key],
      ),
  ];
}

/// What the views of one compute share: the entity views made so far, by key
/// and by identity, and where references resolve.
final class _Scope {
  _Scope(this.stack, this.origins, this.fromBase);

  final OverlayStack stack;
  final Map<Object, EntityKey> origins;
  final bool fromBase;
  final Map<EntityKey, _View> built = <EntityKey, _View>{};

  /// Set when the compute that owns these views returns. From then on a view
  /// answers only from what it already holds and looks nothing up.
  bool sealed = false;

  Object? resolve(Object? node) {
    if (node is EntityRef) return stack._resolveRef(node, this);

    // An unmarked container holds no reference, as in a read.
    if (node is List<Object?>) {
      if (!isRewritten(node)) return node;

      return [
        for (final element in node)
          // A reference whose record is gone is a hole and is dropped.
          if (resolve(element) case final value
              when value != null || element is! EntityRef)
            value,
      ];
    }

    if (node is Map<String, Object?>) {
      return isRewritten(node) ? _View(this, node) : node;
    }

    return node;
  }
}

/// A record or a marked container, its references resolved on first read.
/// Writable, as the plain map it stands for would be: a write lands in the
/// view and never reaches the record it reads from.
final class _View extends MapBase<String, Object?> {
  _View(this._scope, this._source);

  final _Scope _scope;
  final Json _source;
  final Map<String, Object?> _local = <String, Object?>{};
  final Set<String> _removed = <String>{};

  /// Once the scope is sealed, a field never read before answers with its
  /// stored value when that is plain data (part of the record this view
  /// already holds) and null when it would have to follow a reference.
  /// Null rather than a throw: a kept `previous` read later, from a log line
  /// or an undo buffer, must not take the app down.
  @override
  Object? operator [](Object? key) {
    if (key is! String) return null;
    if (_local.containsKey(key)) return _local[key];
    if (_removed.contains(key) || !_source.containsKey(key)) return null;

    final stored = _source[key];

    if (_scope.sealed) return _holdsReference(stored) ? null : stored;

    return _local[key] = _scope.resolve(stored);
  }

  static bool _holdsReference(Object? node) =>
      node is EntityRef ||
      ((node is List<Object?> || node is Map<String, Object?>) &&
          isRewritten(node!));

  @override
  void operator []=(String key, Object? value) {
    _removed.remove(key);
    _local[key] = value;
  }

  @override
  bool containsKey(Object? key) =>
      _local.containsKey(key) ||
      (!_removed.contains(key) && _source.containsKey(key));

  @override
  Iterable<String> get keys => {
    for (final key in _source.keys)
      if (!_removed.contains(key)) key,
    ..._local.keys,
  };

  @override
  int get length => keys.length;

  @override
  Object? remove(Object? key) {
    final value = this[key];

    if (key is String) {
      _local.remove(key);
      _removed.add(key);
    }

    return value;
  }

  @override
  void clear() {
    _removed.addAll(_source.keys);
    _local.clear();
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
/// derived. Pass `key:` instead, or [OptimisticMany] on an untyped binding.
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
      'so its target cannot be derived. Pass key: to name the record to patch, '
      'or, on an untyped binding, OptimisticMany with a key on each patch to '
      'change several.';
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
///
/// `previous` is the record the way a read shows it, not the stored record:
/// every embedded entity is resolved, through whatever other mutations have
/// pending. That is what a typed spec needs to decode it. It differs from the
/// TypeScript runtime, whose function patch gets the raw record with its
/// references. Two consequences for an untyped `OptimisticUpdate<Json>`:
///
/// * `previous` can be cyclic. An order's customer can list the order, and
///   that list then holds `previous` itself, as a read's value would. Spread
///   it (`{...previous, 'status': 'shipped'}`) rather than walking, copying or
///   `jsonEncode`-ing it whole.
/// * Only what [update] changes is written. A field returned as it was keeps
///   the stored reference, so the record goes on tracking the embedded
///   entity, and a change to that entity runs [update] again.
///
/// A change to an embedded entity made through the parent (a new customer
/// name on the order) shows at once, and on success it is written to that
/// entity's own record as well as to the parent. While it is pending, a
/// typed update shows that entity with the fields its model declares and no
/// others; base keeps the undeclared ones, and promotion merges into them.
///
/// `previous` is good for the length of [update]. Keeping it is fine (for an
/// undo, `(_) => saved` returns what it held), but read later it answers
/// only what [update] read while it ran, and null for anything it would have
/// to look up. Returned from a later update, it writes what it held, and a
/// reference it never followed stays a reference.
///
/// A record an optimistic create has not had answered yet (its key is
/// `Type:~opt1`) is never written to base, and neither is anything that
/// names it. A list element that names one, as a reference or as a scalar
/// id, is dropped from the list and the rest of the list's change is
/// promoted. Anywhere else, such as `customer` or a `customerId` holding a
/// minted id, the whole field is left as base held it. The response carries
/// the server's answer for it.
final class OptimisticUpdate<E> extends Optimistic<E> {
  /// Creates the spec.
  const OptimisticUpdate(this.update, {this.key});

  /// Computes the new value from the current one. Re-run on every refold.
  ///
  /// Read the record and the entities embedded in it directly, such as an
  /// order and its customer. Each embedded entity you read is folded with its
  /// own pending changes first, so a compute that walks further, through the
  /// customer to all of the customer's orders and their line items, does
  /// more work for every record it reaches. Across a large connected graph
  /// with many pending changes that cost adds up on every refold.
  ///
  /// The size of it: with n pending records that can all reach each other
  /// through what their updates read, one read of them runs about n squared
  /// computes, about 1.2 s at n = 200. A write to any entity a compute read
  /// pays that again, because each of those computes reruns. Updates that
  /// read only their own record and its direct embeds stay linear.
  final E Function(E previous) update;

  /// The entity, or null to derive it.
  final EntityKey? key;

  /// Runs [update] over a client-shaped [previous]: decoded with [decode],
  /// checked against [E], and encoded back with [encode].
  ///
  /// Returns only the fields [update] changed, judged against [previous]
  /// round-tripped through the same codecs rather than against [previous]
  /// itself. A model's `toClient` rarely reproduces the wire byte for byte
  /// (a `DateTime` respelled or cut from Go's nanoseconds to microseconds, an
  /// explicit null omitted, a field the model does not declare dropped), and
  /// none of that is a change the caller made.
  ///
  /// The comparison goes all the way down. Wherever a part of the new value
  /// encodes the same as the same part of the old one, the raw part of
  /// [previous] is returned in its place: an untouched field keeps the
  /// server's exact spelling, and an untouched embedded entity stays the
  /// object the overlay resolved, which it turns back into a reference. Only
  /// the leaves the caller changed come back encoded. A field the round trip
  /// kept and [update] removed comes back as null; one the round trip dropped
  /// and [update] did not bring back is not returned.
  ///
  /// Entities are matched by identity, not by position. A list of embedded
  /// entities is paired by entity key, so appending, removing or reordering
  /// line items keeps every untouched one as itself. An entity put where
  /// another stood (a customer swapped for another) is compared with its own
  /// record, never with the one it replaced, and one the store does not hold
  /// is written as given. Values with no identity, such as a list of plain
  /// timestamps, are kept raw only where the same index encodes the same; an
  /// edited one comes back encoded whole.
  ///
  /// A field the model does not declare cannot be carried through a typed
  /// update. On the target it is simply not returned, so it keeps its stored
  /// value. On an embedded entity the update changed, the pending view shows
  /// only the declared fields, and base keeps the rest.
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
  ) {
    final model = decode(previous) as E;
    final before = encode(model);
    final after = encode(update(model));

    if (before is! Json || after is! Json || previous is! Json) return after;

    final untouched = _Untouched(previous, decode, encode);
    final type = untouched.typeOf(untouched.keyOf(previous));

    return <String, Object?>{
      for (final MapEntry(:key, :value) in after.entries)
        if (!before.containsKey(key) || !_same(value, before[key], null))
          key: previous.containsKey(key)
              ? untouched.merge(value, before[key], previous[key], previous, [
                  key,
                ], untouched.fieldType(type, key))
              : value,
      for (final key in before.keys)
        if (!after.containsKey(key)) key: null,
    };
  }
}

/// A step on the way from the root of a `previous` to one of its parts: a
/// field name or a list index.
typedef _Path = List<Object>;

/// What [OptimisticUpdate.applyClient] keeps raw: every part of the new value
/// that encodes the same as the same entity's old value is replaced by the
/// raw part of `previous`, so it keeps the server's exact bytes, and an
/// untouched entity comes back as the view the overlay resolved, which it
/// turns back into a reference.
///
/// Entities are paired by identity, never by position. A view knows its key,
/// and the schema names each type's identity field, so a list's elements are
/// paired by entity key: an append, a removal and a reorder each keep every
/// untouched sibling as itself. An entity the caller put where another stood
/// (a customer swapped for another one) is compared with its own record,
/// round-tripped through the same codecs, and never borrows the old one's
/// bytes. One with no record anywhere is written as given. Elements with no
/// identity pair only by index, and only where they encode equal.
final class _Untouched {
  _Untouched(this.root, this.decode, this.encode)
    : scope = root is _View ? root._scope : null;

  final Json root;
  final Object? Function(Object? client) decode;
  final Object? Function(Object? model) encode;

  /// Where the root's references resolve. Null when `previous` is not a view,
  /// which leaves no schema and no identities to pair by.
  final _Scope? scope;

  EntitySchema get _schema => scope?.stack._entities ?? const {};

  /// The entity key of [node] when it is an entity view.
  EntityKey? keyOf(Object? node) =>
      node is _View ? node._scope.origins[node] : null;

  /// The typename of [key].
  String? typeOf(EntityKey? key) {
    if (key == null) return null;

    final colon = key.indexOf(':');

    return colon <= 0 ? null : key.substring(0, colon);
  }

  /// The typename the schema gives [field] of [type].
  String? fieldType(String? type, String field) =>
      type == null ? null : _schema[type]?.fields[field];

  /// The entity key [value], in client shape, would normalize to as a
  /// [type], or null when it has no identity.
  EntityKey? identity(Object? value, String? type) {
    if (value is! Map<String, Object?> || type == null) return null;

    final idField = _schema[type]?.idField;
    final id = idField == null ? null : value[idField];

    return isIdentity(id) ? entityKey(type, id!) : null;
  }

  /// [after] with the parts that encode as [before] replaced by [previous]'s.
  /// [frame] is the raw root [path] walks into from, and [type] the
  /// schema's typename at that place.
  Object? merge(
    Object? after,
    Object? before,
    Object? previous,
    Json frame,
    _Path? path,
    String? type,
  ) {
    final held = keyOf(previous);
    final here = typeOf(held) ?? type;

    if (after is Map<String, Object?>) {
      final named = identity(after, here);

      if (held != null || named != null) {
        // Another entity, or one whose identity cannot be told: nothing of
        // [previous] is its own.
        if (named == null) return after;
        if (named != held) return _elsewhere(after, named, frame, path, here);
      }
    }

    if (_same(after, before, null)) return previous;

    if (after is Map<String, Object?> &&
        before is Map<String, Object?> &&
        previous is Map<String, Object?>) {
      return <String, Object?>{
        for (final MapEntry(:key, :value) in after.entries)
          key: before.containsKey(key) && previous.containsKey(key)
              ? merge(
                  value,
                  before[key],
                  previous[key],
                  frame,
                  path == null ? null : [...path, key],
                  fieldType(here, key),
                )
              : value,
        for (final key in before.keys)
          if (!after.containsKey(key)) key: null,
      };
    }

    if (after is List<Object?> &&
        before is List<Object?> &&
        previous is List<Object?> &&
        before.length == previous.length) {
      return _list(after, before, previous, frame, path, type);
    }

    return after;
  }

  Object? _list(
    List<Object?> after,
    List<Object?> before,
    List<Object?> previous,
    Json frame,
    _Path? path,
    String? type,
  ) {
    final out = List<Object?>.filled(after.length, null);
    final used = List<bool>.filled(previous.length, false);

    // The raw part each element stands for, for the round trip below: the
    // view it was paired with or found as, or the caller's own value.
    final raw = List<Object?>.filled(after.length, null);
    final elsewhere = <int, _View>{};

    for (var j = 0; j < after.length; j++) {
      final element = after[j];
      final named = identity(element, type);

      if (named == null) {
        // No identity: only the same place, only when it encodes the same.
        final same =
            j < previous.length &&
            !used[j] &&
            keyOf(previous[j]) == null &&
            _same(element, before[j], null);

        if (same) used[j] = true;

        out[j] = raw[j] = same ? previous[j] : element;
        continue;
      }

      final at = _unused(previous, used, named);

      if (at >= 0) {
        used[at] = true;
        raw[j] = previous[at];
        out[j] = merge(
          element,
          before[at],
          previous[at],
          frame,
          path == null ? null : [...path, at],
          type,
        );
        continue;
      }

      final found = path == null ? null : _find(named);

      raw[j] = found ?? element;
      out[j] = element;

      if (found != null) elsewhere[j] = found;
    }

    if (elsewhere.isEmpty) return out;

    // An entity that came from elsewhere is compared with its own record:
    // the list is rebuilt from raw parts, each in its new place, and
    // round-tripped once.
    final trip = _roundTrip(frame, path!, raw);
    final encoded = trip?.$2;

    if (encoded is! List<Object?> || encoded.length != after.length) {
      return out;
    }

    for (final MapEntry(key: j, value: found) in elsewhere.entries) {
      out[j] = merge(after[j], encoded[j], found, trip!.$1, [...path, j], type);
    }

    return out;
  }

  /// [after] as the entity [key] it names, which [previous] at this place is
  /// not. Compared with that entity's own record when one is held anywhere,
  /// so its untouched fields keep their own bytes; written as given when not.
  Object? _elsewhere(
    Map<String, Object?> after,
    EntityKey key,
    Json frame,
    _Path? path,
    String? type,
  ) {
    final found = path == null ? null : _find(key);

    if (found == null) return after;

    final trip = _roundTrip(frame, path!, found);
    final encoded = trip?.$2;

    if (encoded is! Map<String, Object?>) return after;

    return merge(after, encoded, found, trip!.$1, path, typeOf(key) ?? type);
  }

  /// The first element of [previous] not yet [used] that is the view of
  /// [key], or -1.
  int _unused(List<Object?> previous, List<bool> used, EntityKey key) {
    for (var i = 0; i < previous.length; i++) {
      if (!used[i] && keyOf(previous[i]) == key) return i;
    }

    return -1;
  }

  /// The view of [key] in this compute's scope, resolved the way any
  /// reference in `previous` is, or null when nothing holds it.
  _View? _find(EntityKey key) {
    final scope = this.scope;

    if (scope == null) return null;

    final found = scope.stack._resolveRef(makeRef(key), scope);

    return found is _View ? found : null;
  }

  /// [frame] with the part at [path] replaced by [raw], and what that
  /// encodes to at [path] through the same codecs. Null when [path] does not
  /// fit, the substituted value does not decode, or its encoding has no such
  /// place: the caller then keeps what it had.
  (Json, Object?)? _roundTrip(Json frame, _Path path, Object raw) {
    try {
      final substituted = _substitute(frame, path, 0, raw)! as Json;
      var node = encode(decode(substituted));

      for (final step in path) {
        if (step is String && node is Map<String, Object?>) {
          node = node[step];
        } else if (step is int && node is List<Object?> && step < node.length) {
          node = node[step];
        } else {
          return null;
        }
      }

      return (substituted, node);
    } on Object {
      return null;
    }
  }

  /// [node] with the part at [path] from [at] on replaced by [raw]. Every
  /// container on the way is copied, never written; a map is wrapped, so a
  /// view's other fields stay lazy. Throws when [path] does not fit.
  Object? _substitute(Object? node, _Path path, int at, Object raw) {
    if (at == path.length) return raw;

    final step = path[at];

    if (step is String && node is Map<String, Object?>) {
      return _Swapped(node, step, _substitute(node[step], path, at + 1, raw));
    }

    if (step is int && node is List<Object?>) {
      return [...node]..[step] = _substitute(node[step], path, at + 1, raw);
    }

    throw StateError('[forge] optimistic: no $step in $node');
  }
}

/// [_source] with [field] answering [value], read-only, for a round trip of
/// one substituted part. Every other field reads through, lazily, so a view
/// stays lazy.
final class _Swapped extends UnmodifiableMapBase<String, Object?> {
  _Swapped(this._source, this._field, this._value);

  final Map<String, Object?> _source;
  final String _field;
  final Object? _value;

  @override
  Object? operator [](Object? key) => key == _field ? _value : _source[key];

  @override
  bool containsKey(Object? key) => key == _field || _source.containsKey(key);

  @override
  Iterable<String> get keys => {..._source.keys, _field};
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
        // Neither `key:` nor OptimisticMany can create, so neither is offered.
        refuse(
          'names no entity to create: an optimistic create needs the '
          'operation to name an entity with an identity field.',
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
        // `key:` first: on a typed binding OptimisticMany does not typecheck.
        refuse(
          'names no entity to patch: its invalidates holds no single entity '
          'key. Pass key: to name the record, for example '
          "key: entityKey('${meta.entity ?? 'Type'}', id), or, on an untyped "
          'binding, OptimisticMany with explicit keys.',
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
