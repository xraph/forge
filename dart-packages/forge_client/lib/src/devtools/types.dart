/// What the inspector reports and what the log records. Port of
/// `client-devtools/src/types.ts`.
///
/// Built from copies: the producers (`inspect.dart`, the recorder) pass fresh or
/// unmodifiable collections, so nothing aliases a live registry entry, entity
/// record or payload and a panel writing into a snapshot cannot move the cache.
/// The const constructors keep the collections they are given.
library;

import '../types.dart';
import 'tag.dart';

/// One query the registry remembers, mounted or not.
final class QuerySnapshot {
  /// Creates a snapshot.
  const QuerySnapshot({
    required this.key,
    required this.operation,
    required this.args,
    required this.mounts,
    required this.stale,
    required this.settled,
    required this.provides,
    required this.tags,
    required this.deps,
    required this.settledAt,
  });

  /// The cache key.
  final String key;

  /// `METHOD /path`.
  final String operation;

  /// A bounded JSON copy of the arguments, headers omitted.
  final Object? args;

  /// How many places have it mounted.
  final int mounts;

  /// Known to be behind the server.
  final bool stale;

  /// Whether a response has ever settled into it.
  final bool settled;

  /// `provides`, as templates.
  final List<String> provides;

  /// Everything it carries, sorted.
  final List<String> tags;

  /// The entity keys it reached, sorted.
  final List<String> deps;

  /// The invalidation clock at its last settle.
  final int settledAt;

  /// The JSON form.
  Json toJson() => {
    'key': key,
    'operation': operation,
    'args': args,
    'mounts': mounts,
    'stale': stale,
    'settled': settled,
    'provides': provides,
    'tags': tags,
    'deps': deps,
    'settledAt': settledAt,
  };
}

/// One query joined across the registry and its tracked record.
final class QueryDetail {
  /// Creates a detail.
  const QueryDetail({
    required this.query,
    required this.status,
    required this.fetching,
    required this.error,
    required this.inflight,
    required this.restart,
    required this.frameRestarts,
    required this.value,
  });

  /// The registry half.
  final QuerySnapshot query;

  /// `idle`, `pending`, `success` or `error`.
  final String status;

  /// A request is in flight.
  final bool fetching;

  /// The error reduced to a message. The object is never retained.
  final String? error;

  /// A request sequence is running.
  final bool inflight;

  /// An invalidation landed mid-flight.
  final bool restart;

  /// How many times a frame overtook its request.
  final int frameRestarts;

  /// A bounded copy of the last settled value.
  final Object? value;

  /// The cache key.
  String get key => query.key;

  /// Mount count.
  int get mounts => query.mounts;

  /// Carried tags.
  List<String> get tags => query.tags;

  /// `provides` templates.
  List<String> get provides => query.provides;

  /// Entity dependencies.
  List<String> get deps => query.deps;

  /// The JSON form.
  Json toJson() => {
    ...query.toJson(),
    'status': status,
    'fetching': fetching,
    'error': error,
    'inflight': inflight,
    'restart': restart,
    'frameRestarts': frameRestarts,
    'value': value,
  };
}

/// One tracked record's cheap fields. Sized by nothing a response controls.
final class RecordSnapshot {
  /// Creates a record snapshot.
  const RecordSnapshot({
    required this.key,
    required this.status,
    required this.fetching,
    required this.settled,
    required this.inflight,
    required this.restart,
    required this.frameRestarts,
  });

  /// The cache key.
  final String key;

  /// `idle`, `pending`, `success` or `error`.
  final String status;

  /// A request is in flight.
  final bool fetching;

  /// A response has settled.
  final bool settled;

  /// A request sequence is running.
  final bool inflight;

  /// An invalidation landed mid-flight.
  final bool restart;

  /// Frames that overtook its request.
  final int frameRestarts;

  /// The JSON form: exactly seven keys, no value and no error.
  Json toJson() => {
    'key': key,
    'status': status,
    'fetching': fetching,
    'settled': settled,
    'inflight': inflight,
    'restart': restart,
    'frameRestarts': frameRestarts,
  };
}

