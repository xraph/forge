import 'operation.dart';
import 'tags.dart';
import 'types.dart';

/// What the caller mounts. Everything but [operation] is optional.
final class QuerySpec {
  /// Creates a spec.
  const QuerySpec({
    required this.operation,
    this.args,
    this.provides = const [],
    this.key,
  });

  /// The operation's name, e.g. `GET /orders` or a test's `orderList`.
  final String operation;

  /// The arguments this query was called with. Part of its cache key. Null
  /// means "called with no arguments", which keys as the operation alone.
  final TagContext? args;

  /// `provides`, still as templates.
  final List<String> provides;

  /// Overrides the derived cache key. Two queries sharing a key are one query.
  final String? key;
}

/// One query in the registry, mounted or merely remembered.
///
/// Treat it as read-only: the registry mutates these fields in place so the
/// tag index can hold entry objects directly.
final class QueryEntry {
  QueryEntry._({
    required this.key,
    required this.operation,
    required this.args,
    required this.provides,
    required this.tags,
    required this.settledAt,
  });

  /// The cache key.
  final String key;

  /// The operation name.
  final String operation;

  /// The arguments the query was mounted with.
  final TagContext args;

  /// `provides`, as templates.
  final List<String> provides;

  /// Everything this query provides: resolved `provides` plus its entity deps.
  Set<String> tags;

  /// The entity keys its skeleton reached.
  Set<EntityKey> deps = <EntityKey>{};

  /// How many places have this query mounted. Zero means nobody is watching.
  int mounts = 0;

  /// Known to be behind the server. Refetches now if mounted, on mount if not.
  bool stale = false;

  /// The last value this query settled with, as the caller supplied it.
  Object? value;

  /// The invalidation clock reading at the last settle.
  int settledAt;
}

/// What [QueryRegistry.settle] records. Omitted fields are left as they were.
final class SettleResult {
  /// Creates a settle result that leaves the entry's value untouched (the
  /// TypeScript "no `value` key" case).
  const SettleResult({this.deps, this.response, this.tags, this.startedAt})
    : value = null,
      hasValue = false;

  /// Creates a settle result that records [value].
  const SettleResult.withValue(
    this.value, {
    this.deps,
    this.response,
    this.tags,
    this.startedAt,
  }) : hasValue = true;

  /// The value to record when [hasValue] is true.
  final Object? value;

  /// Whether [value] should be recorded.
  final bool hasValue;

  /// `StagedWrite.deps` from the entity store.
  final Iterable<EntityKey>? deps;

  /// The response, so `provides` templates naming `{res.x}` can resolve.
  final Object? response;

  /// The already-resolved tag set, bypassing `provides` resolution. Used by
  /// hydrate, which holds no response.
  final Iterable<String>? tags;

  /// The registry clock reading when the request that produced this value was
  /// dispatched. Defaults to the reading now.
  final int? startedAt;
}

/// Undoes one mount. Idempotent.
typedef Unmount = void Function();

/// The mounted-query registry and the tag index, as one structure.
///
/// The tag index holds mounted queries only; an invalidation arriving while a
/// query is unmounted is still observed when it mounts again, by a clock: every
/// invalidation stamps the tags it touched, every settle stamps the query with
/// the reading from when its request was dispatched. A stamp is only written
/// for a tag some remembered query carries, and is deleted when the last
/// carrier is forgotten, so the stamp map stays bounded.
final class QueryRegistry {
  /// Creates a registry.
  QueryRegistry({this.onStale, this.onUnresolved});

  final Map<String, QueryEntry> _entries = <String, QueryEntry>{};
  final Map<String, Set<QueryEntry>> _index = <String, Set<QueryEntry>>{};
  final Map<String, int> _stamps = <String, int>{};
  final Map<String, int> _carriers = <String, int>{};
  int _clock = 0;

  /// A mounted query became stale. The `Invalidator` sets this to its enqueue.
  void Function(QueryEntry entry)? onStale;

  /// A `provides` template resolved to nothing.
  void Function(String template, QueryEntry entry)? onUnresolved;

  /// Every query the registry remembers, mounted or not.
  int get size => _entries.length;

  /// How many distinct queries have at least one mount.
  int get mounted => _entries.values.where((entry) => entry.mounts > 0).length;

  /// How many tags currently have at least one mounted query.
  int get indexedTags => _index.length;

  /// How many tags currently hold an invalidation stamp.
  int get stampedTags => _stamps.length;

  /// The invalidation clock's current reading. Read it when a request goes out
  /// and hand it back as [SettleResult.startedAt].
  int get stamp => _clock;

  /// The entry for [key], or null.
  QueryEntry? get(String key) => _entries[key];

  /// Every query this registry remembers. Read-only.
  Iterable<QueryEntry> all() => _entries.values;

  /// The mounted queries carrying [tag].
  List<QueryEntry> queriesFor(String tag) => [...?_index[tag]];

  /// Mounts a query and returns the undo. The same query mounted from three
  /// places is one entry with three mounts.
  Unmount mount(QuerySpec spec) {
    final key = spec.key ?? operationQueryKey(spec.operation, spec.args);
    var entry = _entries[key];

    if (entry == null) {
      final args = spec.args ?? TagContext.empty;

      entry = QueryEntry._(
        key: key,
        operation: spec.operation,
        args: args,
        provides: spec.provides,
        // Templates naming `{res.x}` resolve at settle, not here.
        tags: resolveTags(spec.provides, args).tags.toSet(),
        settledAt: _clock,
      );

      _entries[key] = entry;

      entry.tags.forEach(_acquire);
    }

    entry.mounts++;

    if (entry.mounts == 1) {
      _link(entry);

      if (entry.stale || _invalidatedSince(entry)) markStale(entry);
    }

    var released = false;

    return () {
      if (released) return;

      released = true;
      _release(key);
    };
  }

