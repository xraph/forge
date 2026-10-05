/// Stream frames and the live runtime. Port of
/// `packages/client-core/src/live.ts`.
library;

import 'dart:async';
import 'dart:developer' as developer;

import 'cache.dart' show CommitScheduler, LiveBinding, QueryCache;
import 'observe.dart' show FramesCommitted;
import 'operation.dart' show OperationMeta, TagContext;
import 'ref.dart' show entityKey, isIdentity;
import 'store.dart' show CommitOptions;
import 'stream.dart' show SubscriptionManager, streamControlEvents;
import 'stream_types.dart';
import 'tags.dart' show queryKey, resolveTags;
import 'transport.dart' show Sleep, realSleep;
import 'types.dart' show EntityKey;

/// Commit a batch of stream frames: the mutation path for a write the client
/// did not initiate.
///
/// Not a second apply path. Every step goes through the same public seams a
/// mutation uses: the store, `notifyChanged`, and `invalidate`. The batch takes
/// one reading of the frame clock and stamps every record it writes with it,
/// so a response dispatched before the batch never overwrites what it wrote.
/// `upsert` and `patch` merge; `evict` drops the record, leaves a tombstone,
/// and raises `<Entity>[]` whatever the binding declared.
///
/// [generation] is the value of [QueryCache.generation] the frames were
/// received under. A batch held back by a coalescing delay may outlive a
/// `clear` or a principal change; when [generation] no longer matches, the
/// frames belong to data that was dropped and nothing is written, observed,
/// notified or invalidated. Omit it to commit unconditionally. Either way, a
/// listener that empties the cache while this commit notifies it stops the
/// invalidation: the batch's tags describe the data that listener dropped.
void applyFrames(
  QueryCache cache,
  List<StreamFrame> frames, {
  void Function(Object error, String context)? onError,
  int? generation,
}) {
  if (frames.isEmpty) return;

  // One user's data must never reach the next: a batch captured before the
  // cache was emptied is refused whole.
  if (generation != null && generation != cache.generation) return;

  final committing = cache.generation;
  final report = onError ?? cache.report;
  final stamp = cache.store.nextFrame();
  final tags = <String>{};

  for (final frame in frames) {
    final binding = frame.binding;

    // A sync source owns this entity's records; a frame must not write them.
    if (cache.owns(binding.entity)) continue;

    var payload = frame.payload;
    final codec = binding.decode;

    // Through the entity's codec, as a response is. A bare identity is not a
    // document and passes through. A codec that throws costs one frame.
    if (codec != null &&
        (payload is Map<Object?, Object?> || payload is List<Object?>)) {
      try {
        payload = codec.decode(payload);
      } on Object catch (error) {
        report(error, 'decode');

        continue;
      }
    }

    if (binding.intent == StreamIntent.evict) {
      final key = _identify(cache, binding.entity, payload);

      if (key != null) cache.store.evict(key, stamp);

      // An eviction changes the membership of every list that held the row,
      // whether or not the manifest says so.
      tags.add('${binding.entity}[]');
    } else {
      final staged = cache.store.stage(payload, cache.entities, binding.entity);

      // Nor may a frame for a plain entity write the owned records nested in
      // it.
      cache.store.commit(
        staged,
        CommitOptions(frameAt: stamp, skip: _owned(cache, staged.records.keys)),
      );
    }

    final resolved = resolveTags(
      binding.invalidates,
      TagContext(body: payload),
      payload,
    );

    tags.addAll(resolved.tags);

    for (final template in resolved.unresolved) {
      report(
        StateError(
          '[forge] stream tag $template (${binding.message}) resolved to nothing',
        ),
        'frame',
      );
    }
  }

  // Before the notification, so an observer sees the cause first.
  cache.observer?.call(
    FramesCommitted(count: frames.length, tags: tags, frames: frames),
  );

  // Before the invalidation, so a pure patch, which invalidates nothing,
  // still reaches its subscribers.
  cache.notifyChanged();

  // A listener notified above may have cleared the cache synchronously. The
  // tags would then refetch the next generation's queries for a batch that
  // belonged to the last one.
  if (cache.generation != committing) return;

  if (tags.isNotEmpty) cache.invalidate(tags.toList());
}

