/// The server-sent events wire format, parsed in Dart for platforms with no
/// `EventSource`. Follows the WHATWG HTML "event stream interpretation".
library;

import 'dart:async';

/// One dispatched event.
final class SseEvent {
  /// An event named [event] carrying [data].
  const SseEvent({required this.event, required this.data, this.lastEventId});

  /// The event type; `message` when the stream named none.
  final String event;

  /// The data lines, joined with a newline.
  final String data;

  /// The last event id the stream set at or before this event, or null.
  final String? lastEventId;

  @override
  bool operator ==(Object other) =>
      other is SseEvent &&
      other.event == event &&
      other.data == data &&
      other.lastEventId == lastEventId;

  @override
  int get hashCode => Object.hash(event, data, lastEventId);

  @override
  String toString() => 'SseEvent($event, $data, id: $lastEventId)';
}

/// Turns decoded text chunks into [SseEvent]s.
///
/// Lines end in LF, CR or CRLF, and a CRLF may be split across chunks. An
/// event is dispatched on a blank line; one the stream ends inside is dropped,
/// as the spec requires, so a connection that dies mid-event never delivers
/// half a payload. `retry` is ignored: the subscription manager owns backoff.
///
/// Built on a controller rather than `async*` so that cancelling the output
/// cancels the source at once. A cancelled `async*` generator waits for its
/// next input event before it finishes, which would leave a quiet SSE
/// connection open until the server happened to write again.
final class SseParser extends StreamTransformerBase<String, SseEvent> {
  /// The parser holds no state between streams.
  const SseParser();

  @override
  Stream<SseEvent> bind(Stream<String> stream) {
    final machine = _Machine();
    late final StreamController<SseEvent> controller;
    StreamSubscription<String>? source;

    controller = StreamController<SseEvent>(
      onListen: () {
        source = stream.listen(
          (chunk) => machine.feed(chunk).forEach(controller.add),
          onError: controller.addError,
          onDone: () => unawaited(controller.close()),
        );
      },
      onPause: () => source?.pause(),
      onResume: () => source?.resume(),
      onCancel: () => source?.cancel(),
    );

    return controller.stream;
  }
}

/// The parser's state between chunks of one stream.
final class _Machine {
  final StringBuffer _line = StringBuffer();
  final StringBuffer _data = StringBuffer();
  String _type = '';
  bool _hasData = false;
  String? _lastEventId;
  bool _skipLineFeed = false;
  bool _first = true;

  /// The events [chunk] completes, in order.
  List<SseEvent> feed(String chunk) {
    final events = <SseEvent>[];

    for (var i = 0; i < chunk.length; i++) {
      final unit = chunk.codeUnitAt(i);

      if (_first) {
        _first = false;

        if (unit == 0xFEFF) continue;
      }

      if (_skipLineFeed) {
        _skipLineFeed = false;

        if (unit == 0x0A) continue;
      }

      if (unit != 0x0A && unit != 0x0D) {
        _line.writeCharCode(unit);

        continue;
      }

      if (unit == 0x0D) _skipLineFeed = true;

      final text = _line.toString();
      _line.clear();

      if (text.isEmpty) {
        if (_hasData) {
          events.add(
            SseEvent(
              event: _type.isEmpty ? 'message' : _type,
              data: _data.toString(),
              lastEventId: _lastEventId,
            ),
          );
        }

        _type = '';
        _data.clear();
        _hasData = false;

        continue;
      }

      if (text.startsWith(':')) continue;

      final colon = text.indexOf(':');
      final field = colon == -1 ? text : text.substring(0, colon);
      var value = colon == -1 ? '' : text.substring(colon + 1);

      if (value.startsWith(' ')) value = value.substring(1);

      switch (field) {
        case 'event':
          _type = value;
        case 'data':
          if (_hasData) _data.write('\n');
          _data.write(value);
          _hasData = true;
        case 'id':
          if (!value.contains('\u0000')) _lastEventId = value;
      }
    }

    return events;
  }
}