  /// Records what a query settled with and re-derives its tags: `provides`
  /// resolved against the response, unioned with the entity deps.
  ///
  /// The stamp recorded is [SettleResult.startedAt], when the request went
  /// out, so an invalidation that landed mid-flight leaves the query stale.
  void settle(String key, [SettleResult result = const SettleResult()]) {
    final entry = _entries[key];

    if (entry == null) return;

    final supplied = result.tags;
    final resolved = supplied == null
        ? resolveTags(entry.provides, entry.args, result.response)
        : ResolvedTags(tags: supplied.toList(), unresolved: const []);

    final deps = result.deps;
    if (deps != null) entry.deps = deps.toSet();
    if (result.hasValue) entry.value = result.value;

    for (final template in resolved.unresolved) {
      if (!_providedByDeps(template, entry.deps)) {
        onUnresolved?.call(template, entry);
      }
    }

    _retag(entry, {...resolved.tags, ...entry.deps});

    entry.stale = false;
    entry.settledAt = result.startedAt ?? _clock;

    if (_invalidatedSince(entry)) markStale(entry);
  }

  /// Stamps [tags] as invalidated and returns the mounted queries each hit,
  /// with the tags that matched.
  Map<QueryEntry, Set<String>> invalidated(Iterable<String> tags) {
    _clock++;

    final hits = <QueryEntry, Set<String>>{};

    for (final tag in tags) {
      if (_carriers.containsKey(tag)) _stamps[tag] = _clock;

      final bucket = _index[tag];

      if (bucket == null) continue;

      for (final entry in bucket) {
        (hits[entry] ??= <String>{}).add(tag);
      }
    }

    return hits;
  }

  /// Marks stale and, when someone is watching, reports it.
  void markStale(QueryEntry entry) {
    entry.stale = true;

    if (entry.mounts > 0) onStale?.call(entry);
  }

  /// A placement callback answered for this query, so no refetch is owed.
  void place(QueryEntry entry, Object? value) {
    entry.value = value;
    entry.stale = false;
    entry.settledAt = _clock;
  }

  /// Swaps the entity keys a placed query reaches, keeping its resolved
  /// `provides`.
  void adopt(String key, Iterable<EntityKey> deps) {
    final entry = _entries[key];

    if (entry == null) return;

    final next = deps.toSet();
    final tags = <String>{
      ...next,
      for (final tag in entry.tags)
        if (!entry.deps.contains(tag)) tag,
    };

    entry.deps = next;
    _retag(entry, tags);
  }

  /// Forgets a query entirely, mounted or not.
  bool drop(String key) {
    final entry = _entries[key];

    if (entry == null) return false;

    if (entry.mounts > 0) _unlink(entry);

    entry.tags.forEach(_discharge);

    return _entries.remove(key) != null;
  }

  /// Drops everything, including the tag stamps. The identity-change path.
  void clear() {
    _entries.clear();
    _index.clear();
    _stamps.clear();
    _carriers.clear();
  }

  void _release(String key) {
    final entry = _entries[key];

    if (entry == null || entry.mounts == 0) return;

    entry.mounts--;

    if (entry.mounts == 0) _unlink(entry);
  }

  bool _invalidatedSince(QueryEntry entry) =>
      entry.tags.any((tag) => (_stamps[tag] ?? 0) > entry.settledAt);

  void _retag(QueryEntry entry, Set<String> tags) {
    for (final tag in entry.tags) {
      if (tags.contains(tag)) continue;

      if (entry.mounts > 0) _unlinkTag(entry, tag);
      _discharge(tag);
    }

    for (final tag in tags) {
      if (entry.tags.contains(tag)) continue;

      if (entry.mounts > 0) _linkTag(entry, tag);
      _acquire(tag);
    }

    entry.tags = tags;
  }

  void _acquire(String tag) {
    _carriers[tag] = (_carriers[tag] ?? 0) + 1;
  }

  void _discharge(String tag) {
    final count = _carriers[tag];

    if (count == null) return;

    if (count > 1) {
      _carriers[tag] = count - 1;

      return;
    }

    _carriers.remove(tag);
    _stamps.remove(tag);
  }

  void _link(QueryEntry entry) {
    for (final tag in entry.tags) {
      _linkTag(entry, tag);
    }
  }

  void _unlink(QueryEntry entry) {
    for (final tag in entry.tags) {
      _unlinkTag(entry, tag);
    }
  }

  void _linkTag(QueryEntry entry, String tag) {
    (_index[tag] ??= <QueryEntry>{}).add(entry);
  }

  void _unlinkTag(QueryEntry entry, String tag) {
    final bucket = _index[tag];

    if (bucket == null) return;

    bucket.remove(entry);

    // An emptied bucket is deleted rather than left behind.
    if (bucket.isEmpty) _index.remove(tag);
  }
}

/// Whether [deps] holds a key of the entity type a tag template names.
bool _providedByDeps(String template, Set<EntityKey> deps) {
  final prefix = template.substring(0, template.indexOf(':') + 1);

  if (prefix.isEmpty) return false;

  return deps.any((dep) => dep.startsWith(prefix));
}