/// The keys among [keys] whose entity a sync source owns, or null for none.
Set<EntityKey>? _owned(QueryCache cache, Iterable<EntityKey> keys) {
  Set<EntityKey>? owned;

  for (final key in keys) {
    final colon = key.indexOf(':');

    if (cache.owns(colon == -1 ? key : key.substring(0, colon))) {
      (owned ??= <EntityKey>{}).add(key);
    }
  }

  return owned;
}

/// The key an evict payload names: a bare identity, or a record carrying the
/// type's id field. Anything else is skipped rather than guessed.
EntityKey? _identify(QueryCache cache, String type, Object? payload) {
  if (isIdentity(payload)) return entityKey(type, payload!);

  final idField = cache.entities[type]?.idField;

  if (idField == null || payload is! Map<Object?, Object?>) return null;

  final id = payload[idField];

  return isIdentity(id) ? entityKey(type, id!) : null;
}

/// A frame pulled apart into what a binding is looked up by.
final class DecodedFrame {
  /// Message [message] carrying [payload], optionally naming its [channel].
  const DecodedFrame({
    required this.message,
    required this.payload,
    this.channel,
  });

  /// The message name, e.g. `order.created`.
  final String message;

  /// What the message carried.
  final Object? payload;

  /// The channel, when the envelope names one. It overrides the channel the
  /// frame arrived on.
  final String? channel;

  @override
  String toString() =>
      'DecodedFrame($message, channel: $channel, payload: $payload)';
}

/// Pull a message apart. Returning null means "not a frame this runtime
/// should look at", which is how a keepalive or an ack is dropped quietly.
typedef FrameDecoder = DecodedFrame? Function(Object? message);

String? _usableName(Object? value) =>
    value is String && value.isNotEmpty ? value : null;

/// The default envelope reader, over the three shapes in circulation.
///
/// `event`/`data` is what SSE and `extensions/streaming` send, `type`/`payload`
/// is a plain Forge WebSocket handler, `name` is the AsyncAPI spelling. The
/// name is the first usable of `event`, `type`, `name`. A message with a name
/// and no payload field is its own payload.
DecodedFrame? decodeFrame(Object? message) {
  if (message is! Map<Object?, Object?>) return null;

  final name =
      _usableName(message['event']) ??
      _usableName(message['type']) ??
      _usableName(message['name']);

  if (name == null) return null;

  final payload = message.containsKey('payload')
      ? message['payload']
      : message.containsKey('data')
      ? message['data']
      : message;
  final channel = message['channel'];

  return DecodedFrame(
    message: name,
    payload: payload,
    channel: channel is String ? channel : null,
  );
}

/// One live query, held while its subscription is.
final class _LiveQuery {
  _LiveQuery(this.meta, this.args);

  final OperationMeta meta;
  final TagContext args;
  int refs = 1;
}

/// A reconnect waiting to hear whether the server filled its gap.
final class _Recovery {
  _Recovery(this.channels);

  final List<String> channels;

  /// The grace timer, when it is a real one this binder can cancel.
  Timer? timer;
}

/// One manager subscription this binder took, and how to give it back.
final class _Held {
  _Held(this.channel, this.release);

  final String channel;
  final void Function() release;
}

