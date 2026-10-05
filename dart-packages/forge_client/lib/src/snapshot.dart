/// Snapshots: the cache serialized as the TS `DehydratedState` JSON, and read
/// back. Port of `packages/client-core/src/ssr.ts`, which in Dart backs
/// persistence rather than server rendering.
///
/// The record set is built by a reachability walk from the exported queries,
/// so an entity no exported query references cannot appear in a snapshot.
/// Both directions assert the principal. Optimistic overlays are never
/// written: both reads go through the entity plane.
///
/// The text is the text TS writes. Both modes copy through the wire encoder,
/// so key order, integral doubles, `-0` (written as `0`) and non-finite
/// numbers (written as `null`) come out as `JSON.stringify` writes them. A
/// dehydrated `-0` arrives as `0`, as it does in TS.
library;

import 'dart:convert';

import 'cache.dart' show CachedQuery, QueryCache, RestoreInput;
import 'operation.dart' show OperationMeta, TagContext;
import 'store.dart' show CommitOptions;
import 'tags.dart' show operationName;
import 'types.dart' show EntityKey, Json;
import 'wire.dart' show EncodeContext, encode, encodePlain, revive;

/// How queries are written.
enum SnapshotMode {
  /// Shared entities once, under `records`, with `__ref` skeletons. The
  /// smallest form, and the only one that can hold an entity cycle.
  normalized,

  /// Each query's rehydrated value, duplicating shared entities.
  denormalized,
}

/// A serialized cache, the same JSON document TS `dehydrate` produces.
final class Snapshot {
  /// Wraps an already-decoded payload.
  const Snapshot(this.json);

  /// The payload: `{v, mode, principal?, records?, queries}`.
  final Json json;

  /// The payload as JSON text.
  String encode() => jsonEncode(json);

  /// Reads JSON text written by [encode] or by TS `JSON.stringify`.
  static Snapshot decode(String source) {
    final value = jsonDecode(source);

    if (value is! Map<String, Object?>) {
      throw const FormatException(
        '[forge] snapshot: the payload is not a JSON object',
      );
    }

    return Snapshot(value);
  }
}

/// Why [hydrate] refused a payload: `principal` (it belongs to someone else,
/// or the principal changed while it was being read), `version` (written by
/// code this client does not know, or malformed), or `operation` (it names an
/// operation absent from `operations`).
final class HydrationFailure implements Exception {
  /// A refusal for [reason], explained by [message].
  const HydrationFailure(this.reason, this.message);

  /// `principal`, `version` or `operation`.
  final String reason;

  /// What went wrong, for a log.
  final String message;

  @override
  String toString() => '[forge] hydrate: $message';
}

/// Serialize [cache] for [principal], which must be the cache's principal.
///
/// [queries] names the cache keys to export; every settled query when null.
/// A named key the cache does not hold throws rather than exporting nothing.
Snapshot dehydrate(
  QueryCache cache, {
  List<String>? queries,
  SnapshotMode mode = SnapshotMode.normalized,
  String? principal,
}) {
  if (principal != cache.principal) {
    throw StateError(
      '[forge] dehydrate: principal does not match the cache owner; '
      "this cache holds another identity's data",
    );
  }

  final exported = _select(cache, queries);

  return switch (mode) {
    SnapshotMode.normalized => _normalized(cache, exported, principal),
    SnapshotMode.denormalized => _denormalized(cache, exported, principal),
  };
}

