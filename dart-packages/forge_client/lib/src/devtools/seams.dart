/// The one file in the devtools that reads `forge_client` internals.
///
/// Everything else under `lib/src/devtools/` goes through the extension types
/// and the normalised events below, so a private name changing in the cache is
/// fixed here and nowhere else. Nothing in this file copies a value, opens a
/// record or moves the LRU order: every getter is a field load.
library;

import 'package:meta/meta.dart';

import '../cache.dart';
import '../live.dart';
import '../observe.dart';
import '../operation.dart';
import '../overlay.dart';
import '../owned.dart';
import '../registry.dart';
import '../storage.dart';
import '../stream_types.dart';
import '../sync.dart';
import '../transport.dart';
import '../types.dart';

/// A registry entry, read in place.
extension type const DevQuery._(QueryEntry _entry) {
  /// The cache key.
  String get key => _entry.key;

  /// `METHOD /path`.
  String get operation => _entry.operation;

  /// The arguments the query was called with.
  TagContext get args => _entry.args;

  /// How many places have it mounted.
  int get mounts => _entry.mounts;

  /// Known to be behind the server.
  bool get stale => _entry.stale;

  /// The last settled value, by identity. Callers copy it before keeping it.
  Object? get value => _entry.value;

  /// Whether a response has ever settled into it.
  bool get settled => _entry.value != null;

  /// `provides`, still as templates.
  List<String> get provides => _entry.provides;

  /// Everything it carries: resolved templates plus entity dependencies.
  Set<String> get tags => _entry.tags;

  /// The entity keys its skeleton reached.
  Set<String> get deps => _entry.deps;

  /// The invalidation clock reading at its last settle.
  int get settledAt => _entry.settledAt;
}

/// An entity record, read in place.
extension type const DevRecord._(EntityRecord _record) {
  /// The record's client-shaped fields. Callers copy before handing out.
  Json get data => _record.data;

  /// Bumps only when the data moved.
  int get version => _record.version;

  /// The frame clock when a stream frame last wrote it, 0 if none did.
  int get frameAt => _record.frameAt ?? 0;
}

/// A tracked query record, read in place.
extension type const DevTracked._(TrackedRecord _record) {
  /// The cache key.
  String get key => _record.key;

  /// The operation it runs.
  OperationMeta get meta => _record.meta;

  /// Its arguments.
  TagContext get args => _record.args;

  /// `idle`, `pending`, `success` or `error`.
  String get status => _record.status.name;

  /// A request is in flight for it.
  bool get fetching => _record.fetching;

  /// A response has settled into it.
  bool get settled => _record.settled;

  /// A request sequence is running.
  bool get inflight => _record.inflight != null;

  /// An invalidation landed mid-flight.
  bool get restart => _record.restart;

  /// How many times a stream frame overtook its request.
  int get frameRestarts => _record.frameRestarts;

  /// The last error, if the record is failed.
  Object? get error => _record.error;
}

/// One optimistic layer, reduced to its shape. Never carries a patch's value.
final class DevOverlay {
  /// Creates a layer view.
  const DevOverlay({
    required this.id,
    required this.patches,
    required this.tags,
    required this.created,
    required this.places,
  });

  /// The layer id.
  final int id;

  /// Each patched entity and whether the patch merges, creates or deletes.
  final List<({String key, String kind})> patches;

  /// Its `invalidates`, resolved.
  final List<String> tags;

  /// The minted key, for a create.
  final String? created;

  /// Whether it declares placement callbacks.
  final bool places;
}

/// The devtools' whole view of a [QueryCache].
extension type const DevCache(QueryCache cache) {
  /// Every registry entry, mounted or not.
  Iterable<DevQuery> queries() => cache.registry.all().map(DevQuery._);

  /// One registry entry.
  DevQuery? query(String key) {
    final entry = cache.registry.get(key);
    return entry == null ? null : DevQuery._(entry);
  }

  /// The keys of the mounted queries an invalidation of [tag] reaches now.
  List<String> mountedKeysFor(String tag) => [
    for (final entry in cache.registry.queriesFor(tag)) entry.key,
  ];

  /// Registry size.
  int get remembered => cache.registry.size;

  /// Registry entries with at least one mount.
  int get mounted => cache.registry.mounted;

  /// Tags with at least one mounted query.
  int get indexedTags => cache.registry.indexedTags;

  /// Tags holding an invalidation stamp.
  int get stampedTags => cache.registry.stampedTags;

  /// Marks one entry stale without raising its tags.
  void markStale(DevQuery query) => cache.registry.markStale(query._entry);

