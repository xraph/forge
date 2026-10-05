/// The envelope reader for the Forge streaming extension. Port of
/// `packages/client-core/src/streaming.ts`.
library;

import 'live.dart' show DecodedFrame, FrameDecoder;

/// The transport kinds `extensions/streaming` reserves in its `type` field,
/// copied from the `MessageType*` constants in
/// `extensions/streaming/internal/streaming.go`. `streaming_kinds_test.dart`
/// reads that file and fails if the two drift.
const Set<String> forgeTransportKinds = {
  'message',
  'presence',
  'typing',
  'system',
  'join',
  'leave',
  'error',
};

/// The decoder for the streaming extension's envelope.
///
/// The name is `event` when usable; otherwise `type`, unless `type` is one of
/// [forgeTransportKinds], in which case the frame is a transport frame and is
/// dropped without being reported. The payload is `payload`, then `data`, then
/// the envelope itself. A literal `channel` is always surfaced. `channel_id` is
/// a logical id, so it is surfaced only through [channelOf], and a recognised
/// id wins over a literal `channel`; an unrecognised or empty one falls through
/// to the literal and then to the arrival channel.
///
/// The envelope's own `id` is never read, so an empty or absent one (the
/// connections stamp `''` before the server has assigned one) changes nothing.
FrameDecoder forgeStreamingDecoder({
  String? Function(String channelId)? channelOf,
}) => (message) {
  if (message is! Map<Object?, Object?>) return null;

  final event = message['event'];
  final String name;

  if (event is String && event.isNotEmpty) {
    name = event;
  } else {
    final kind = message['type'];

    if (kind is! String || kind.isEmpty) return null;

    // A transport frame carries no domain event, so no binding could claim it.
    if (forgeTransportKinds.contains(kind)) return null;

    name = kind;
  }

  final payload = message.containsKey('payload')
      ? message['payload']
      : message.containsKey('data')
      ? message['data']
      : message;

  final stated = message['channel'];
  final path = stated is String && stated.isNotEmpty ? stated : null;

  if (channelOf == null) {
    return DecodedFrame(message: name, payload: payload, channel: path);
  }

  final id = message['channel_id'];
  final mapped = id is String && id.isNotEmpty ? channelOf(id) : null;
  final channel = mapped != null && mapped.isNotEmpty ? mapped : path;

  return DecodedFrame(message: name, payload: payload, channel: channel);
};
