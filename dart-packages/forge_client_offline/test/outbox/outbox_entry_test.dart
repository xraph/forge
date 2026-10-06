import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

OutboxEntry entry({
  DateTime? sentAt,
  String? failureJson,
  Object? body = const {
    'note': 'x',
    'lines': [1, 2],
  },
}) => OutboxEntry(
  id: 'm1',
  operationId: 'op_update_order',
  args: TagContext(
    path: const {'id': '7'},
    query: const {'notify': true},
    headers: const {'X-Tenant': 'acme'},
    body: body,
  ),
  requestHeaders: const {'X-Trace': 't'},
  intent: const MergeOverlay('Order:7', {'note': 'x'}),
  idempotencyKey: 'key-1',
  createdAt: DateTime.utc(2026, 10, 4, 12),
  seq: 41,
  sentAt: sentAt,
  failureJson: failureJson,
);

/// The record of [entry] with its args replaced by [argsJson].
PendingMutationRecord withArgs(String argsJson) {
  final good = entry().toRecord();

  return PendingMutationRecord(
    id: good.id,
    operationId: good.operationId,
    argsJson: argsJson,
    idempotencyKey: good.idempotencyKey,
    createdAt: good.createdAt,
    stateJson: good.stateJson,
  );
}

/// An args envelope whose `args` object is [args].
String envelope(Map<String, Object?> args) => jsonEncode({
  'v': 1,
  'seq': 1,
  'requestHeaders': <String, String>{},
  'args': args,
});

Uint8List allBytes() => Uint8List.fromList(List<int>.generate(256, (i) => i));

