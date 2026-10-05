import 'package:forge_client/src/sse.dart';
import 'package:test/test.dart';

Future<List<SseEvent>> _parse(List<String> chunks) =>
    Stream.fromIterable(chunks).transform(const SseParser()).toList();

/// Parse [chunks] and collect the ids the parser committed, with the events.
Future<({List<SseEvent> events, List<String> ids})> _parseWithIds(
  List<String> chunks,
) async {
  final ids = <String>[];
  final events = await Stream.fromIterable(chunks)
      .transform(SseParser(onLastEventId: ids.add))
      .toList();

  return (events: events, ids: ids);
}

void main() {
  test('dispatches an event on a blank line', () async {
    expect(await _parse(['event: order.created\ndata: {"id":9}\n\n']), [
      const SseEvent(event: 'order.created', data: '{"id":9}'),
    ]);
  });

  test('joins multiple data lines with a newline', () async {
    expect(await _parse(['data: a\ndata: b\n\n']), [
      const SseEvent(event: 'message', data: 'a\nb'),
    ]);
  });

  test('defaults the event type to message', () async {
    expect(await _parse(['data: x\n\n']), [
      const SseEvent(event: 'message', data: 'x'),
    ]);
  });

  test('accepts LF, CR and CRLF line endings, including a CRLF split across chunks', () async {
    expect(
      await _parse([
        'data: a\r\n\r\ndata: b\r',
        '\n\r\ndata: c\r\rdata: d\n\n',
      ]),
      [
        const SseEvent(event: 'message', data: 'a'),
        const SseEvent(event: 'message', data: 'b'),
        const SseEvent(event: 'message', data: 'c'),
        const SseEvent(event: 'message', data: 'd'),
      ],
    );
  });

  // A stray blank line would split an event, so these fail if CRLF is read as
  // two line endings. The earlier case cannot tell: extra blank lines between
  // events are harmless.
  test(
    'reads CRLF as one line ending, so it does not split an event',
    () async {
      expect(await _parse(['data: a\r\ndata: b\r\n\r\n']), [
        const SseEvent(event: 'message', data: 'a\nb'),
      ]);
      expect(await _parse(['data: a\r', '\ndata: b\r\n\r', '\n']), [
        const SseEvent(event: 'message', data: 'a\nb'),
      ]);
    },
  );

  test(
    'reads a lone CR as a line ending, so two CR lines join into one event',
    () async {
      expect(await _parse(['data: a\rdata: b\r\r']), [
        const SseEvent(event: 'message', data: 'a\nb'),
      ]);
    },
  );

  test('assembles a line split across chunks', () async {
    expect(await _parse(['da', 'ta: he', 'llo\n', '\n']), [
      const SseEvent(event: 'message', data: 'hello'),
    ]);
  });

  test('ignores comments and unknown fields', () async {
    expect(await _parse([': hi\nretry: 10\nfoo: bar\ndata: x\n\n']), [
      const SseEvent(event: 'message', data: 'x'),
    ]);
  });

  test(
    'a comment line is not a blank line and does not end the event',
    () async {
      expect(await _parse(['data: a\n: keepalive\ndata: b\n\n']), [
        const SseEvent(event: 'message', data: 'a\nb'),
      ]);
    },
  );

  test('strips exactly one leading space from a value', () async {
    expect(await _parse(['data:  two\ndata:none\n\n']), [
      const SseEvent(event: 'message', data: ' two\nnone'),
    ]);
  });

  test('carries the last event id forward to later events', () async {
    expect(await _parse(['id: 1\ndata: a\n\ndata: b\n\nid\ndata: c\n\n']), [
      const SseEvent(event: 'message', data: 'a', lastEventId: '1'),
      const SseEvent(event: 'message', data: 'b', lastEventId: '1'),
      const SseEvent(event: 'message', data: 'c', lastEventId: ''),
    ]);
  });

  test('ignores an id containing a NUL', () async {
    expect(await _parse(['id: a\u0000b\ndata: x\n\n']), [
      const SseEvent(event: 'message', data: 'x'),
    ]);
  });

  // Review Focus: a stream that ends inside an event must not deliver half of
  // it, which is what the spec says and what a reconnect relies on.
  test('drops an event the stream ended in the middle of', () async {
    expect(
      await _parse(['data: whole\n\n', 'event: order.updated\ndata: {"id":']),
      [const SseEvent(event: 'message', data: 'whole')],
    );
  });

  test('does not dispatch an event with no data', () async {
    expect(await _parse(['event: lonely\n\n']), isEmpty);
  });

  test('strips a leading byte order mark', () async {
    expect(await _parse(['﻿data: x\n\n']), [
      const SseEvent(event: 'message', data: 'x'),
    ]);
  });

  group('committing the last event id', () {
    test('an id-only event commits its id and dispatches nothing', () async {
      final parsed = await _parseWithIds(['id: 5\n\n']);

      expect(parsed.events, isEmpty);
      expect(parsed.ids, ['5']);
    });

    test(
      'an event with data commits its id before the stream moves on',
      () async {
        final parsed = await _parseWithIds([
          'id: 1\ndata: a\n\nid: 2\ndata: b\n\n',
        ]);

        expect(parsed.events.map((e) => e.lastEventId), ['1', '2']);
        expect(parsed.ids, ['1', '2']);
      },
    );

    test('an empty id commits as the empty string, which clears it', () async {
      final parsed = await _parseWithIds(['id: 1\ndata: a\n\nid\ndata: b\n\n']);

      expect(parsed.ids, ['1', '']);
    });

    test(
      'an id in an event the stream ended in the middle of is never committed',
      () async {
        final parsed = await _parseWithIds([
          'id: 1\ndata: a\n\n',
          'id: 2\ndata: half',
        ]);

        expect(parsed.ids, ['1']);
      },
    );

    test('an id with no terminating blank line is never committed', () async {
      final parsed = await _parseWithIds(['id: 7\n']);

      expect(parsed.ids, isEmpty);
    });

    test('an id that did not change is not committed again', () async {
      final parsed = await _parseWithIds(['id: 3\n\n\n', 'data: x\n\n']);

      expect(parsed.ids, ['3']);
    });
  });
}