/// Read [snapshot] into [cache].
///
/// [principal] must equal the cache's principal, and so must the payload's.
/// [operations] is the generated table; queries are matched to it by
/// `'<METHOD> <path>'`, the name TS writes. With [stale] every restored query
/// settles behind the server, so a mount refetches.
///
/// Every refusal the payload itself can cause (version, principal, shape,
/// operation) happens before anything is written. A principal change while
/// hydrating stops the writes at once and throws, so nothing read for one
/// principal lands in another's cache. An entity a stream frame deleted is
/// not put back: a reference to it reads as a hole.
void hydrate(
  QueryCache cache,
  Snapshot snapshot, {
  String? principal,
  Map<String, OperationMeta> operations = const {},
  bool stale = false,
}) {
  final json = snapshot.json;
  final version = json['v'];

  if (version != 1) {
    throw HydrationFailure('version', 'unsupported payload version $version');
  }

  final owner = json['principal'];

  // Compared by value. An absent payload principal reads as null, so it
  // matches only an anonymous cache, and a numeric TS principal never equals
  // a Dart one, which is always a string.
  if (owner != cache.principal || principal != cache.principal) {
    throw const HydrationFailure(
      'principal',
      'this payload belongs to a different principal; set the principal '
          'before hydrating, and never hydrate a payload built for someone else',
    );
  }

  final index = {
    for (final meta in operations.values) operationName(meta): meta,
  };
  final queries = switch (json['queries']) {
    final List<Object?> list => [for (final entry in list) _entry(entry)],
    _ => throw const HydrationFailure('version', 'malformed payload queries'),
  };
  final mode = json['mode'];

  if (mode != 'normalized' && mode != 'denormalized') {
    throw HydrationFailure('version', 'unrecognised payload mode $mode');
  }

  final normalized = mode == 'normalized';
  final records = normalized ? _records(json['records']) : null;

  // Every query resolved and parsed before the first write.
  final planned = [
    for (final query in queries)
      (
        meta:
            index['${query['operation']}'] ??
            (throw HydrationFailure(
              'operation',
              'no operation named ${query['operation']}',
            )),
        args: _tagContext(query['args']),
        skeleton: normalized ? revive(query['skeleton']) : null,
        tags: normalized
            ? [for (final tag in _list(query['tags'])) '$tag']
            : null,
        value: query['value'],
        settledTime: _settledTime(query['settledTime']),
      ),
  ];

  final generation = cache.generation;

  // Checked before every write that follows a notification: a listener that
  // signs someone else in mid-hydrate clears the cache, and the rest of this
  // payload belongs to the principal that was dropped.
  void ensureSameLife() {
    if (cache.generation != generation) {
      throw const HydrationFailure(
        'principal',
        'the principal changed while hydrating; nothing further was written',
      );
    }
  }

  bool buried(EntityKey key) =>
      !cache.store.has(key) && cache.store.frameStamp(key) > 0;

  if (records != null) {
    // Records before skeletons: a skeleton restored before the entity it
    // references would settle with a hole.
    for (final MapEntry(:key, value: data) in records.entries) {
      if (!buried(key)) cache.store.put(key, data);
    }

    for (final query in planned) {
      ensureSameLife();
      cache.restore(
        RestoreInput(
          meta: query.meta,
          args: query.args,
          skeleton: query.skeleton,
          tags: query.tags,
          settledTime: query.settledTime,
          stale: stale,
        ),
      );
    }

    return;
  }

  for (final query in planned) {
    final meta = query.meta;

    ensureSameLife();

    final staged = cache.store.stage(
      query.value,
      cache.entities,
      meta.rootType ?? meta.entity,
    );
    final skip = staged.records.keys.where(buried).toSet();

    cache.store.commit(staged, CommitOptions(skip: skip.isEmpty ? null : skip));
    cache.restore(
      RestoreInput(
        meta: meta,
        args: query.args,
        skeleton: staged.skeleton,
        response: query.value,
        settledTime: query.settledTime,
        stale: stale,
      ),
    );
  }
}

List<CachedQuery> _select(QueryCache cache, List<String>? include) {
  final settled = cache.queries.toList();

  if (include == null) return settled;

  final byKey = {for (final query in settled) query.key: query};

  return [
    for (final key in include)
      byKey[key] ??
          (throw StateError('[forge] dehydrate: no settled query for $key')),
  ];
}