/// One entity record as the store holds it.
final class EntitySnapshot {
  /// Creates an entity snapshot.
  const EntitySnapshot({
    required this.key,
    required this.type,
    required this.id,
    required this.version,
    required this.frameAt,
    required this.fields,
    required this.refs,
    required this.dependents,
  });

  /// `Type:id`.
  final String key;

  /// The typename.
  final String type;

  /// The id.
  final String id;

  /// Bumps only when the data moved.
  final int version;

  /// Frame clock at the last frame write, 0 if none.
  final int frameAt;

  /// A bounded copy of the fields. References appear as `{'__ref': key}`.
  final Map<String, Object?> fields;

  /// Entity keys this record points at, one hop.
  final List<String> refs;

  /// Query keys that reached it. Filled only by `entity(key)`.
  final List<String> dependents;

  /// The JSON form.
  Json toJson() => {
    'key': key,
    'type': type,
    'id': id,
    'version': version,
    'frameAt': frameAt,
    'fields': fields,
    'refs': refs,
    'dependents': dependents,
  };
}

/// One tag and who is on either side of it.
final class TagSnapshot {
  /// Creates a tag snapshot.
  const TagSnapshot({
    required this.tag,
    required this.carriers,
    required this.mounted,
  });

  /// The tag.
  final String tag;

  /// Query keys carrying it, mounted or not.
  final List<String> carriers;

  /// Of those, the ones an invalidation reaches now.
  final List<String> mounted;

  /// The JSON form.
  Json toJson() => {'tag': tag, 'carriers': carriers, 'mounted': mounted};
}

/// The counters that say whether anything is leaking.
final class StoreSnapshot {
  /// Creates the counters.
  const StoreSnapshot({
    required this.records,
    required this.version,
    required this.frameVersion,
    required this.tombstones,
    required this.tracked,
    required this.remembered,
    required this.mounted,
    required this.indexedTags,
    required this.stampedTags,
  });

  /// Entity records held.
  final int records;

  /// Total record writes.
  final int version;

  /// Committed frame batches.
  final int frameVersion;

  /// Frame-evicted keys still stamped.
  final int tombstones;

  /// Tracked query records.
  final int tracked;

  /// Registry entries.
  final int remembered;

  /// Registry entries with a mount.
  final int mounted;

  /// Tags with a mounted query.
  final int indexedTags;

  /// Tags holding an invalidation stamp.
  final int stampedTags;

  /// The JSON form.
  Json toJson() => {
    'records': records,
    'version': version,
    'frameVersion': frameVersion,
    'tombstones': tombstones,
    'tracked': tracked,
    'remembered': remembered,
    'mounted': mounted,
    'indexedTags': indexedTags,
    'stampedTags': stampedTags,
  };
}

/// Counters, queries and the tag graph in one read.
final class CacheSnapshot {
  /// Creates the snapshot.
  const CacheSnapshot({
    required this.store,
    required this.queries,
    required this.tags,
  });

  /// The counters.
  final StoreSnapshot store;

  /// Every remembered query.
  final List<QuerySnapshot> queries;

  /// Every tag.
  final List<TagSnapshot> tags;

  /// The JSON form.
  Json toJson() => {
    'store': store.toJson(),
    'queries': [for (final q in queries) q.toJson()],
    'tags': [for (final t in tags) t.toJson()],
  };
}

/// Why a request went out.
enum FetchReason {
  /// An invalidation reached the query.
  invalidation,

  /// Nobody had asked for the query before.
  mount,

  /// A refetch with no invalidation behind it.
  manual,
}

/// What a panel action did. The names are the TS wire names.
enum ActionKind {
  /// Ran one query again.
  refetch,

  /// Raised one query's tags.
  invalidate,

  /// Raised one tag.
  invalidateTag,

  /// Dropped one entity.
  evict,

  /// Forgot one query.
  drop,

  /// Cleared the cache.
  clear,

  /// Took, promoted or pushed an overlay layer.
  rollback,

  /// Held a query in a state.
  hold,

  /// Released a held query.
  release,

