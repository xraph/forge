import 'codec.dart';

/// What a frame does to its entity.
enum StreamIntent {
  /// Normalize and merge; membership may change.
  upsert,

  /// Normalize and merge; membership does not change.
  patch,

  /// Drop the record and leave a tombstone.
  evict,
}

/// One row of the generated `streams` table.
sealed class StreamBinding {
  /// Const base constructor.
  const StreamBinding({required this.channel});

  /// The endpoint path the channel is served on, e.g. `/ws/orders`.
  final String channel;
}

/// A message that carries an entity.
final class EntityStreamBinding extends StreamBinding {
  /// Creates the binding.
  const EntityStreamBinding({
    required super.channel,
    required this.message,
    required this.entity,
    required this.intent,
    this.invalidates = const [],
    this.decode,
  });

  /// The message name, e.g. `order.created`.
  final String message;

  /// The typename the payload carries.
  final String entity;

  /// What the frame does.
  final StreamIntent intent;

  /// Tag templates the message invalidates, unresolved.
  final List<String> invalidates;

  /// Turns the wire payload into the client shape. Null means the wire shape
  /// is the client shape.
  final WireCodec? decode;
}

/// A channel the application speaks on directly.
final class DuplexStreamBinding extends StreamBinding {
  /// Creates the binding.
  const DuplexStreamBinding({
    required super.channel,
    required this.send,
    required this.receive,
  });

  /// The message name the client sends.
  final String send;

  /// The message name the client receives.
  final String receive;
}

/// One stream frame, matched to its manifest binding.
final class StreamFrame {
  /// Creates a frame.
  const StreamFrame({required this.binding, required this.payload});

  /// The binding the frame matched.
  final EntityStreamBinding binding;

  /// The decoded payload: the entity, or its identity for an evict.
  final Object? payload;
}

/// Receives one raw message and the channel it arrived on.
typedef FrameHandler = void Function(Object? message, String channel);

/// Frames a subscription sends on its own behalf. A value that is an
/// `Object? Function()` is called each time it is sent.
final class SubscribeOptions {
  /// Creates the options.
  const SubscribeOptions({this.hello, this.goodbye});

  /// Sent once the socket is open, and again after every reconnect.
  final Object? hello;

  /// Sent on release, only if the socket is connected at that moment.
  final Object? goodbye;
}
