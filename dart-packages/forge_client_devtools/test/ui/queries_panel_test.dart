import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';

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
}