/// Binds channels to the cache: decode, match, queue, commit, and recover the
/// gap after a reconnect.
///
/// One binder per manager. The constructor claims the manager's
/// `onReconnect` and the cache's `live` slot, so it cannot be left unwired; a
/// second binder over the same manager takes both.
///
/// Build the manager with `principal: () => cache.principal`. The binder
/// repartitions it inside the cache's synchronous [QueryCache.principalChanges]
/// listener, so a principal change has moved every socket before
/// `setPrincipal` returns.
final class StreamBinder implements LiveBinding {
  /// [streams] is the generated table. [scheduler] defaults to the cache's
  /// commit scheduler. [resumeGrace] is how long to wait after a reconnect for
  /// a `forge.resumed` before recovering anyway; zero recovers at once.
  /// A custom `sleep` gives up cancelling the grace timer, so widget tests
  /// should keep the default.
  StreamBinder({
    required QueryCache cache,
    required List<StreamBinding> streams,
    required this.manager,
    CommitScheduler? scheduler,
    this._decode = decodeFrame,
    void Function(String message, String channel)? onUnknown,
    this._onError,
    this._resumeGrace = const Duration(seconds: 1),
    this._sleep = realSleep,
  }) : _cache = cache,
       _schedule = scheduler ?? cache.commitScheduler,
       _onUnknown = onUnknown ?? _warnUnknown {
    for (final binding in streams) {
      switch (binding) {
        case DuplexStreamBinding():
          (_byChannel[binding.channel] ??= []).add(binding);
        case EntityStreamBinding():
          _bindings[_slot(binding.channel, binding.message)] = binding;
          (_byChannel[binding.channel] ??= []).add(binding);

          final channels = _byEntity[binding.entity] ??= [];

          if (!channels.contains(binding.channel)) {
            channels.add(binding.channel);
          }
      }
    }

    manager.onReconnect = _reconnected;
    _cache.live = this;

    // Synchronous, because principalChanges is. The queue holds frames decoded
    // under the previous identity, and the store they were destined for has
    // just been emptied; the sockets belong to the previous identity too.
    _unwatch = _cache.principalChanges.listen((_) {
      _queue = [];
      manager.repartition();
      _checkPrincipal();
    });

    _checkPrincipal();
  }

  /// The manager this binder claimed.
  final SubscriptionManager manager;

  final QueryCache _cache;
  final CommitScheduler _schedule;
  final FrameDecoder _decode;
  final void Function(String message, String channel) _onUnknown;
  final void Function(Object error, String context)? _onError;
  final Duration _resumeGrace;
  final Sleep _sleep;
  late final StreamSubscription<String?> _unwatch;

  /// Reconnects awaiting a resume verdict, by endpoint.
  final Map<String, _Recovery> _pendingRecovery = {};
  final Map<String, EntityStreamBinding> _bindings = {};
  final Map<String, List<StreamBinding>> _byChannel = {};
  final Map<String, List<String>> _byEntity = {};
  final Map<String, List<String>> _reach = {};

  /// Every manager subscription this binder holds, so [dispose] can release
  /// them and a release can tell when an endpoint has nothing left to recover.
  final Set<_Held> _held = {};

  List<StreamFrame> _queue = [];

  /// The [QueryCache.generation] every frame in [_queue] was decoded under.
  int _queueGeneration = 0;
  bool _scheduled = false;
  bool _disposed = false;

  /// Mounted live queries, by channel and then by cache key.
  final Map<String, Map<String, _LiveQuery>> _live = {};

  /// Which channels a query's entities are pushed on: every channel binding a
  /// typename reachable from `meta.entity` through `entities[T].fields`.
  /// Derived from the manifest, never from settled deps, and memoized.
  @override
  List<String> channelsFor(OperationMeta meta) {
    final root = meta.entity;

    if (root == null) return const [];

    final memo = _reach[root];

    if (memo != null) return memo;

    final channels = <String>[];
    final seen = <String>{root};
    final pending = [root];

    // Iterative and seen-guarded: the schema is a graph, not a tree.
    while (pending.isNotEmpty) {
      final type = pending.removeLast();

      for (final channel in _byEntity[type] ?? const <String>[]) {
        if (!channels.contains(channel)) channels.add(channel);
      }

      for (final target
          in _cache.entities[type]?.fields.values ?? const <String>[]) {
        if (seen.add(target)) pending.add(target);
      }
    }

    return _reach[root] = List.unmodifiable(channels);
  }

  /// Frames decoded but not yet committed.
  int get pending => _queue.length;