Snapshot _normalized(
  QueryCache cache,
  List<CachedQuery> exported,
  String? principal,
) {
  final queries = <Json>[];
  final records = <String, Object?>{};
  final seen = <EntityKey>{};
  final pending = <(EntityKey, String)>[];

  void enqueue(List<EntityKey> keys, String from) {
    for (final key in keys) {
      if (seen.add(key)) pending.add((key, from));
    }
  }

  for (final query in exported) {
    final context = EncodeContext(query: query.key);
    final encoded = encode(query.skeleton, context);

    enqueue(encoded.refs, query.key);

    queries.add({
      'operation': operationName(query.meta),
      'args': ?_argsJson(query.args, context),
      'skeleton': encoded.value,
      'tags': [...?cache.registry.get(query.key)?.tags],
      'settledTime': query.settledTime,
    });
  }

  while (pending.isNotEmpty) {
    final (key, from) = pending.removeLast();
    final record = cache.store.getRecord(key);

    // Evicted between the fetch and now: it rehydrates to a hole, as it would
    // have here.
    if (record == null) continue;

    final encoded = encode(
      record.data,
      EncodeContext(query: from, entity: key),
    );

    records[key] = encoded.value;
    enqueue(encoded.refs, from);
  }

  return Snapshot({
    'v': 1,
    'mode': 'normalized',
    'principal': ?principal,
    'records': records,
    'queries': queries,
  });
}

Snapshot _denormalized(
  QueryCache cache,
  List<CachedQuery> exported,
  String? principal,
) {
  final queries = <Json>[];

  for (final query in exported) {
    final context = EncodeContext(query: query.key);

    // The response as the store now holds it, merges included. Copied as
    // `JSON.stringify` would write it, which also refuses an entity cycle.
    final value = encodePlain(cache.store.read(query.skeleton), context);

    queries.add({
      'operation': operationName(query.meta),
      'args': ?_argsJson(query.args, context),
      'value': value,
      'settledTime': query.settledTime,
    });
  }

  return Snapshot({
    'v': 1,
    'mode': 'denormalized',
    'principal': ?principal,
    'queries': queries,
  });
}

/// The arguments as TS writes them, or null when there are none, so a query
/// with no arguments (01a reports those as `TagContext.empty`) carries no
/// `args` key.
Object? _argsJson(TagContext args, EncodeContext context) {
  final json = <String, Object?>{
    if (args.path.isNotEmpty) 'path': args.path,
    if (args.query.isNotEmpty) 'query': args.query,
    if (args.headers.isNotEmpty) 'headers': args.headers,
    if (args.body != null) 'body': args.body,
  };

  return json.isEmpty ? null : encodePlain(json, context);
}

TagContext _tagContext(Object? json) {
  if (json is! Map<Object?, Object?>) return TagContext.empty;

  return TagContext(
    path: _stringKeyed(json['path']),
    query: _stringKeyed(json['query']),
    headers: {
      for (final MapEntry(:key, :value) in _stringKeyed(
        json['headers'],
      ).entries)
        key: '$value',
    },
    body: json['body'],
  );
}

Map<String, Object?> _stringKeyed(Object? source) =>
    source is Map<Object?, Object?>
    ? {for (final MapEntry(:key, :value) in source.entries) '$key': value}
    : const {};

/// Every record, revived, or a refusal when the payload is not shaped like
/// one. Revived up front so a malformed record refuses before any write.
Map<EntityKey, Json> _records(Object? source) {
  if (source is! Map<Object?, Object?>) {
    throw const HydrationFailure('version', 'malformed payload records');
  }

  return {
    for (final MapEntry(:key, :value) in source.entries)
      '$key': switch (revive(value)) {
        final Json data => data,
        final Map<Object?, Object?> data => _stringKeyed(data),
        _ => throw HydrationFailure('version', 'malformed record $key'),
      },
  };
}

List<Object?> _list(Object? source) => switch (source) {
  null => const [],
  final List<Object?> list => list,
  _ => throw const HydrationFailure('version', 'malformed query tags'),
};

int? _settledTime(Object? source) => switch (source) {
  null => null,
  final num time => time.toInt(),
  _ => throw const HydrationFailure('version', 'malformed settle time'),
};

Map<Object?, Object?> _entry(Object? entry) => entry is Map<Object?, Object?>
    ? entry
    : throw const HydrationFailure('version', 'malformed query entry');
