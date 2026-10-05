/// Stream frames and the live runtime. Port of
/// `packages/client-core/src/live.ts`.
library;

import 'cache.dart' show QueryCache;
import 'observe.dart' show FramesCommitted;
import 'operation.dart' show TagContext;
import 'ref.dart' show entityKey, isIdentity;
import 'store.dart' show CommitOptions;
import 'stream_types.dart';
import 'tags.dart' show resolveTags;
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
/// notified or invalidated. Omit it to commit unconditionally.
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

  final report = onError ?? cache.report;
  final stamp = cache.store.nextFrame();
  final tags = <String>{};

  for (final frame in frames) {
    final binding = frame.binding;
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
      cache.store.write(
        payload,
        cache.entities,
        binding.entity,
        CommitOptions(frameAt: stamp),
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

  if (tags.isNotEmpty) cache.invalidate(tags.toList());
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