  /// Marked one query stale.
  stale,
}

/// What happened to a queued write.
enum OutboxPhase {
  /// It entered the outbox.
  enqueued,

  /// It replayed successfully.
  replayed,

  /// It failed on replay.
  failed,
}

/// One recorded event. `seq` is monotonic for the life of the devtools and
/// `session` counts identity changes.
sealed class LogEntry {
  /// Creates the stamped fields.
  const LogEntry({required this.seq, required this.at, required this.session});

  /// Monotonic sequence.
  final int seq;

  /// Clock reading.
  final int at;

  /// Identity session.
  final int session;

  /// The TS `kind`.
  String get kind;

  Json _fields();

  /// The JSON form, `kind` first.
  Json toJson() => {
    'kind': kind,
    'seq': seq,
    'at': at,
    'session': session,
    ..._fields(),
  };
}

/// A mutation settled, with the tags it raised. A cause.
final class MutationLog extends LogEntry {
  /// Creates the entry.
  const MutationLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.operation,
    required this.args,
    required this.tags,
    required this.unresolved,
  });

  /// `METHOD /path`.
  final String operation;

  /// The truncated argument key. Never the response.
  final String args;

  /// `invalidates`, resolved.
  final List<String> tags;

  /// Templates that resolved to nothing.
  final List<String> unresolved;

  @override
  String get kind => 'mutation';

  @override
  Json _fields() => {
    'operation': operation,
    'args': args,
    'tags': tags,
    'unresolved': unresolved,
  };
}

/// A batch of frames committed. A cause.
final class FramesLog extends LogEntry {
  /// Creates the entry.
  const FramesLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.frames,
    required this.tags,
  });

  /// Frames in the batch.
  final int frames;

  /// Tags raised.
  final List<String> tags;

  @override
  String get kind => 'frames';

  @override
  Json _fields() => {'frames': frames, 'tags': tags};
}

/// A mounted query was reached by these tags.
final class InvalidatedLog extends LogEntry {
  /// Creates the entry.
  const InvalidatedLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.query,
    required this.matched,
    required this.cause,
  });

  /// The query key.
  final String query;

  /// The tags that reached it.
  final List<String> matched;

  /// The `seq` of the responsible cause, when known.
  final int? cause;

  @override
  String get kind => 'invalidated';

  @override
  Json _fields() => {'query': query, 'matched': matched, 'cause': cause};
}

/// A placement callback answered instead of a refetch.
final class PlacedLog extends LogEntry {
  /// Creates the entry.
  const PlacedLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.query,
    required this.cause,
  });

  /// The query key.
  final String query;

  /// The responsible cause.
  final int? cause;

  @override
  String get kind => 'placed';

  @override
  Json _fields() => {'query': query, 'cause': cause};
}

/// A request went out for a query.
final class FetchLog extends LogEntry {
  /// Creates the entry.
  const FetchLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.query,
    required this.reason,
    required this.cause,
  });

  /// The query key.
  final String query;

  /// Why.
  final FetchReason reason;

  /// The responsible cause.
  final int? cause;

  @override
  String get kind => 'fetch';

  @override
  Json _fields() => {'query': query, 'reason': reason.name, 'cause': cause};
}

/// A response settled into a query.
final class SettleLog extends LogEntry {
  /// Creates the entry.
  const SettleLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.query,
    required this.version,
  });

  /// The query key.
  final String query;

  /// The store write counter afterwards.
  final int version;

  @override
  String get kind => 'settle';

  @override
  Json _fields() => {'query': query, 'version': version};
}

/// A request failed. The message only.
final class ErrorLog extends LogEntry {
  /// Creates the entry.
  const ErrorLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.query,
    required this.message,
  });

  /// The query key.
  final String query;

  /// What failed, as a message.
  final String message;

  @override
  String get kind => 'error';

  @override
  Json _fields() => {'query': query, 'message': message};
}

/// The identity changed and the cache was dropped.
final class PrincipalLog extends LogEntry {
  /// Creates the entry.
  const PrincipalLog({
    required super.seq,
    required super.at,
    required super.session,
  });