  /// Every entity key the store holds, in insertion order.
  Iterable<String> entityKeys() => cache.store.keys;

  /// One entity record.
  DevRecord? record(String key) {
    final found = cache.store.getRecord(key);
    return found == null ? null : DevRecord._(found);
  }

  /// Whether the store holds [key].
  bool hasEntity(String key) => cache.store.has(key);

  /// Whether a sync source owns the entity [key] names, so only that source
  /// may write it. True whether or not the store holds it.
  bool ownsKey(String key) => cache.owns(typenameOf(key));

  /// Whether a sync source owns the entity type a tag names: `Note:1`,
  /// `Note[]` and `Note[]:open` all name `Note`.
  bool ownsTag(String tag) {
    final end = tag.indexOf(_tagTypeEnd);

    return cache.owns(end == -1 ? tag : tag.substring(0, end));
  }

  /// Drops one entity record.
  bool evictEntity(String key) => cache.store.evict(key);

  /// Entity records held.
  int get records => cache.store.size;

  /// Total record writes.
  int get version => cache.store.version;

  /// Committed frame batches.
  int get frameVersion => cache.store.frameVersion;

  /// Frame-evicted keys still holding a stamp.
  int get tombstones => cache.store.tombstones;

  /// Tracked query records.
  int get tracked => cache.size;

  /// Every tracked record.
  Iterable<DevTracked> trackedRecords() => cache.tracked().map(DevTracked._);

  /// The overlay stack, bottom first.
  List<DevOverlay> overlays() => [
    for (final entry in cache.overlays.list())
      DevOverlay(
        id: entry.id,
        patches: [
          for (final MapEntry(:key, :value) in entry.patches.entries)
            (key: key, kind: _patchKind(value)),
        ],
        tags: [...entry.tags],
        created: entry.created,
        places: entry.place != null,
      ),
  ];

  /// The record with every pending overlay folded over it.
  Json? folded(String key) => cache.overlays.effective(key)?.data;

  /// Pushes a hand-written merge onto the overlay stack and returns its id.
  int pushMerge(String key, Json fields, {List<String> tags = const []}) =>
      cache.overlays.add(
        {
          key: MergePatch({...fields}),
        },
        null,
        tags,
      );

  /// Pushes a delete layer and returns its id.
  int pushDelete(String key, {List<String> tags = const [], String? created}) =>
      cache.overlays.add({key: const DeletePatch()}, null, tags, created);

  /// Removes one layer. False when no layer has [id].
  bool takeOverlay(int id) => cache.overlays.take(id) != null;

  /// Commits one layer to the base store. False when no layer has [id].
  bool promoteOverlay(int id) {
    final entry = cache.overlays.take(id);
    if (entry == null) return false;
    cache.overlays.promote(entry);
    return true;
  }

  /// Tells every reader the store or the stack changed.
  void notifyChanged() => cache.notifyChanged();

  /// The private LRU order, oldest first. Read only by the non-mutation test.
  List<String> lruOrder() => cache.debugLruOrder;

  /// The sync sources the cache was built with.
  List<SyncSource> get syncSources => cache.syncSources;

  /// The per-principal storage session the cache owns, when it has one. The
  /// Outbox panel reads queued writes from it, so `forge_client` never imports
  /// `forge_client_offline`.
  StorageSession? get session => cache.session;
}

final RegExp _tagTypeEnd = RegExp(r'[:\[]');

String _patchKind(EntityPatch patch) => switch (patch) {
  MergePatch() => 'merge',
  CreatePatch() => 'create',
  DeletePatch() => 'delete',
};

/// [text] cut to 200 characters, so a failure message that carries a response
/// body cannot make a log entry or a snapshot enormous.
String shortMessage(String text) =>
    text.length > 200 ? '${text.substring(0, 200)}...' : text;

/// `synced`, `pending`, `offline` or `failed`.
String syncStatusName(SyncStatus status) => switch (status) {
  Synced() => 'synced',
  Pending() => 'pending',
  Offline() => 'offline',
  SyncFailed() => 'failed',
};

/// A cache event, normalised to the fields the devtools read.
sealed class DevEvent {
  /// Const base.
  const DevEvent();
}

/// A tracked query changed state.
final class DevQueryEvent extends DevEvent {
  /// Creates the event.
  const DevQueryEvent({
    required this.key,
    required this.status,
    required this.fetching,
  });

  /// The query key.
  final String key;

  /// `idle`, `pending`, `success` or `error`.
  final String status;

  /// Whether a request is in flight.
  final bool fetching;
}

/// A mutation settled, immediately before its tags are applied.
final class DevMutationEvent extends DevEvent {
  /// Creates the event.
  const DevMutationEvent({
    required this.meta,
    required this.args,
    required this.response,
  });

