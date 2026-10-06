/// Reading the cache, under one rule: inspection must not mutate. Port of
/// `client-devtools/src/inspect.ts`. Nothing here calls `getState`, `fetch` or
/// `peek`; every read is a [DevCache] field load or a copy of one.
///
/// Every value that leaves is a [bounded] copy, so it is capped in size and
/// cannot be written to. The collections of names (tags, dependencies) are
/// unmodifiable copies too. A panel holding a snapshot cannot move the store,
/// and a snapshot does not alias anything the cache will later change.
///
/// These functions answer from whatever cache they are given. The
/// "nothing crosses principals" rule is enforced one level up, by [Devtools],
/// which answers empty while an identity change is in progress.
library;

import '../ref.dart';
import 'explain.dart' show tagContextJson;
import 'frames.dart';
import 'seams.dart';
import 'types.dart';

const _valueWidth = 100;
const _fieldWidth = 50;

/// How [entities] is narrowed and paged. The default is everything.
final class EntityFilter {
  /// Creates a filter.
  const EntityFilter({this.type, this.contains, this.offset = 0, this.limit});

  /// Only keys starting with `$type:`.
  final String? type;

  /// Only keys containing this substring.
  final String? contains;

  /// Matching keys to skip.
  final int offset;

  /// Stop after this many.
  final int? limit;

  bool _matches(String key) =>
      (type == null || key.startsWith('$type:')) &&
      (contains == null || key.contains(contains!));
}

List<T> _fixed<T>(Iterable<T> items) => List<T>.unmodifiable(items);

/// Copies one registry entry.
QuerySnapshot toQuery(DevQuery entry) => QuerySnapshot(
  key: entry.key,
  operation: entry.operation,
  args: bounded(tagContextJson(entry.args), _fieldWidth),
  mounts: entry.mounts,
  stale: entry.stale,
  settled: entry.settled,
  provides: _fixed(entry.provides),
  tags: _fixed([...entry.tags]..sort()),
  deps: _fixed([...entry.deps]..sort()),
  settledAt: entry.settledAt,
);

/// Every remembered query.
List<QuerySnapshot> queries(DevCache cache) =>
    _fixed([for (final entry in cache.queries()) toQuery(entry)]);

/// One query by key.
QuerySnapshot? query(DevCache cache, String key) {
  final entry = cache.query(key);

  return entry == null ? null : toQuery(entry);
}

/// The entity keys a record's fields point at, one hop, sorted.
List<String> refsOf(Object? data) {
  final found = <String>{};

  void walk(Object? node, int depth) {
    if (node == null || depth > 8) return;

    if (node is EntityRef) {
      found.add(node.key);
      return;
    }

    if (node is List<Object?>) {
      for (final element in node) {
        walk(element, depth + 1);
      }
      return;
    }

    if (node is Map<Object?, Object?>) {
      for (final value in node.values) {
        walk(value, depth + 1);
      }
    }
  }

  walk(data, 0);

  return _fixed([...found]..sort());
}

EntitySnapshot _entity(String key, DevRecord record, List<String> dependents) {
  final colon = key.indexOf(':');

  return EntitySnapshot(
    key: key,
    type: colon > 0 ? key.substring(0, colon) : key,
    id: colon > 0 ? key.substring(colon + 1) : '',
    version: record.version,
    frameAt: record.frameAt,
    fields: bounded(record.data, _fieldWidth)! as Map<String, Object?>,
    refs: refsOf(record.data),
    dependents: _fixed(dependents),
  );
}

bool _reaches(DevQuery entry, String key) =>
    entry.deps.contains(key) || entry.tags.contains(key);

/// What the cache holds for one entity, and which queries depend on it.
EntitySnapshot? entity(DevCache cache, String key) {
  final record = cache.record(key);

  if (record == null) return null;

  final dependents = [
    for (final entry in cache.queries())
      if (_reaches(entry, key)) entry.key,
  ]..sort();

  return _entity(key, record, dependents);
}

/// Which queries reached this entity.
List<QuerySnapshot> dependents(DevCache cache, String key) => _fixed([
  for (final entry in cache.queries())
    if (_reaches(entry, key)) toQuery(entry),
]);

