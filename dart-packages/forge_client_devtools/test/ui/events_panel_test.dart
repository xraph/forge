import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/src/ui/events_panel.dart';

import '../support/pump.dart';

void main() {
  testWidgets(
    'backfills the log, then appends live events without repeating one',
    (tester) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Events');

      expect(
        find.textContaining('GET /orders fetched (mount)'),
        findsOneWidget,
      );

      fake.emit({
        'cache': '1',
        'skipped': 0,
        'entries': [
          {
            'kind': 'settle',
            'seq': 2,
            'at': 2,
            'session': 0,
            'query': 'GET /orders',
            'version': 3,
          },
          {
            'kind': 'mutation',
            'seq': 3,
            'at': 3,
            'session': 0,
            'operation': 'POST /orders',
            'args': '{}',
            'tags': ['Order:9'],
            'unresolved': <String>[],
          },
        ],
      });
      await tester.pumpAndSettle();

      expect(
        find.textContaining('POST /orders raised Order:9'),
        findsOneWidget,
      );
      expect(
        find.textContaining('GET /orders settled, store v3'),
        findsOneWidget,
      );
    },
  );

  testWidgets('filters by kind', (tester) async {
    await pumpPanel(tester);
    await openTab(tester, 'Events');

    await tester.tap(find.byKey(const ValueKey('events-kind-settle')));
    await tester.pumpAndSettle();

    expect(find.textContaining('settled, store v3'), findsOneWidget);
    expect(find.textContaining('fetched (mount)'), findsNothing);
  });

  testWidgets('says how much the app dropped or skipped', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Events');

    fake.emit({'cache': '1', 'skipped': 5, 'entries': <Object?>[]});
    await tester.pumpAndSettle();

    expect(find.textContaining('skipped 5'), findsOneWidget);
  });

  test('describes every log kind in one line', () {
    expect(
      describeLogEntry({
        'kind': 'invalidated',
        'query': 'GET /orders',
        'matched': ['Order[]'],
        'cause': 3,
      }),
      'GET /orders hit by Order[] (cause #3)',
    );
    expect(
      describeLogEntry({
        'kind': 'outbox',
        'phase': 'failed',
        'mutationId': 'm2',
        'failure': 'OutboxConflict',
      }),
      'outbox failed m2: OutboxConflict',
    );
    expect(
      describeLogEntry({
        'kind': 'sync',
        'entity': 'Doc',
        'status': 'pending',
        'detail': '3',
      }),
      'sync Doc pending (3)',
    );
    expect(
      describeLogEntry({
        'kind': 'action',
        'action': 'evict',
        'target': 'Order:1',
      }),
      'panel evict Order:1',
    );
    expect(
      describeLogEntry({'kind': 'principal', 'session': 2}),
      'identity changed, session 2',
    );
  });

  testWidgets('a log read that holds a principal marker keeps the marker and '
      'what follows it, and nothing before it', (tester) async {
    final fake = await pumpPanel(tester);
    fake.overrides[ForgeDevtoolsProtocol.log] = (_) => {
      'entries': [
        {
          'kind': 'mutation',
          'seq': 1,
          'at': 1,
          'session': 0,
          'operation': 'POST /users',
          'args': '{"ssn":"alice-ssn"}',
          'tags': ['User:alice-ssn'],
          'unresolved': <String>[],
        },
        {'kind': 'principal', 'seq': 2, 'at': 2, 'session': 0},
        {
          'kind': 'fetch',
          'seq': 3,
          'at': 3,
          'session': 0,
          'query': 'GET /orders',
          'reason': 'mount',
          'cause': null,
        },
      ],
      'dropped': 0,
      'sequence': 4,
      'session': 0,
      'truncated': false,
    };

    await openTab(tester, 'Events');

    expect(find.textContaining('alice-ssn', skipOffstage: false), findsNothing);
    expect(find.text('identity changed, session 0'), findsOneWidget);
    expect(find.textContaining('GET /orders fetched (mount)'), findsOneWidget);
  });

  testWidgets('never holds more than the runtime keeps', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Events');

    for (var batch = 0; batch < 6; batch++) {
      fake.emit({
        'cache': '1',
        'skipped': 0,
        'entries': [
          for (var i = 0; i < 200; i++)
            {
              'kind': 'settle',
              'seq': 10 + batch * 200 + i,
              'at': 10,
              'session': 0,
              'query': 'GET /orders',
              'version': 3,
            },
        ],
      });
    }
    await tester.pumpAndSettle();

    final list = tester.widget<ListView>(find.byType(ListView));
    expect(list.childrenDelegate.estimatedChildCount, 1000);
    // The oldest are the ones that went: the log's two entries, and then the
    // first 200 posted, which the connection let go of itself.
    expect(find.text('#210'), findsOneWidget);
    expect(find.text('#1'), findsNothing);
    expect(find.text('#2'), findsNothing);
  });

  testWidgets('shows a row the app would not send as too large', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);
    fake.overrides[ForgeDevtoolsProtocol.log] = (_) => {
      'entries': [
        {'oversized': true, 'field': 'query', 'seq': 1, 'kind': 'fetch'},
      ],
      'dropped': 0,
      'sequence': 2,
      'session': 0,
      'truncated': false,
    };

    await openTab(tester, 'Events');

    expect(find.textContaining('too large to show (query'), findsOneWidget);
  });
}