  /// Make one query live on every channel bound to an entity it can reach.
  /// Ref-counted per query and per socket underneath.
  @override
  void Function() subscribe(
    OperationMeta meta, [
    TagContext args = TagContext.empty,
  ]) {
    _ensureOpen();

    final channels = channelsFor(meta);

    if (channels.isEmpty) {
      _onUnknown(
        '${meta.method} ${meta.path}',
        'live: no channel binds ${meta.entity ?? '?'}',
      );

      return () {};
    }

    final key = queryKey(meta, args);
    final releases = [
      for (final channel in channels) _hold(channel, key, meta, args),
    ];
    var released = false;

    return () {
      if (released) return;

      released = true;

      for (final release in releases) {
        release();
      }
    };
  }

  /// Subscribe to a channel without binding a query to it.
  void Function() channel(String name) {
    _ensureOpen();

    return _take(name, _accept);
  }

  /// Subscribe to a declared duplex channel and take its frames raw. Frames
  /// on it never reach the entity store.
  @override
  void Function() raw(
    String channel,
    FrameHandler handler, [
    SubscribeOptions options = const SubscribeOptions(),
  ]) {
    _ensureOpen();

    final declared = (_byChannel[channel] ?? const <StreamBinding>[]).any(
      (binding) => binding is DuplexStreamBinding,
    );

    if (!declared) {
      throw StateError(
        '[forge] $channel is not a duplex channel in the generated stream '
        'bindings',
      );
    }

    return _take(channel, handler, options);
  }

  /// Commit the queued frames now, whatever the scheduler had planned.
  void flush() {
    _scheduled = false;

    final queued = _queue;
    _queue = [];

    if (queued.isEmpty) return;

    // The generation the frames were decoded under, not the current one: a
    // clear or a principal change since then means the store they were
    // destined for is gone, and applyFrames refuses the batch whole.
    try {
      applyFrames(
        _cache,
        queued,
        onError: _onError,
        generation: _queueGeneration,
      );
    } on Object catch (error) {
      _onError?.call(error, 'frames');
    }
  }

  /// Release everything this binder holds: its manager subscriptions, its
  /// grace timers, its queued frames and its watch on the cache's identity.
  /// Gives up the cache's live slot and the manager's `onReconnect` if they
  /// are still this binder's.
  ///
  /// TypeScript leaves the subscriptions to their holders. Here a holder's
  /// release after dispose is a no-op, and a disposed binder throws on reuse.
  void dispose() {
    if (_disposed) return;

    _disposed = true;
    unawaited(_unwatch.cancel());
    _queue = [];
    _scheduled = false;

    for (final recovery in _pendingRecovery.values) {
      recovery.timer?.cancel();
    }

    _pendingRecovery.clear();

    final held = [..._held];
    _held.clear();
    _live.clear();

    for (final subscription in held) {
      subscription.release();
    }

    if (manager.onReconnect == _reconnected) manager.onReconnect = null;
    if (identical(_cache.live, this)) _cache.live = null;
  }

  /// A socket that had dropped is back: invalidate the channels' list tags
  /// and declared tags, then refetch every live query on them, once each.
  ///
  /// Both halves are needed. The tags catch membership that moved while the
  /// socket was down; the refetch catches a channel that only patches and so
  /// declares nothing to invalidate. The invalidation runs first, so a query
  /// refetched below is already stale and the batch does not ask twice.
  void recover(List<String> channels) {
    final tags = <String>{};

    for (final channel in channels) {
      for (final binding in _byChannel[channel] ?? const <StreamBinding>[]) {
        if (binding is! EntityStreamBinding) continue;

        tags.add('${binding.entity}[]');

        // A template naming a request argument cannot resolve here; the list
        // tag above already covers the entity.
        tags.addAll(resolveTags(binding.invalidates, TagContext.empty).tags);
      }
    }

    if (tags.isNotEmpty) _cache.invalidate(tags.toList());

    final seen = <String>{};

    for (final channel in channels) {
      final queries = _live[channel];

      if (queries == null) continue;

      // Copied: a refetch can notify a listener that releases a live query.
      for (final MapEntry(:key, value: query) in queries.entries.toList()) {
        if (!seen.add(key)) continue;

        unawaited(
          Future<Object?>.sync(() => _cache.refetch(query.meta, query.args))
              .catchError((Object error) {
                _onError?.call(error, 'recover');

                return null;
              }),
        );
      }
    }
  }