  @override
  String get kind => 'principal';

  @override
  Json _fields() => const {};
}

/// The panel did something.
final class ActionLog extends LogEntry {
  /// Creates the entry.
  const ActionLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.action,
    required this.target,
  });

  /// What it did.
  final ActionKind action;

  /// The query key, entity key or tag it aimed at.
  final String target;

  @override
  String get kind => 'action';

  @override
  Json _fields() => {'action': action.name, 'target': target};
}

/// A queued write moved. New in Dart.
final class OutboxLog extends LogEntry {
  /// Creates the entry.
  const OutboxLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.phase,
    required this.mutationId,
    required this.operation,
    required this.failure,
  });

  /// What happened.
  final OutboxPhase phase;

  /// The pending mutation id.
  final String mutationId;

  /// `METHOD /path`, known at enqueue.
  final String? operation;

  /// The failure message, for `failed`.
  final String? failure;

  @override
  String get kind => 'outbox';

  @override
  Json _fields() => {
    'phase': phase.name,
    'mutationId': mutationId,
    'operation': operation,
    'failure': failure,
  };
}

/// A sync source reported a status change. New in Dart.
final class SyncLog extends LogEntry {
  /// Creates the entry.
  const SyncLog({
    required super.seq,
    required super.at,
    required super.session,
    required this.entity,
    required this.status,
    required this.detail,
  });

  /// The entity typename.
  final String entity;

  /// `synced`, `pending`, `offline` or `failed`.
  final String status;

  /// A pending count or a failure message.
  final String? detail;

  @override
  String get kind => 'sync';

  @override
  Json _fields() => {'entity': entity, 'status': status, 'detail': detail};
}

/// One decoded frame, captured with a bounded copy of its payload.
final class FrameCapture {
  /// Creates a capture.
  const FrameCapture({
    required this.seq,
    required this.at,
    required this.channel,
    required this.message,
    required this.intent,
    required this.entity,
    required this.payload,
  });

  /// The `seq` of the frames log entry it belongs to.
  final int seq;

  /// Clock reading.
  final int at;

  /// The binding channel.
  final String channel;

  /// The message name.
  final String message;

  /// `upsert`, `patch` or `evict`.
  final String intent;

  /// The typename.
  final String entity;

  /// Copied, capped in depth and width. Never the live payload.
  final Object? payload;

  /// The JSON form.
  Json toJson() => {
    'seq': seq,
    'at': at,
    'channel': channel,
    'message': message,
    'intent': intent,
    'entity': entity,
    'payload': payload,
  };
}

/// What raised a set of tags, reduced to what an explanation needs.
final class CauseSummary {
  /// Creates a summary.
  const CauseSummary({
    required this.label,
    required this.seq,
    required this.tags,
    required this.unresolved,
  });

  /// `mutation POST /orders`, `3 stream frames`, or a caller label.
  final String label;

  /// The log `seq` it came from.
  final int? seq;

  /// What it raised.
  final List<String> tags;

  /// Templates that resolved to nothing.
  final List<String> unresolved;

  /// The JSON form.
  Json toJson() => {
    'label': label,
    'seq': seq,
    'tags': tags,
    'unresolved': unresolved,
  };
}

/// What happened to a query when a cause went past it.
enum MissOutcome {
  /// The tags met and a request went out.
  refetched('refetched'),

  /// The tags met; a placement callback answered.
  placed('placed'),

  /// The tags met but nothing has it mounted.
  staleWhileUnmounted('stale-while-unmounted'),

  /// The tag sets are disjoint.
  missed('missed'),

  /// The cache has never heard of the key.
  notTracked('not-tracked');

  const MissOutcome(this.wire);

  /// The TS name, used on the wire.
  final String wire;
}

/// The answer to one of the two questions.
sealed class Explanation {
  /// Const base.
  const Explanation();

  /// The JSON form, with `kind` set to `miss` or `refetch`.
  Json toJson();
}