/// Entities matching [filter], in store order. `dependents` is left empty:
/// filling it is one registry scan per record.
List<EntitySnapshot> entities(
  DevCache cache, [
  EntityFilter filter = const EntityFilter(),
]) {
  final out = <EntitySnapshot>[];
  var skipped = 0;

  for (final key in cache.entityKeys()) {
    final limit = filter.limit;

    if (limit != null && out.length >= limit) break;
    if (!filter._matches(key)) continue;

    if (skipped < filter.offset) {
      skipped++;
      continue;
    }

    final record = cache.record(key);

    if (record != null) out.add(_entity(key, record, const []));
  }

  return _fixed(out);
}

/// How many entity keys match [filter], ignoring its offset and limit.
int countEntities(
  DevCache cache, [
  EntityFilter filter = const EntityFilter(),
]) {
  var count = 0;

  for (final key in cache.entityKeys()) {
    if (filter._matches(key)) count++;
  }

  return count;
}

/// Every tag any remembered query carries, who carries it, and who is mounted.
List<TagSnapshot> tags(DevCache cache) {
  final carriers = <String, List<String>>{};

  for (final entry in cache.queries()) {
    for (final tag in entry.tags) {
      (carriers[tag] ??= []).add(entry.key);
    }
  }

  return _fixed(
    [
      for (final MapEntry(key: tag, value: keys) in carriers.entries)
        TagSnapshot(
          tag: tag,
          carriers: _fixed(keys..sort()),
          mounted: _fixed(cache.mountedKeysFor(tag)..sort()),
        ),
    ]..sort((a, b) => a.tag.compareTo(b.tag)),
  );
}

/// The counters.
StoreSnapshot store(DevCache cache) => StoreSnapshot(
  records: cache.records,
  version: cache.version,
  frameVersion: cache.frameVersion,
  tombstones: cache.tombstones,
  tracked: cache.tracked,
  remembered: cache.remembered,
  mounted: cache.mounted,
  indexedTags: cache.indexedTags,
  stampedTags: cache.stampedTags,
);

/// Everything except the entity table.
CacheSnapshot snapshot(DevCache cache) => CacheSnapshot(
  store: store(cache),
  queries: queries(cache),
  tags: tags(cache),
);

/// Every tracked record's cheap fields, in one pass.
List<RecordSnapshot> records(DevCache cache) => _fixed([
  for (final record in cache.trackedRecords())
    RecordSnapshot(
      key: record.key,
      status: record.status,
      fetching: record.fetching,
      settled: record.settled,
      inflight: record.inflight,
      restart: record.restart,
      frameRestarts: record.frameRestarts,
    ),
]);

/// One query joined across the registry and its record. A remembered query
/// whose record was reaped reports `idle`.
QueryDetail? detail(DevCache cache, String key) {
  final entry = cache.query(key);

  if (entry == null) return null;

  DevTracked? record;

  for (final candidate in cache.trackedRecords()) {
    if (candidate.key == key) {
      record = candidate;
      break;
    }
  }

  final error = record?.error?.toString();

  return QueryDetail(
    query: toQuery(entry),
    status: record?.status ?? 'idle',
    fetching: record?.fetching ?? false,
    error: error == null ? null : shortMessage(error),
    inflight: record?.inflight ?? false,
    restart: record?.restart ?? false,
    frameRestarts: record?.frameRestarts ?? 0,
    value: bounded(entry.value, _valueWidth),
  );
}

/// One record as the store holds it, no overlay folded in. A bounded copy.
Map<String, Object?>? baseRecord(DevCache cache, String key) {
  final found = cache.record(key);

  return found == null
      ? null
      : bounded(found.data, _fieldWidth)! as Map<String, Object?>;
}

/// The same record with every pending overlay folded over it. A bounded copy.
///
/// The overlay stack's fold of one key: a plain read of the store record when
/// no layer touches it, and a memoized fold for a key a layer does hold. It
/// writes no store row and touches no LRU order or frame stamp. Folding a held
/// key can run that layer's `compute` patch, and a throwing one is reported to
/// the app's error callback, once per key until the stack changes.
Map<String, Object?>? foldedRecord(DevCache cache, String key) {
  final found = cache.folded(key);

  return found == null
      ? null
      : bounded(found, _fieldWidth)! as Map<String, Object?>;
}

/// The pending optimistic writes, bottom of the stack first.
List<OverlaySnapshot> overlays(DevCache cache) => _fixed([
  for (final layer in cache.overlays())
    OverlaySnapshot(
      id: layer.id,
      patches: _fixed(layer.patches),
      tags: _fixed(layer.tags),
      created: layer.created,
      places: layer.places,
    ),
]);
