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
      await connection.call(ForgeDevtoolsProtocol.capture, {'enabled': 'true'});

      // The session was read once, before the first change.
      expect(fake.callsTo(ForgeDevtoolsProtocol.snapshot), hasLength(1));
      expect(connection.session, 0);
      for (final method in [
        ForgeDevtoolsProtocol.action,
        ForgeDevtoolsProtocol.outboxAction,
        ForgeDevtoolsProtocol.control,
        ForgeDevtoolsProtocol.capture,
      ]) {
        for (final params in fake.callsTo(method)) {
          expect(params['session'], '0', reason: '$method $params');
          expect(params['cache'], '1');
        }
      }
    });

    // Final fix P6: the app refuses a change that names no session, so the
    // panel must never send one without it. The fake refuses it as the app
    // does.
    test('a change the panel sends always names a session, and the app refuses one that does not', () async {
      final (fake, connection) = await _ready();

      await expectLater(
        fake.call(ForgeDevtoolsProtocol.action, {
          'cache': '1',
          'action': 'clear',
        }),
        throwsA(
          isA<BackendError>().having(
            (e) => e.message,
            'message',
            contains('session is required'),
          ),
        ),
      );

      await connection.call(ForgeDevtoolsProtocol.action, {'action': 'clear'});

      expect(fake.actions, ['clear']);
    });

    // Final fix M1: the first session the panel learns is adopted only after
    // dropping what it already read, which no session fenced.
    test('the first session seen after something was read drops what was read', () async {
      final (fake, connection) = await _ready();
      expect(connection.session, isNull);

      // A page of the previous principal's, read before any session was known.
      await connection.call(ForgeDevtoolsProtocol.queries);
      final generation = connection.generation;

      // The switch happened; the first snapshot already says session 1.
      fake.session = 1;
      await connection.call(ForgeDevtoolsProtocol.snapshot);

      expect(connection.session, 1);
      expect(connection.generation, greaterThan(generation));
    });

    test(
      'the first session seen before anything was read is taken as it is',
      () async {
        final (fake, connection) = await _ready();
        final generation = connection.generation;

        await connection.call(ForgeDevtoolsProtocol.snapshot);

        expect(connection.session, 0);
        expect(connection.generation, generation);
      },
    );

    test('an aimed change whose first session read follows a read is refused as moved, and sends nothing', () async {
      final (fake, connection) = await _ready();
      await connection.call(ForgeDevtoolsProtocol.queries);
      fake.session = 1;

      await expectLater(
        connection.call(ForgeDevtoolsProtocol.action, {'action': 'clear'}),
        throwsA(
          isA<BackendError>().having(
            (e) => e.message,
            'message',
            ForgeConnection.movedMessage,
          ),
        ),
      );

      expect(fake.actions, isEmpty);
      expect(connection.session, 1);
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

  // Final review I2, from the reviewer's probe: the panel stayed on a
  // disposed cache, kept its events, and dropped every event for the cache
  // that replaced it.
  group('a cache that is disposed or replaced', () {
    test('a disposed cache and a new one in the same isolate: the panel moves to the new one and keeps nothing of the old', () async {
      final (fake, connection) = await _ready();
      fake.emit({
        'cache': '1',
        'entries': [
          {
            'kind': 'mutation',
            'seq': 1,
            'session': 0,
            'at': 1,
            'operation': 'alice-op',
          },
        ],
        'skipped': 0,
      });
      await pumpEventQueue();
      expect(connection.events.single['operation'], 'alice-op');
      final generation = connection.generation;

      // The app closes cache 1 and opens cache 2 for the next user.
      fake
        ..gone.add('1')
        ..caches = [
          {'id': '2', 'label': 'cache 2'},
        ];
      fake.emitLifecycle('1', ForgeDevtoolsProtocol.detached);
      fake.emitLifecycle('2', ForgeDevtoolsProtocol.attached);
      await pumpEventQueue();

      expect(connection.phase, ConnectionPhase.ready);
      expect(connection.cacheId, '2');
      expect(connection.events, isEmpty);
      expect(connection.session, isNull);
      expect(connection.generation, greaterThan(generation));
      expect(fake.callsTo(ForgeDevtoolsProtocol.hello).length, greaterThan(1));

      // Cache 2's events now show.
      fake.emit({
        'cache': '2',
        'entries': [
          {
            'kind': 'mutation',
            'seq': 1,
            'session': 0,
            'at': 1,
            'operation': 'bob-op',
          },
        ],
        'skipped': 0,
      });
      await pumpEventQueue();

      expect([for (final e in connection.events) e['operation']], ['bob-op']);

      // And calls are scoped to it.
      await connection.call(ForgeDevtoolsProtocol.queries);
      expect(fake.callsTo(ForgeDevtoolsProtocol.queries).last['cache'], '2');
    });

    test('a call refused because the cache is gone says hello again and drops what the panel holds', () async {
      final (fake, connection) = await _ready();
      await _emit(fake, [_entry('fetch', 1, 0)]);
      final generation = connection.generation;

      // The lifecycle event never arrived (a missed post), but the refusal does.
      fake
        ..gone.add('1')
        ..caches = [
          {'id': '2', 'label': 'cache 2'},
        ];

      await expectLater(
        connection.call(ForgeDevtoolsProtocol.queries),
        throwsA(
          isA<BackendError>().having(
            (e) => e.code,
            'code',
            ForgeDevtoolsProtocol.cacheGone,
          ),
        ),
      );
      await pumpEventQueue();

      expect(fake.callsTo(ForgeDevtoolsProtocol.hello), hasLength(2));
      expect(connection.cacheId, '2');
      expect(connection.events, isEmpty);
      expect(connection.generation, greaterThan(generation));
    });

    test('a refused change says hello again too, without reading the session of a cache that is gone', () async {
      final (fake, connection) = await _ready();
      await connection.call(ForgeDevtoolsProtocol.snapshot);
      fake
        ..gone.add('1')
        ..caches = [
          {'id': '2', 'label': 'cache 2'},
        ];

      await expectLater(
        connection.call(ForgeDevtoolsProtocol.action, {
          'action': 'invalidate',
          'target': 'GET /orders',
        }),
        throwsA(isA<BackendError>()),
      );
      await pumpEventQueue();

      expect(fake.actions, isEmpty);
      expect(connection.cacheId, '2');
      expect(fake.callsTo(ForgeDevtoolsProtocol.hello), hasLength(2));
    });

    test('a refusal that is not about a gone cache says no hello', () async {
      final (fake, connection) = await _ready();
      fake.overrides[ForgeDevtoolsProtocol.queries] = (_) =>
          throw const BackendError(ForgeDevtoolsProtocol.queries, 'busy');

      await expectLater(
        connection.call(ForgeDevtoolsProtocol.queries),
        throwsA(isA<BackendError>()),
      );
      await pumpEventQueue();

      expect(fake.callsTo(ForgeDevtoolsProtocol.hello), hasLength(1));
    });

    test('a cache attaching beside the picked one keeps the pick, and still clears what was shown', () async {
      final (fake, connection) = await _ready();
      await _emit(fake, [_entry('fetch', 1, 0)]);
      final generation = connection.generation;

      fake.caches = [
        {'id': '1', 'label': 'cache 1'},
        {'id': '2', 'label': 'cache 2'},
      ];
      fake.emitLifecycle('2', ForgeDevtoolsProtocol.attached);
      await pumpEventQueue();

      expect(connection.cacheId, '1');
      expect(connection.caches.map((c) => c['id']), ['1', '2']);
      expect(connection.events, isEmpty);
      expect(connection.generation, greaterThan(generation));
    });

    test('only the latest hello is used when several run at once', () async {
      final (fake, connection) = await _ready();
      final slow = Completer<Json>();
      fake.overrides[ForgeDevtoolsProtocol.hello] = (_) => slow.future;
      fake.emitLifecycle('2', ForgeDevtoolsProtocol.attached);
      await pumpEventQueue();

      fake.overrides.clear();
      fake.caches = [
        {'id': '3', 'label': 'cache 3'},
      ];
      fake.emitLifecycle('3', ForgeDevtoolsProtocol.attached);
      await pumpEventQueue();
      slow.complete({
        'protocol': ForgeDevtoolsProtocol.version,
        'caches': [
          {'id': '2', 'label': 'cache 2'},
        ],
      });
      await pumpEventQueue();

      expect(connection.cacheId, '3');
      expect(connection.phase, ConnectionPhase.ready);
    });
  });

  group('isolate changes', () {
    test('an isolate swap drops the cache id, the session and the events, and says hello again', () async {
      final (fake, connection) = await _ready(
        FakeForgeBackend()
          ..caches = [
            {'id': '4', 'label': 'cache 4'},
          ]
          ..session = 3,
      );
      await connection.call(ForgeDevtoolsProtocol.snapshot);
      fake.emit({
        'cache': '4',
        'entries': [_entry('fetch', 1, 3)],
        'skipped': 0,
      });
      await pumpEventQueue();
      expect(connection.cacheId, '4');
      expect(connection.session, 3);
      expect(connection.events, hasLength(1));
      final generation = connection.generation;

      // A hot restart: the new isolate attaches its cache fresh.
      fake
        ..caches = [
          {'id': '1', 'label': 'cache 1'},
        ]
        ..session = 0;
      final before = fake.calls.length;
      fake.swapIsolate();
      await pumpEventQueue();

      expect(connection.phase, ConnectionPhase.ready);
      expect(fake.callsTo(ForgeDevtoolsProtocol.hello), hasLength(2));
      expect(connection.cacheId, '1');
      expect(connection.session, isNull);
      expect(connection.events, isEmpty);
      expect(connection.generation, greaterThan(generation));

      // The first change after the swap is aimed at the new isolate's
      // session, not refused for carrying the old one.
      await connection.call(ForgeDevtoolsProtocol.action, {
        'action': 'invalidate',
        'target': 'GET /orders',
      });
      expect(fake.callsTo(ForgeDevtoolsProtocol.action).single['session'], '0');
      expect(
        fake.calls.skip(before).any((c) => c.params['cache'] == '4'),
        isFalse,
      );
    });

    test(
      'a swap keeps nothing even when the new isolate reuses the cache id',
      () async {
        final (fake, connection) = await _ready(
          FakeForgeBackend()..session = 2,
        );
        await connection.call(ForgeDevtoolsProtocol.snapshot);
        await _emit(fake, [_entry('fetch', 1, 2)]);

        fake.session = 0;
        fake.swapIsolate();
        await pumpEventQueue();

        expect(connection.cacheId, '1');
        expect(connection.session, isNull);
        expect(connection.events, isEmpty);
      },
    );

    test('a hello answered after the app went away is ignored', () async {
      final fake = FakeForgeBackend();
      final slow = Completer<Json>();
      fake.overrides[ForgeDevtoolsProtocol.hello] = (_) => slow.future;
      final connection = ForgeConnection(fake);
      addTearDown(connection.dispose);
      await pumpEventQueue();
      expect(connection.phase, ConnectionPhase.connecting);

      fake.isAvailable = false;
      await pumpEventQueue();
      slow.complete({
        'protocol': ForgeDevtoolsProtocol.version,
        'caches': [
          {'id': '9', 'label': 'cache 9'},
        ],
      });
      await pumpEventQueue();

      expect(connection.phase, ConnectionPhase.unavailable);
      expect(connection.cacheId, isNull);
      expect(connection.caches, isEmpty);
    });

    test('a hello answered by the previous isolate is ignored', () async {
      final fake = FakeForgeBackend();
      final first = Completer<Json>();
      fake.overrides[ForgeDevtoolsProtocol.hello] = (_) => first.future;
      final connection = ForgeConnection(fake);
      addTearDown(connection.dispose);
      await pumpEventQueue();

      fake.overrides.clear();
      fake.swapIsolate();
      await pumpEventQueue();
      first.complete({
        'protocol': ForgeDevtoolsProtocol.version,
        'caches': [
          {'id': '9', 'label': 'cache 9'},
        ],
      });
      await pumpEventQueue();

      expect(connection.cacheId, '1');
      expect(connection.caches.map((c) => c['id']), ['1']);
    });

    test(
      'picking another cache forgets the session of the one it leaves',
      () async {
        final (fake, connection) = await _ready(
          FakeForgeBackend()
            ..caches = [
              {'id': '1', 'label': 'cache 1'},
              {'id': '2', 'label': 'cache 2'},
            ]
            ..session = 5,
        );
        await connection.call(ForgeDevtoolsProtocol.snapshot);
        expect(connection.session, 5);

        connection.selectCache('1');

        expect(connection.session, isNull);
      },
    );
  });
}