  /// The operation.
  final OperationMeta meta;

  /// Its arguments.
  final TagContext args;

  /// The live response. Read inside the synchronous call, never retain.
  final Object? response;
}

/// One decoded frame inside a [DevFramesEvent].
final class DevFrame {
  /// Creates a frame view.
  const DevFrame({
    required this.channel,
    required this.message,
    required this.intent,
    required this.entity,
    required this.payload,
  });

  /// The binding's channel.
  final String channel;

  /// The binding's message name.
  final String message;

  /// `upsert`, `patch` or `evict`.
  final String intent;

  /// The entity typename.
  final String entity;

  /// The live payload. Copy before keeping.
  final Object? payload;
}

/// A batch of stream frames committed.
final class DevFramesEvent extends DevEvent {
  /// Creates the event.
  const DevFramesEvent({
    required this.count,
    required this.tags,
    required this.frames,
  });

  /// Frames in the batch.
  final int count;

  /// The tags the batch raised.
  final List<String> tags;

  /// The frames.
  final List<DevFrame> frames;
}

/// One mounted query was hit by an invalidation.
final class DevInvalidatedEvent extends DevEvent {
  /// Creates the event.
  const DevInvalidatedEvent({required this.key, required this.matched});

  /// The query key.
  final String key;

  /// The tags that reached it.
  final List<String> matched;
}

/// A placement callback answered for a query.
final class DevPlacedEvent extends DevEvent {
  /// Creates the event.
  const DevPlacedEvent({required this.key});

  /// The query key.
  final String key;
}

/// A write entered the outbox.
final class DevOutboxEnqueued extends DevEvent {
  /// Creates the event.
  const DevOutboxEnqueued({required this.mutationId, required this.operation});

  /// The pending mutation id.
  final String mutationId;

  /// The operation's table key, e.g. `op_create_order`.
  final String operation;
}

/// A queued write replayed successfully.
final class DevOutboxReplayed extends DevEvent {
  /// Creates the event.
  const DevOutboxReplayed({required this.mutationId, required this.operation});

  /// The pending mutation id.
  final String mutationId;

  /// The operation's table key.
  final String operation;
}

/// A queued write failed on replay.
final class DevOutboxFailed extends DevEvent {
  /// Creates the event.
  const DevOutboxFailed({
    required this.mutationId,
    required this.operation,
    required this.failure,
  });

  /// The pending mutation id.
  final String mutationId;

  /// The operation's table key.
  final String operation;

  /// The failure, reduced to a message of at most 200 characters.
  final String failure;
}

/// A sync source reported a new status for one entity.
final class DevSyncStatus extends DevEvent {
  /// Creates the event.
  const DevSyncStatus({
    required this.entity,
    required this.status,
    required this.pending,
    required this.error,
  });

  /// The entity typename.
  final String entity;

  /// `synced`, `pending`, `offline` or `failed`.
  final String status;

  /// Pending change count, 0 unless [status] is `pending`.
  final int pending;

  /// The failure message, when [status] is `failed`.
  final String? error;
}

/// Maps one cache event to its devtools form.
DevEvent devEvent(CacheEvent event) => switch (event) {
  QueryTransition(:final key, :final status, :final fetching) => DevQueryEvent(
    key: key,
    status: status.name,
    fetching: fetching,
  ),
  MutationCommitted(:final meta, :final args, :final response) =>
    DevMutationEvent(meta: meta, args: args, response: response),
  FramesCommitted(:final count, :final tags, :final frames) => DevFramesEvent(
    count: count,
    tags: [...tags],
    frames: [
      for (final frame in frames)
        DevFrame(
          channel: frame.binding.channel,
          message: frame.binding.message,
          intent: frame.binding.intent.name,
          entity: frame.binding.entity,
          payload: frame.payload,
        ),
    ],
  ),
  QueryInvalidated(:final key, :final matched) => DevInvalidatedEvent(
    key: key,
    matched: [...matched],
  ),
  QueryPlaced(:final key) => DevPlacedEvent(key: key),
  OutboxEnqueued(:final mutationId, :final operationId) => DevOutboxEnqueued(
    mutationId: mutationId,
    operation: operationId,
  ),
  OutboxReplayed(:final mutationId, :final operationId) => DevOutboxReplayed(
    mutationId: mutationId,
    operation: operationId,
  ),
  OutboxFailed(:final mutationId, :final operationId, :final failure) =>
    DevOutboxFailed(
      mutationId: mutationId,
      operation: operationId,
      failure: shortMessage(failure.toString()),
    ),
  SyncStatusChanged(:final entity, :final status) => DevSyncStatus(
    entity: entity,
    status: syncStatusName(status),
    pending: status is Pending ? status.count : 0,
    error: status is SyncFailed ? shortMessage(status.error.toString()) : null,
  ),
};