/// The answer to "why did this query not refetch".
final class MissReport extends Explanation {
  /// Creates a report.
  const MissReport({
    required this.query,
    required this.outcome,
    required this.reason,
    required this.cause,
    required this.mounts,
    required this.settled,
    required this.invalidated,
    required this.carried,
    required this.matched,
    required this.nearest,
    required this.suggestions,
  });

  /// The query key.
  final String query;

  /// Which of the five outcomes.
  final MissOutcome outcome;

  /// One sentence, read first.
  final String reason;

  /// The cause examined.
  final CauseSummary cause;

  /// Mount count.
  final int mounts;

  /// Whether it ever settled.
  final bool settled;

  /// What the cause raised.
  final List<String> invalidated;

  /// What the query carries.
  final List<String> carried;

  /// Where they meet.
  final List<String> matched;

  /// Where they nearly meet.
  final List<NearMiss> nearest;

  /// Concrete things to change.
  final List<String> suggestions;

  @override
  Json toJson() => {
    'kind': 'miss',
    'query': query,
    'outcome': outcome.wire,
    'reason': reason,
    'cause': cause.toJson(),
    'mounts': mounts,
    'settled': settled,
    'invalidated': invalidated,
    'carried': carried,
    'matched': matched,
    'nearest': [for (final miss in nearest) miss.toJson()],
    'suggestions': suggestions,
  };
}

/// The answer to "why did this query refetch".
final class RefetchReport extends Explanation {
  /// Creates a report.
  const RefetchReport({
    required this.query,
    required this.at,
    required this.reason,
    required this.cause,
    required this.matched,
    required this.summary,
  });

  /// The query key.
  final String query;

  /// When the request went out.
  final int at;

  /// Why.
  final FetchReason reason;

  /// The responsible cause.
  final CauseSummary? cause;

  /// Which of its tags the cause reached.
  final List<String> matched;

  /// One sentence.
  final String summary;

  @override
  Json toJson() => {
    'kind': 'refetch',
    'query': query,
    'at': at,
    'reason': reason.name,
    'cause': cause?.toJson(),
    'matched': matched,
    'summary': summary,
  };
}

/// One resolved tag and the mounted queries it would reach.
final class TagHit {
  /// Creates a hit.
  const TagHit({required this.tag, required this.queries});

  /// The tag.
  final String tag;

  /// Mounted query keys, sorted.
  final List<String> queries;

  /// The JSON form.
  Json toJson() => {'tag': tag, 'queries': queries};
}

/// What an operation would invalidate, asked without running it.
final class InvalidationPreview {
  /// Creates a preview.
  const InvalidationPreview({
    required this.operation,
    required this.templates,
    required this.tags,
    required this.unresolved,
    required this.hits,
    required this.missed,
  });

  /// `METHOD /path`.
  final String operation;

  /// `invalidates`, as declared.
  final List<String> templates;

  /// Those that resolved.
  final List<String> tags;

  /// Those that did not.
  final List<String> unresolved;

  /// Per tag, who it reaches.
  final List<TagHit> hits;

  /// Tags nothing carries.
  final List<String> missed;

  /// The JSON form.
  Json toJson() => {
    'operation': operation,
    'templates': templates,
    'tags': tags,
    'unresolved': unresolved,
    'hits': [for (final hit in hits) hit.toJson()],
    'missed': missed,
  };
}

/// One pending optimistic write, as a shape. Never carries what it writes.
final class OverlaySnapshot {
  /// Creates a snapshot.
  const OverlaySnapshot({
    required this.id,
    required this.patches,
    required this.tags,
    required this.created,
    required this.places,
  });

  /// The layer id.
  final int id;

  /// Each key and whether the patch merges, creates or deletes.
  final List<({String key, String kind})> patches;

  /// Its resolved `invalidates`.
  final List<String> tags;

  /// The minted key, for a create.
  final String? created;

  /// Whether it declares placement callbacks.
  final bool places;

  /// The JSON form.
  Json toJson() => {
    'id': id,
    'patches': [
      for (final p in patches) {'key': p.key, 'kind': p.kind},
    ],
    'tags': tags,
    'created': created,
    'places': places,
  };
}
