import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

void main() {
  testWidgets('lists the queries with their status, mounts and tag counts', (
    tester,
  ) async {
    await pumpPanel(tester);

    expect(find.byKey(const ValueKey('query-GET /orders')), findsOneWidget);
    expect(find.textContaining('success'), findsWidgets);
    expect(find.text('2 queries'), findsOneWidget);
  });

  testWidgets('opens a query to show provides, tags, deps and the last value', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);

    await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
    await tester.pumpAndSettle();

    expect(
      fake.callsTo(ForgeDevtoolsProtocol.query).last['key'],
      'GET /orders',
    );
    expect(find.text('tags (2)'), findsOneWidget);
    expect(find.text('Order:1, Order[]'), findsOneWidget);
    expect(find.text('deps (1)'), findsOneWidget);
  });

  testWidgets(
    'runs refetch, invalidate, mark stale and drop on the selected query',
    (tester) async {
      final fake = await pumpPanel(tester);

      await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
      await tester.pumpAndSettle();

      for (final action in ['refetch', 'invalidate', 'stale', 'drop']) {
        await tester.tap(find.byKey(ValueKey('query-action-$action')));
        await tester.pumpAndSettle();
      }

      expect(fake.actions, [
        'refetch GET /orders',
        'invalidate GET /orders',
        'stale GET /orders',
        'drop GET /orders',
      ]);
    },
  );

  testWidgets('filters by key on submit', (tester) async {
    final fake = await pumpPanel(tester);

    await tester.enterText(
      find.byKey(const ValueKey('queries-filter')),
      '{id}',
    );
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();

    expect(fake.callsTo(ForgeDevtoolsProtocol.queries).last['filter'], '{id}');
    expect(find.byKey(const ValueKey('query-GET /orders')), findsNothing);
    expect(find.text('1 queries'), findsOneWidget);
  });

  testWidgets('re-reads the list when the app reports activity', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);
    final before = fake.callsTo(ForgeDevtoolsProtocol.queries).length;

    fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pumpAndSettle();

    expect(
      fake.callsTo(ForgeDevtoolsProtocol.queries).length,
      greaterThan(before),
    );
  });

  testWidgets('sends the session it last read with every action', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);

    await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('query-action-invalidate')));
    await tester.pumpAndSettle();

    expect(fake.callsTo(ForgeDevtoolsProtocol.action).single['session'], '0');
  });

  testWidgets(
    'shows a stale-session refusal as the app said it and reloads the panel',
    (tester) async {
      final fake = await pumpPanel(tester);

      await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
      await tester.pumpAndSettle();
      final reads = fake.callsTo(ForgeDevtoolsProtocol.queries).length;

      // The app switched account; the panel has not heard yet.
      fake.session = 1;
      await tester.tap(find.byKey(const ValueKey('query-action-refetch')));
      await tester.pumpAndSettle();

      expect(fake.actions, isEmpty);
      expect(find.textContaining('the principal changed'), findsOneWidget);
      expect(find.text('Pick a query.'), findsOneWidget);
      expect(
        fake.callsTo(ForgeDevtoolsProtocol.queries).length,
        greaterThan(reads),
      );
    },
  );

  testWidgets('a principal marker drops the open query and re-reads the list', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);

    await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
    await tester.pumpAndSettle();
    expect(find.text('tags (2)'), findsOneWidget);
    final reads = fake.callsTo(ForgeDevtoolsProtocol.queries).length;

    fake.session = 1;
    fake.emit({
      'cache': '1',
      'skipped': 0,
      'entries': [
        {'kind': 'principal', 'seq': 7, 'at': 7, 'session': 1},
      ],
    });
    await tester.pumpAndSettle();

    expect(find.text('tags (2)'), findsNothing);
    expect(find.text('Pick a query.'), findsOneWidget);
    expect(
      fake.callsTo(ForgeDevtoolsProtocol.queries).length,
      greaterThan(reads),
    );
  });

  testWidgets(
    'shows a query the app would not send whole as too large to show',
    (tester) async {
      final fake = FakeForgeBackend();
      fake.overrides[ForgeDevtoolsProtocol.queries] = (_) => {
        'total': 2,
        'offset': 0,
        'truncated': false,
        'items': [
          {'key': 'GET /orders', 'status': 'success'},
          {'oversized': true, 'field': 'key'},
        ],
      };
      await pumpPanel(tester, fake);

      expect(find.byKey(const ValueKey('query-GET /orders')), findsOneWidget);
      expect(
        find.text('too large to show (key is over 64 KB)'),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('query-')), findsNothing);
    },
  );

  testWidgets(
    'an app with no queries yet shows the first one when activity arrives',
    (tester) async {
      final fake = FakeForgeBackend();
      var rows = <Json>[];
      fake.overrides[ForgeDevtoolsProtocol.queries] = (_) => {
        'total': rows.length,
        'offset': 0,
        'truncated': false,
        'items': rows,
      };
      await pumpPanel(tester, fake);
      expect(find.text('0 queries'), findsOneWidget);

      rows = [
        {'key': 'GET /orders', 'status': 'success'},
      ];
      fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
      await tester.pump(const Duration(milliseconds: 600));
      await tester.pumpAndSettle();

      expect(find.text('1 queries'), findsOneWidget);
      expect(find.byKey(const ValueKey('query-GET /orders')), findsOneWidget);
    },
  );

  testWidgets(
    'says switching account for a list and a query the app answered empty for that reason',
    (tester) async {
      final fake = FakeForgeBackend();
      fake.overrides[ForgeDevtoolsProtocol.queries] = (_) => {
        'total': 1,
        'offset': 0,
        'truncated': false,
        'stale': true,
        'items': [
          {'key': 'GET /orders', 'status': 'success'},
        ],
      };
      fake.overrides[ForgeDevtoolsProtocol.query] = (_) => {
        'detail': null,
        'stale': true,
      };
      await pumpPanel(tester, fake);

      expect(find.byKey(const ValueKey('queries-switching')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
      await tester.pumpAndSettle();

      expect(find.text('This query is no longer tracked.'), findsNothing);
      expect(find.text('switching account'), findsNWidgets(2));
    },
  );
}