/// A transport event, normalised for the request log.
sealed class DevRequestEvent {
  /// Const base.
  const DevRequestEvent({required this.id});

  /// The request id the transport assigned.
  final int id;
}

/// A request was dispatched.
final class DevRequestStarted extends DevRequestEvent {
  /// Creates the event.
  const DevRequestStarted({
    required super.id,
    required this.method,
    required this.path,
    required this.args,
    required this.limit,
  });

  /// Upper-case HTTP method.
  final String method;

  /// The path template.
  final String path;

  /// The arguments. Reduced to a truncated key before the log keeps anything.
  final TagContext args;

  /// How many attempts this method is allowed.
  final int limit;
}

/// A failed attempt is being retried.
final class DevRequestRetried extends DevRequestEvent {
  /// Creates the event.
  const DevRequestRetried({
    required super.id,
    required this.attempt,
    required this.delayMs,
    required this.status,
  });

  /// The zero-based attempt that failed.
  final int attempt;

  /// The backoff before the next attempt.
  final int delayMs;

  /// The status that failed it, when there was one.
  final int? status;
}

/// A 401 sent the request to the credential refresh.
final class DevRequestRefresh extends DevRequestEvent {
  /// Creates the event.
  const DevRequestRefresh({required super.id, required this.joined});

  /// Whether it joined a refresh another request started.
  final bool joined;
}

/// The refresh the request waited on finished.
final class DevRequestRefreshed extends DevRequestEvent {
  /// Creates the event.
  const DevRequestRefreshed({required super.id, required this.ok});

  /// Whether the refresh succeeded.
  final bool ok;
}

/// The request settled.
final class DevRequestSettled extends DevRequestEvent {
  /// Creates the event.
  const DevRequestSettled({
    required super.id,
    required this.ok,
    required this.status,
  });

  /// Whether it succeeded.
  final bool ok;

  /// The final status, when there was one.
  final int? status;
}

/// Maps one transport event to its devtools form, or null for a kind the
/// request log does not record.
DevRequestEvent? devRequestEvent(RequestEvent event) => switch (event) {
  RequestStarted(:final id, :final meta, :final args, :final limit) =>
    DevRequestStarted(
      id: id,
      method: meta.method.toUpperCase(),
      path: meta.path,
      args: args,
      limit: limit,
    ),
  // The log counts attempts from the start and the retries, which is the
  // count TS derives from `attempt`, so the event itself carries nothing new.
  RequestAttempt() => null,
  RequestRefresh(:final id, :final joined) => DevRequestRefresh(
    id: id,
    joined: joined,
  ),
  RequestRefreshed(:final id, :final ok) => DevRequestRefreshed(id: id, ok: ok),
  RequestRetried(:final id, :final attempt, :final delay, :final status) =>
    DevRequestRetried(
      id: id,
      attempt: attempt,
      delayMs: delay.inMilliseconds,
      status: status,
    ),
  RequestSettled(:final id, :final ok, :final status) => DevRequestSettled(
    id: id,
    ok: ok,
    status: status,
  ),
};

/// Applies one frame to [cache] the way the stream binder would. Tests only.
@visibleForTesting
void debugApplyFrames(
  QueryCache cache,
  EntityStreamBinding binding,
  Object? payload,
) => applyFrames(cache, [StreamFrame(binding: binding, payload: payload)]);

/// Builds the real `OutboxEnqueued` event. Tests only.
@visibleForTesting
CacheEvent debugOutboxEnqueued(String mutationId, String operationId) =>
    OutboxEnqueued(mutationId: mutationId, operationId: operationId);

/// Builds the real `OutboxReplayed` event. Tests only.
@visibleForTesting
CacheEvent debugOutboxReplayed(String mutationId, String operationId) =>
    OutboxReplayed(mutationId: mutationId, operationId: operationId);

/// Builds the real `OutboxFailed` event. Tests only.
@visibleForTesting
CacheEvent debugOutboxFailed(
  String mutationId,
  String operationId,
  Object failure,
) => OutboxFailed(
  mutationId: mutationId,
  operationId: operationId,
  failure: failure,
);

/// Builds the real `SyncStatusChanged` event. Tests only.
@visibleForTesting
CacheEvent debugSyncStatusChanged(String entity, SyncStatus status) =>
    SyncStatusChanged(entity: entity, status: status);