void main() {
  test('an entry survives toRecord and fromRecord', () {
    final original = entry(failureJson: '{"kind":"gone","status":410}');
    final back = OutboxEntry.fromRecord(original.toRecord());

    expect(back.id, 'm1');
    expect(back.operationId, 'op_update_order');
    expect(back.args.path, {'id': '7'});
    expect(back.args.query, {'notify': true});
    expect(back.args.headers, {'X-Tenant': 'acme'});
    expect(back.args.body, {
      'note': 'x',
      'lines': [1, 2],
    });
    expect(back.requestHeaders, {'X-Trace': 't'});
    expect(
      back.intent,
      isA<MergeOverlay>().having((i) => i.key, 'key', 'Order:7'),
    );
    expect(back.idempotencyKey, 'key-1');
    expect(back.createdAt, DateTime.utc(2026, 10, 4, 12));
    expect(back.seq, 41);
    expect(back.sentAt, isNull);
    expect(back.failureJson, '{"kind":"gone","status":410}');
  });

  test('the stored args carry the envelope version and the sequence', () {
    final json =
        jsonDecode(entry().toRecord().argsJson) as Map<String, Object?>;

    expect(json['v'], 1);
    expect(json['seq'], 41);
  });

  test('the state travels in stateJson: queued, sending, or failed', () {
    expect(entry().stateJson, '{"kind":"queued"}');
    expect(entry().toRecord().stateJson, '{"kind":"queued"}');

    final sending = entry(
      sentAt: DateTime.fromMillisecondsSinceEpoch(5000, isUtc: true),
    );
    expect(jsonDecode(sending.stateJson), {'kind': 'sending', 'at': 5000});
    expect(
      OutboxEntry.fromRecord(sending.toRecord()).sentAt,
      DateTime.fromMillisecondsSinceEpoch(5000, isUtc: true),
    );
    expect(OutboxEntry.fromRecord(sending.toRecord()).failureJson, isNull);

    final failed = entry(
      sentAt: DateTime.utc(2026),
      failureJson: '{"kind":"conflict","status":409}',
    );
    expect(jsonDecode(failed.stateJson), {
      'kind': 'failed',
      'failure': {'kind': 'conflict', 'status': 409},
    });
    expect(
      OutboxEntry.fromRecord(failed.toRecord()).failureJson,
      '{"kind":"conflict","status":409}',
    );

    final record = entry().toRecord();
    final queuedAgain = OutboxEntry.fromRecord(
      PendingMutationRecord(
        id: record.id,
        operationId: record.operationId,
        argsJson: record.argsJson,
        optimisticJson: record.optimisticJson,
        idempotencyKey: record.idempotencyKey,
        createdAt: record.createdAt,
        stateJson: '{"kind":"queued"}',
      ),
    );
    expect(queuedAgain.sentAt, isNull);
    expect(queuedAgain.failureJson, isNull);

    final unreadable = OutboxEntry.fromRecord(
      PendingMutationRecord(
        id: record.id,
        operationId: record.operationId,
        argsJson: record.argsJson,
        idempotencyKey: record.idempotencyKey,
        createdAt: record.createdAt,
        stateJson: 'not json',
      ),
    );
    expect(
      unreadable.failureJson,
      'not json',
      reason: 'an unreadable state surfaces as a failure, never as pending',
    );
  });

  test('a failed state without a failure object is kept as raw text', () {
    final record = entry().toRecord();
    PendingMutationRecord withState(String state) => PendingMutationRecord(
      id: record.id,
      operationId: record.operationId,
      argsJson: record.argsJson,
      idempotencyKey: record.idempotencyKey,
      createdAt: record.createdAt,
      stateJson: state,
    );

    for (final state in [
      '{"kind":"failed"}',
      '{"kind":"failed","failure":null}',
      '{"kind":"failed","failure":"text"}',
    ]) {
      final back = OutboxEntry.fromRecord(withState(state));

      expect(back.failureJson, state, reason: state);
      expect(back.sentAt, isNull);
    }

    // And writing one never stores `failure: null`.
    for (final failure in ['null', '5', 'not json']) {
      final written = jsonDecode(entry(failureJson: failure).stateJson);

      expect(written, {
        'kind': 'failed',
        'failure': {'kind': 'unreadable', 'raw': failure},
      });
    }
  });

  test('an unknown envelope or broken JSON is a FormatException', () {
    expect(
      () => OutboxEntry.fromRecord(withArgs('{"v":2,"seq":1,"args":{}}')),
      throwsFormatException,
    );
    expect(
      () => OutboxEntry.fromRecord(withArgs('not json')),
      throwsFormatException,
    );
    expect(
      () => OutboxEntry.fromRecord(withArgs('{"v":1,"args":{}}')),
      throwsFormatException,
    );
  });

  test('copyWith can clear sentAt and the failure', () {
    final sent = entry(sentAt: DateTime.utc(2026), failureJson: '{}');
    final cleared = sent.copyWith(
      clearSentAt: true,
      clearFailure: true,
      idempotencyKey: 'key-2',
      id: 'm2',
    );

    expect(cleared.sentAt, isNull);
    expect(cleared.failureJson, isNull);
    expect(cleared.idempotencyKey, 'key-2');
    expect(cleared.id, 'm2');
    expect(cleared.seq, 41);
  });

  test('canonicalArgs is equal for equal arguments only', () {
    expect(entry().canonicalArgs, entry().canonicalArgs);
    expect(
      entry().copyWith(args: const TagContext(path: {'id': '8'})).canonicalArgs,
      isNot(entry().canonicalArgs),
    );
  });

  test(
    'persistableHeaders drops credentials and outbox headers in any case',
    () {
      expect(
        persistableHeaders({
          'Authorization': 'Bearer t',
          'cookie': 'a=b',
          'idempotency-key': 'k',
          'X-Forge-Outbox-Replay': 'm1',
          'X-Trace': 't',
        }),
        {'X-Trace': 't'},
      );
    },
  );

  test('headerValue ignores case', () {
    expect(headerValue({'idempotency-key': 'k'}, 'Idempotency-Key'), 'k');
    expect(headerValue({}, 'Idempotency-Key'), isNull);
  });

  test('PUT, DELETE and idempotent operations are safe to repeat; POST and PATCH are not', () {
    OperationMeta op(String method, {bool idempotent = false}) => OperationMeta(
      id: method,
      method: method,
      path: '/x',
      idempotent: idempotent,
    );

    expect(isSafeToRepeat(op('PUT')), isTrue);
    expect(isSafeToRepeat(op('delete')), isTrue);
    expect(isSafeToRepeat(op('POST', idempotent: true)), isTrue);
    expect(isSafeToRepeat(op('POST')), isFalse);
    expect(isSafeToRepeat(op('PATCH')), isFalse);
  });

  group('a binary body', () {
    test('keeps its exact bytes through toRecord and fromRecord', () {
      final bytes = allBytes();
      final back = OutboxEntry.fromRecord(entry(body: bytes).toRecord());

      expect(back.args.body, isA<Uint8List>());
      expect(back.args.body, orderedEquals(bytes));
    });

    test('keeps the bytes of a view onto a larger buffer', () {
      final buffer = allBytes();
      final view = Uint8List.sublistView(buffer, 10, 20);
      final back = OutboxEntry.fromRecord(entry(body: view).toRecord());

      expect(back.args.body, isA<Uint8List>());
      expect(back.args.body, orderedEquals(buffer.sublist(10, 20)));
    });

    test('an empty body stays an empty Uint8List, not a missing body', () {
      final back = OutboxEntry.fromRecord(entry(body: Uint8List(0)).toRecord());

      expect(back.args.body, isA<Uint8List>());
      expect(back.args.body as Uint8List, isEmpty);
    });

    test('is stored as base64 under bodyBase64, never as an int array', () {
      final bytes = allBytes();
      final json = jsonDecode(
        entry(body: bytes).toRecord().argsJson,
      ) as Map<String, Object?>;
      final args = json['args']! as Map<String, Object?>;

      expect(args['bodyBase64'], base64Encode(bytes));
      expect(args.containsKey('body'), isFalse);
    });

    test('is part of canonicalArgs by content', () {
      final a = Uint8List.fromList([1, 2, 3]);

      expect(
        entry(body: a).canonicalArgs,
        entry(body: Uint8List.fromList([1, 2, 3])).canonicalArgs,
      );
      expect(
        entry(body: a).canonicalArgs,
        isNot(entry(body: Uint8List.fromList([1, 2, 4])).canonicalArgs),
      );
    });

    test('is not confused with a JSON body that looks like the marker', () {
      final lookalikes = <Object?>[
        {'bodyBase64': 'AAEC'},
        {
          'args': {'bodyBase64': 'AAEC'},
        },
        'AAEC',
        [0, 1, 2],
        {r'$bytes': 'AAEC', 'bodyBase64': 'AAEC'},
      ];

      for (final body in lookalikes) {
        final back = OutboxEntry.fromRecord(entry(body: body).toRecord());

        expect(back.args.body, isNot(isA<Uint8List>()), reason: '$body');
        expect(back.args.body, body);
      }
    });

    test('a record that carries both a body and bytes is unreadable', () {
      expect(
        () => OutboxEntry.fromRecord(
          withArgs(
            envelope({
              'body': {'a': 1},
              'bodyBase64': 'AAEC',
            }),
          ),
        ),
        throwsFormatException,
      );
    });

    test('bytes that are not base64 are unreadable', () {
      expect(
        () => OutboxEntry.fromRecord(
          withArgs(envelope({'bodyBase64': '***not base64***'})),
        ),
        throwsFormatException,
      );
      expect(
        () => OutboxEntry.fromRecord(withArgs(envelope({'bodyBase64': 7}))),
        throwsFormatException,
      );
    });

    test(
      'replays as bytes under the operation\'s request content type',
      () async {
        late http.Request seen;
        final transport = RestTransport(
          baseUrl: Uri.parse('https://api.example.com'),
          client: MockClient((request) async {
            seen = request;
            return http.Response('', 204);
          }),
        );
        const upload = OperationMeta(
          id: 'op_upload_avatar',
          method: 'PUT',
          path: '/avatars/{id}',
          requestContentType: 'image/png',
        );
        final bytes = allBytes();

        final back = OutboxEntry.fromRecord(entry(body: bytes).toRecord());
        await transport.execute(
          TransportRequest(meta: upload, args: back.args),
        );

        expect(seen.bodyBytes, orderedEquals(bytes));
        expect(seen.headers['content-type'], 'image/png');
      },
    );

    test('a body flattened to a JSON array cannot be replayed', () async {
      // What jsonEncode alone would have persisted: the int array comes back
      // as a List, and the transport refuses it for a binary content type.
      final transport = RestTransport(
        baseUrl: Uri.parse('https://api.example.com'),
        client: MockClient((request) async => http.Response('', 204)),
      );
      const upload = OperationMeta(
        id: 'op_upload_avatar',
        method: 'PUT',
        path: '/avatars/{id}',
        requestContentType: 'image/png',
      );
      final flattened = jsonDecode(jsonEncode(allBytes()));

      expect(flattened, isNot(isA<Uint8List>()));
      await expectLater(
        transport.execute(
          TransportRequest(
            meta: upload,
            args: TagContext(path: const {'id': '7'}, body: flattened),
          ),
        ),
        throwsArgumentError,
      );
    });
  });
}
