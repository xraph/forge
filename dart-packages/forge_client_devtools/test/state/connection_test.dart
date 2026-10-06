import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';
import 'package:forge_client_devtools/src/state/connection.dart';

import '../support/fake_backend.dart';

Future<(FakeForgeBackend, ForgeConnection)> _ready([
  FakeForgeBackend? backend,
]) async {
  final fake = backend ?? FakeForgeBackend();
  final connection = ForgeConnection(fake);
  addTearDown(connection.dispose);
  await pumpEventQueue();
  expect(connection.phase, ConnectionPhase.ready);
  return (fake, connection);
}

Json _entry(String kind, int seq, int session) => {
  'kind': kind,
  'seq': seq,
  'at': seq,
  'session': session,
  'query': 'GET /orders',
};

Future<void> _emit(FakeForgeBackend fake, List<Json> entries) async {
  fake.emit({'cache': '1', 'entries': entries, 'skipped': 0});
  await pumpEventQueue();
}

void main() {
  group('session', () {
    test('every action, outbox action and network change carries the session it read first', () async {
      final (fake, connection) = await _ready();
      expect(connection.session, isNull);

      await connection.call(ForgeDevtoolsProtocol.action, {
        'action': 'refetch',
        'target': 'GET /orders',
      });
      await connection.call(ForgeDevtoolsProtocol.outboxAction, {
        'action': 'replay',
        'id': 'm1',
      });
      await connection.call(ForgeDevtoolsProtocol.control, {'failNext': '503'});
      await connection.call(ForgeDevtoolsProtocol.control, {'mode': 'offline'});

      // The session was read once, before the first change.
      expect(fake.callsTo(ForgeDevtoolsProtocol.snapshot), hasLength(1));
      expect(connection.session, 0);
      for (final method in [
        ForgeDevtoolsProtocol.action,
        ForgeDevtoolsProtocol.outboxAction,
        ForgeDevtoolsProtocol.control,
      ]) {
        for (final params in fake.callsTo(method)) {
          expect(params['session'], '0', reason: '$method $params');
          expect(params['cache'], '1');
        }
      }
    });

    test('a read sends no session', () async {
      final (fake, connection) = await _ready();

      await connection.call(ForgeDevtoolsProtocol.control);
      await connection.call(ForgeDevtoolsProtocol.queries);

      expect(fake.callsTo(ForgeDevtoolsProtocol.control).single, {
        'cache': '1',
      });
      expect(
        fake
            .callsTo(ForgeDevtoolsProtocol.queries)
            .single
            .containsKey('session'),
        isFalse,
      );
    });

    test('a change refused because the principal changed reads the session again and drops what the panel holds', () async {
      final (fake, connection) = await _ready();
      await connection.call(ForgeDevtoolsProtocol.snapshot);
      await _emit(fake, [_entry('fetch', 1, 0)]);
      final generation = connection.generation;

      // The app switched account and the panel has not heard yet.
      fake.session = 1;

      await expectLater(
        connection.call(ForgeDevtoolsProtocol.action, {
          'action': 'invalidate',
          'target': 'GET /orders',
        }),
        throwsA(
          isA<BackendError>().having(
            (e) => e.message,
            'message',
            contains('the principal changed'),
          ),
        ),
      );

      expect(fake.actions, isEmpty);
      expect(connection.session, 1);
      expect(connection.generation, greaterThan(generation));
      expect(connection.events, isEmpty);
    });
  });

  group('principal changes (P4)', () {
    test(
      'a principal marker clears the event list and keeps only what follows it',
      () async {
        final (fake, connection) = await _ready();
        await _emit(fake, [_entry('fetch', 1, 0), _entry('settle', 2, 0)]);
        expect(connection.events, hasLength(2));
        final generation = connection.generation;

        await _emit(fake, [
          _entry('settle', 3, 0),
          {'kind': 'principal', 'seq': 4, 'at': 4, 'session': 1},
          _entry('fetch', 5, 1),
        ]);

        expect([for (final e in connection.events) e['seq']], [4, 5]);
        expect(connection.session, 1);
        expect(connection.generation, greaterThan(generation));
      },
    );

    test(
      'a marker clears even when a snapshot already showed the new session',
      () async {
        final (fake, connection) = await _ready();
        fake.session = 1;
        await connection.call(ForgeDevtoolsProtocol.snapshot);
        await _emit(fake, [_entry('fetch', 1, 1)]);
        final generation = connection.generation;

        await _emit(fake, [
          {'kind': 'principal', 'seq': 2, 'at': 2, 'session': 1},
        ]);

        expect([for (final e in connection.events) e['seq']], [2]);
        expect(connection.generation, greaterThan(generation));
      },
    );

    test(
      'an entry from a later session clears the list even without its marker',
      () async {
        final (fake, connection) = await _ready();
        await _emit(fake, [_entry('fetch', 1, 0)]);

        await _emit(fake, [_entry('settle', 2, 0), _entry('fetch', 9, 1)]);

        expect([for (final e in connection.events) e['seq']], [9]);
        expect(connection.session, 1);
      },
    );

    test(
      'a log read from a later session drops the event list and every panel',
      () async {
        final (fake, connection) = await _ready();
        await _emit(fake, [_entry('fetch', 1, 0)]);
        final generation = connection.generation;

        fake.session = 2;
        await connection.call(ForgeDevtoolsProtocol.log);

        expect(connection.events, isEmpty);
        expect(connection.session, 2);
        expect(connection.generation, greaterThan(generation));
      },
    );

    test(
      'a log or snapshot read from the same session changes nothing',
      () async {
        final (fake, connection) = await _ready();
        await _emit(fake, [_entry('fetch', 1, 0)]);
        final generation = connection.generation;

        await connection.call(ForgeDevtoolsProtocol.log);
        await connection.call(ForgeDevtoolsProtocol.snapshot);

        expect(connection.events, hasLength(1));
        expect(connection.generation, generation);
      },
    );

    test('an answer that arrives after the principal changed is dropped, not returned', () async {
      final (fake, connection) = await _ready();
      final answer = Completer<Json>();
      fake.overrides[ForgeDevtoolsProtocol.queries] = (_) => answer.future;

      final pending = connection.call(ForgeDevtoolsProtocol.queries);
      await pumpEventQueue();
      await _emit(fake, [
        {'kind': 'principal', 'seq': 1, 'at': 1, 'session': 1},
      ]);
      answer.complete({
        'total': 1,
        'offset': 0,
        'truncated': false,
        'items': [
          {'key': 'GET /alice/secrets'},
        ],
      });

      await expectLater(
        pending,
        throwsA(
          isA<BackendError>().having(
            (e) => e.message,
            'message',
            ForgeConnection.movedMessage,
          ),
        ),
      );
    });

    test('forgets the session and the events when the app goes away', () async {
      final (fake, connection) = await _ready();
      await connection.call(ForgeDevtoolsProtocol.snapshot);
      await _emit(fake, [_entry('fetch', 1, 0)]);

      fake.isAvailable = false;
      await pumpEventQueue();

      expect(connection.phase, ConnectionPhase.unavailable);
      expect(connection.events, isEmpty);
      expect(connection.session, isNull);
    });

    test(
      'events for another cache are ignored and never clear this one',
      () async {
        final (fake, connection) = await _ready();
        await _emit(fake, [_entry('fetch', 1, 0)]);

        fake.emit({
          'cache': '2',
          'entries': [
            {'kind': 'principal', 'seq': 1, 'at': 1, 'session': 5},
          ],
          'skipped': 0,
        });
        await pumpEventQueue();

        expect(connection.events, hasLength(1));
        expect(connection.session, 0);
      },
    );
  });
}