  /// Report a manager whose principal source disagrees with the cache: its
  /// sockets would be opened, and repartitioned, for the wrong identity.
  void _checkPrincipal() {
    final opens = manager.principal;
    final owns = _cache.principal;

    if (opens == owns) return;

    (_onError ?? _cache.report)(
      StateError(
        '[forge] the subscription manager opens sockets for $opens but the '
        'cache belongs to $owns; build it with principal: () => '
        'cache.principal',
      ),
      'principal',
    );
  }

  void _ensureOpen() {
    if (_disposed) throw StateError('[forge] this StreamBinder was disposed');
  }

  /// Take one manager subscription and hand back its release, which also
  /// drops a pending recovery its endpoint no longer needs.
  void Function() _take(
    String channel,
    FrameHandler handler, [
    SubscribeOptions options = const SubscribeOptions(),
  ]) {
    final held = _Held(channel, manager.subscribe(channel, handler, options));

    _held.add(held);

    return () {
      if (!_held.remove(held)) return;

      held.release();
      _forgetIdle(manager.endpointFor(channel));
    };
  }

  /// Settle, now and as unfilled, the pending recovery of an endpoint this
  /// binder no longer holds anything on. Its timer goes with it, but the
  /// recovery itself still runs: nothing confirmed the gap was filled, and
  /// cancelling is allowed only on a well-formed `forge.resumed`.
  void _forgetIdle(String endpoint) {
    if (!_pendingRecovery.containsKey(endpoint)) return;

    for (final held in _held) {
      if (manager.endpointFor(held.channel) == endpoint) return;
    }

    _settleRecovery(endpoint, filled: false);
  }

  void Function() _hold(
    String channel,
    String key,
    OperationMeta meta,
    TagContext args,
  ) {
    final queries = _live[channel] ??= {};
    final existing = queries[key];

    if (existing == null) {
      queries[key] = _LiveQuery(meta, args);
    } else {
      existing.refs++;
    }

    final release = _take(channel, _accept);

    return () {
      final held = _live[channel]?[key];

      if (held != null) {
        held.refs--;

        if (held.refs == 0) {
          _live[channel]?.remove(key);

          if (_live[channel]?.isEmpty ?? false) _live.remove(channel);
        }
      }

      release();
    };
  }

  void _reconnected(String endpoint, List<String> channels) {
    if (_disposed) return;

    // A newer reconnect of the same endpoint supersedes the older one; its
    // channels are every channel the socket carries now.
    _pendingRecovery.remove(endpoint)?.timer?.cancel();

    if (_resumeGrace == Duration.zero) {
      recover(channels);

      return;
    }

    final recovery = _pendingRecovery[endpoint] = _Recovery(channels);

    void expire() {
      // Only the reconnect that started this wait may settle it.
      if (identical(_pendingRecovery[endpoint], recovery)) {
        _settleRecovery(endpoint, filled: false);
      }
    }

    // The default timer is a real Timer, so a resume, a release or dispose
    // can cancel it rather than leave it pending.
    if (identical(_sleep, realSleep)) {
      recovery.timer = Timer(_resumeGrace, expire);

      return;
    }

    // A rejected or throwing sleep settles as unfilled, the same answer the
    // timer would have given.
    unawaited(
      Future<void>.sync(() => _sleep(_resumeGrace))
          .then<void>((_) => expire(), onError: (Object _) => expire()),
    );
  }

  void _settleRecovery(String endpoint, {required bool filled}) {
    final recovery = _pendingRecovery.remove(endpoint);

    if (recovery == null) return;

    recovery.timer?.cancel();

    if (!filled) recover(recovery.channels);
  }

  void _accept(Object? message, String arrived) {
    if (_disposed) return;

    final DecodedFrame? decoded;

    try {
      decoded = _decode(message);
    } on Object catch (error) {
      _onError?.call(error, 'decode');

      return;
    }

    if (decoded == null) return;

    if (streamControlEvents.contains(decoded.message)) {
      // Settled against the endpoint this frame's socket serves, never a
      // channel the payload names. A forge.gap always recovers; a
      // forge.resumed cancels only with a payload well-formed enough to trust.
      final endpoint = manager.endpointFor(arrived);
      final filled =
          decoded.message == 'forge.resumed' &&
          _isResumedPayload(decoded.payload);

      _settleRecovery(endpoint, filled: filled);

      return;
    }

    final channel = decoded.channel ?? arrived;
    final binding = _bindings[_slot(channel, decoded.message)];

    if (binding == null) {
      // The server is ahead of this client's manifest, which is normal.
      _onUnknown(decoded.message, channel);

      return;
    }

    // One generation per batch. Anything already queued under an older one
    // was decoded for data the cache has since dropped.
    final generation = _cache.generation;

    if (generation != _queueGeneration) {
      _queue = [];
      _queueGeneration = generation;
    }

    _queue.add(StreamFrame(binding: binding, payload: decoded.payload));

    if (_scheduled) return;

    _scheduled = true;
    _schedule.schedule(() {
      if (_scheduled) flush();
    });
  }
}

/// The key a `(channel, message)` pair is looked up under. Length-prefixed so
/// no separator character can collide.
String _slot(String channel, String message) =>
    '${channel.length}:$channel$message';

/// Whether a `forge.resumed` payload is well-formed enough to cancel a
/// recovery: the server's `ResumedPayload{from string, count int}`.
bool _isResumedPayload(Object? payload) =>
    payload is Map<Object?, Object?> &&
    payload['from'] is String &&
    payload['count'] is num;

final Set<String> _warned = {};

const int _warnLimit = 32;

/// The default `onUnknown`: one log line per `(channel, message)`, in debug
/// builds only, capped so a server's vocabulary cannot grow it without bound.
void _warnUnknown(String message, String channel) {
  assert(() {
    final slug = _slot(channel, message);

    if (_warned.contains(slug)) return true;

    if (_warned.length >= _warnLimit) {
      if (_warned.length == _warnLimit) {
        _warned.add('');
        developer.log(
          '[forge] more than $_warnLimit unbound stream message types; '
          'further warnings suppressed',
          name: 'forge',
        );
      }

      return true;
    }

    _warned.add(slug);
    developer.log(
      '[forge] no stream binding for $message on $channel; the frame was '
      'ignored',
      name: 'forge',
    );

    return true;
  }());
}

/// The manifest bindings carried on one channel.
typedef ChannelBindings = ({String channel, List<StreamBinding> bindings});

/// One live query the binder holds, the channel it rides, and its ref count.
typedef LiveQuerySnapshot = ({
  String channel,
  String key,
  String operation,
  int refs,
});

/// The binder, copied out for an inspector.
final class BinderSnapshot {
  /// Copies the binder's state.
  const BinderSnapshot({
    required this.channels,
    required this.live,
    required this.queued,
    required this.recovering,
  });

  /// Every channel in the manifest and its bindings.
  final List<ChannelBindings> channels;

  /// Every mounted live query.
  final List<LiveQuerySnapshot> live;

  /// Frames decoded and waiting for the next commit.
  final int queued;

  /// Endpoints inside the gap window after a reconnect.
  final List<String> recovering;
}

/// A copy of what the binder knows. Opens and changes nothing.
BinderSnapshot binderSnapshot(StreamBinder binder) => BinderSnapshot(
  channels: [
    for (final MapEntry(key: channel, value: bindings)
        in binder._byChannel.entries)
      (channel: channel, bindings: [...bindings]),
  ],
  live: [
    for (final MapEntry(key: channel, value: queries) in binder._live.entries)
      for (final MapEntry(:key, value: entry) in queries.entries)
        (
          channel: channel,
          key: key,
          operation: '${entry.meta.method} ${entry.meta.path}',
          refs: entry.refs,
        ),
  ],
  queued: binder._queue.length,
  recovering: [...binder._pendingRecovery.keys],
);
