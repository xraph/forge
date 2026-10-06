import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

/// The scrollable that belongs to the entity table.
ScrollPosition _table(WidgetTester tester) => tester
    .state<ScrollableState>(
      find.descendant(
        of: find.byKey(const ValueKey('entities-list')),
        matching: find.byType(Scrollable),
      ),
    )
    .position;

/// Entity offsets asked for so far, in order.
List<int> _offsets(FakeForgeBackend fake) => [
  for (final c in fake.callsTo(ForgeDevtoolsProtocol.entities))
    int.parse(c['offset']!),
];

void main() {
  // Review Focus 1, from the extension's side: the panel asks for a hundred
  // rows at a time and never for more than the protocol's page cap.
  testWidgets('pages a 10,000 entity store a hundred rows at a time', (
    tester,
  ) async {
    final fake = await pumpPanel(tester, FakeForgeBackend(entityCount: 10000));
    await openTab(tester, 'Entities');

    expect(find.text('10000 entities'), findsOneWidget);
    expect(fake.callsTo(ForgeDevtoolsProtocol.entities), [
      {'cache': '1', 'offset': '0', 'limit': '100'},
    ]);

    for (var i = 0; i < 4; i++) {
      await tester.drag(
        find.byKey(const ValueKey('entities-list')),
        const Offset(0, -6000),
      );
      await tester.pumpAndSettle();
    }

    final calls = fake.callsTo(ForgeDevtoolsProtocol.entities);

    expect([
      for (final c in calls) c['offset'],
    ], containsAllInOrder(['0', '100', '200']));
    expect(
      calls.every(
        (c) => int.parse(c['limit']!) <= ForgeDevtoolsProtocol.maxPage,
      ),
      isTrue,
    );
    expect(calls.length, lessThan(10));
  });

  testWidgets('filters by type and by substring on submit', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Entities');

    await tester.enterText(
      find.byKey(const ValueKey('entities-type')),
      'Order',
    );
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();

    expect(fake.callsTo(ForgeDevtoolsProtocol.entities).last['type'], 'Order');
    expect(find.text('3 entities'), findsOneWidget);

    await tester.enterText(
      find.byKey(const ValueKey('entities-type')),
      'Invoice',
    );
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();

    expect(find.text('No entities match.'), findsOneWidget);

    await tester.enterText(find.byKey(const ValueKey('entities-filter')), 'c1');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();

    expect(fake.callsTo(ForgeDevtoolsProtocol.entities).last['filter'], 'c1');
  });

  testWidgets(
    'opens an entity with its fields, the folded view, references and dependents',
    (tester) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Entities');

      await tester.tap(find.byKey(const ValueKey('entity-Order:0')));
      await tester.pumpAndSettle();

      expect(fake.callsTo(ForgeDevtoolsProtocol.entity).last['key'], 'Order:0');
      expect(find.text('total: 10'), findsOneWidget);
      expect(find.text('total: 12'), findsOneWidget);
      expect(find.text('customer {1}'), findsOneWidget);
      expect(find.text('Customer:c1'), findsOneWidget);
      expect(find.text('GET /orders'), findsOneWidget);
    },
  );

  testWidgets('evicts the open entity', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Entities');

    await tester.tap(find.byKey(const ValueKey('entity-Order:1')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('entity-evict')));
    await tester.pumpAndSettle();

    expect(fake.actions, ['evict Order:1']);
  });

  testWidgets(
    'does not reload the table on activity, so the scroll position survives',
    (tester) async {
      final fake = await pumpPanel(
        tester,
        FakeForgeBackend(entityCount: 10000),
      );
      await openTab(tester, 'Entities');
      final before = fake.callsTo(ForgeDevtoolsProtocol.entities).length;

      fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
      await tester.pump(const Duration(milliseconds: 600));
      await tester.pumpAndSettle();

      expect(fake.callsTo(ForgeDevtoolsProtocol.entities).length, before);

      await tester.tap(find.byKey(const ValueKey('entities-refresh')));
      await tester.pumpAndSettle();

      expect(fake.callsTo(ForgeDevtoolsProtocol.entities).length, before + 1);
    },
  );

  group('a store of 10,000', () {
    testWidgets(
      'a scroll to the far end reads the pages there and none between',
      (tester) async {
        final fake = await pumpPanel(
          tester,
          FakeForgeBackend(entityCount: 10000),
        );
        await openTab(tester, 'Entities');

        _table(tester).jumpTo(9000 * 64.0);
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('entity-Order:9000')), findsOneWidget);
        // Row 9,000 starts page 90. The page before it may be read for the
        // rows just above the fold. Nothing in between is.
        expect(_offsets(fake).where((o) => o > 100 && o < 8800), isEmpty);
        expect(_offsets(fake).length, lessThan(6));
      },
    );

    testWidgets(
      'holds a window of pages, so memory follows the window and not the store',
      (tester) async {
        final fake = await pumpPanel(
          tester,
          FakeForgeBackend(entityCount: 10000),
        );
        await openTab(tester, 'Entities');

        // Walk down fifteen pages, then back to the top.
        for (var page = 1; page <= 15; page++) {
          _table(tester).jumpTo(page * 100 * 64.0);
          await tester.pumpAndSettle();
        }
        final deepest = _offsets(fake).length;
        expect(deepest, lessThanOrEqualTo(40));

        _table(tester).jumpTo(0);
        await tester.pumpAndSettle();

        // Page 0 was let go on the way down, so it is read again. Were every
        // page kept, nothing would be asked for here.
        expect(_offsets(fake).length, greaterThan(deepest));
        expect(_offsets(fake).last, 0);
        expect(find.byKey(const ValueKey('entity-Order:0')), findsOneWidget);
      },
    );

    testWidgets('refresh re-reads only the pages in the window, in place', (
      tester,
    ) async {
      final fake = await pumpPanel(
        tester,
        FakeForgeBackend(entityCount: 10000),
      );
      await openTab(tester, 'Entities');

      for (var page = 1; page <= 15; page++) {
        _table(tester).jumpTo(page * 100 * 64.0);
        await tester.pumpAndSettle();
      }
      final offset = _table(tester).pixels;
      final before = fake.callsTo(ForgeDevtoolsProtocol.entities).length;

      await tester.tap(find.byKey(const ValueKey('entities-refresh')));
      await tester.pumpAndSettle();

      final reread =
          fake.callsTo(ForgeDevtoolsProtocol.entities).length - before;
      expect(reread, inInclusiveRange(1, 6));
      expect(_table(tester).pixels, offset);
      expect(find.text('Loading...'), findsNothing);
    });
  });

  group('identifiers', () {
    testWidgets(
      'go back to the app verbatim, and a long key is cut in the row only',
      (tester) async {
        final long = 'Order:${'k' * 5000}';
        const odd = 'Order:a b/{x}|"q"é';
        final fake = FakeForgeBackend();
        fake.overrides[ForgeDevtoolsProtocol.entities] = (_) => {
          'total': 2,
          'offset': 0,
          'truncated': false,
          'items': [
            {'key': long, 'version': 1, 'frameAt': 0, 'refCount': 0},
            {'key': odd, 'version': 1, 'frameAt': 0, 'refCount': 0},
          ],
        };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Entities');

        await tester.tap(find.byKey(ValueKey('entity-$long')));
        await tester.pumpAndSettle();
        expect(fake.callsTo(ForgeDevtoolsProtocol.entity).last['key'], long);

        await tester.tap(find.byKey(const ValueKey('entity-$odd')));
        await tester.pumpAndSettle();
        expect(fake.callsTo(ForgeDevtoolsProtocol.entity).last['key'], odd);

        await tester.tap(find.byKey(const ValueKey('entity-evict')));
        await tester.pumpAndSettle();
        expect(
          fake.callsTo(ForgeDevtoolsProtocol.action).single['target'],
          odd,
        );
        expect(tester.takeException(), isNull);
      },
    );

    testWidgets(
      'a row the app would not send whole shows as too large, in the list',
      (tester) async {
        final fake = FakeForgeBackend();
        fake.overrides[ForgeDevtoolsProtocol.entities] = (_) => {
          'total': 2,
          'offset': 0,
          'truncated': false,
          'items': [
            {'key': 'Order:1', 'version': 1, 'frameAt': 0, 'refCount': 0},
            {'oversized': true, 'field': 'key'},
          ],
        };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Entities');

        expect(find.byKey(const ValueKey('entity-Order:1')), findsOneWidget);
        expect(
          find.text('too large to show (key is over 64 KB)'),
          findsOneWidget,
        );
      },
    );

    testWidgets(
      'an entity the app would not send whole shows as too large, in the detail',
      (tester) async {
        final fake = FakeForgeBackend();
        fake.overrides[ForgeDevtoolsProtocol.entity] = (_) => {
          'entity': {'oversized': true, 'field': 'key'},
          'folded': null,
        };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Entities');

        await tester.tap(find.byKey(const ValueKey('entity-Order:0')));
        await tester.pumpAndSettle();

        expect(
          find.text('too large to show (key is over 64 KB)'),
          findsOneWidget,
        );
        expect(find.byKey(const ValueKey('entity-evict')), findsNothing);
      },
    );
  });

  group('actions', () {
    testWidgets('send the session the panel last read', (tester) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Entities');

      await tester.tap(find.byKey(const ValueKey('entity-Order:1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('entity-evict')));
      await tester.pumpAndSettle();

      final action = fake.callsTo(ForgeDevtoolsProtocol.action).single;
      expect(action['session'], '0');
      expect(action['action'], 'evict');
      expect(action['target'], 'Order:1');
    });

    testWidgets('show the refusal for a sync-owned entity as the app said it', (
      tester,
    ) async {
      final fake = FakeForgeBackend();
      const refusal =
          '[forge] cannot evict Doc:7 from the devtools: a sync source owns '
          'Doc, and only that source may change its records';
      fake.overrides[ForgeDevtoolsProtocol.action] = (_) =>
          throw const BackendError(ForgeDevtoolsProtocol.action, refusal);
      await pumpPanel(tester, fake);
      await openTab(tester, 'Entities');
      final reads = fake.callsTo(ForgeDevtoolsProtocol.entities).length;

      await tester.tap(find.byKey(const ValueKey('entity-Order:1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('entity-evict')));
      await tester.pumpAndSettle();

      expect(find.text(refusal), findsOneWidget);
      // Nothing changed, so the open entity stays and the table is not re-read.
      expect(find.byKey(const ValueKey('entity-evict')), findsOneWidget);
      expect(fake.callsTo(ForgeDevtoolsProtocol.entities).length, reads);
    });

    testWidgets('show a stale-session refusal and reload the panel', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Entities');

      await tester.tap(find.byKey(const ValueKey('entity-Order:1')));
      await tester.pumpAndSettle();

      // The app switched account; the panel has not heard yet.
      fake.session = 1;
      await tester.tap(find.byKey(const ValueKey('entity-evict')));
      await tester.pumpAndSettle();

      expect(fake.actions, isEmpty);
      expect(find.textContaining('the principal changed'), findsOneWidget);
      expect(find.text('Pick an entity.'), findsOneWidget);
    });

    testWidgets('a successful evict re-reads the table in place', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Entities');
      await tester.tap(find.byKey(const ValueKey('entity-Order:1')));
      await tester.pumpAndSettle();
      final reads = fake.callsTo(ForgeDevtoolsProtocol.entities).length;

      await tester.tap(find.byKey(const ValueKey('entity-evict')));
      await tester.pumpAndSettle();

      expect(fake.callsTo(ForgeDevtoolsProtocol.entities).length, reads + 1);
    });

    testWidgets('say when the store no longer holds the entity', (
      tester,
    ) async {
      final fake = FakeForgeBackend();
      fake.overrides[ForgeDevtoolsProtocol.entity] = (_) => {
        'entity': null,
        'folded': null,
      };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Entities');

      await tester.tap(find.byKey(const ValueKey('entity-Order:0')));
      await tester.pumpAndSettle();

      expect(
        find.text('The store no longer holds this entity.'),
        findsOneWidget,
      );
    });
  });

  group('while the app changes account', () {
    testWidgets('a stale list says so, and a stale entity says so', (
      tester,
    ) async {
      final fake = FakeForgeBackend();
      fake.overrides[ForgeDevtoolsProtocol.entities] = (_) => {
        'total': 1,
        'offset': 0,
        'truncated': false,
        'stale': true,
        'items': [
          {'key': 'Order:0', 'version': 1, 'frameAt': 0, 'refCount': 0},
        ],
      };
      fake.overrides[ForgeDevtoolsProtocol.entity] = (_) => {
        'entity': null,
        'folded': null,
        'stale': true,
      };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Entities');

      expect(find.byKey(const ValueKey('entities-switching')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('entity-Order:0')));
      await tester.pumpAndSettle();

      // The detail pane, not only the status bar.
      expect(find.text('switching account'), findsNWidgets(2));
    });
  });

  group('privacy', () {
    testWidgets(
      'a principal marker clears the table, the open entity and every cached page',
      (tester) async {
        final fake = await pumpPanel(
          tester,
          FakeForgeBackend(entityCount: 250),
        );
        await openTab(tester, 'Entities');
        _table(tester).jumpTo(150 * 64.0);
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const ValueKey('entity-Order:155')));
        await tester.pumpAndSettle();
        expect(find.text('total: 10'), findsOneWidget);
        final reads = fake.callsTo(ForgeDevtoolsProtocol.entities).length;

        // Bob's store holds different records. Hold the answer so the moment
        // after the marker is visible.
        fake.session = 1;
        fake.entityCount = 5;
        fake.emit({
          'cache': '1',
          'skipped': 0,
          'entries': [
            {'kind': 'principal', 'seq': 7, 'at': 7, 'session': 1},
          ],
        });
        await tester.pumpAndSettle();

        expect(find.text('Pick an entity.'), findsOneWidget);
        expect(find.text('total: 10'), findsNothing);
        expect(find.byKey(const ValueKey('entity-Order:155')), findsNothing);
        expect(find.text('5 entities'), findsOneWidget);
        // Read again from the first page, not from a cache.
        expect(
          fake.callsTo(ForgeDevtoolsProtocol.entities).length,
          greaterThan(reads),
        );
        expect(_offsets(fake).last, 0);
        expect(_table(tester).pixels, 0);
      },
    );

    testWidgets('an isolate change clears the panel as well', (tester) async {
      final fake = await pumpPanel(tester, FakeForgeBackend(entityCount: 250));
      await openTab(tester, 'Entities');
      await tester.tap(find.byKey(const ValueKey('entity-Order:1')));
      await tester.pumpAndSettle();
      expect(find.text('total: 10'), findsOneWidget);

      fake.entityCount = 2;
      fake.swapIsolate();
      await tester.pumpAndSettle();

      expect(find.text('total: 10'), findsNothing);
      expect(find.text('2 entities'), findsOneWidget);
      expect(find.byKey(const ValueKey('entity-Order:1')), findsOneWidget);
      expect(find.text('Pick an entity.'), findsOneWidget);
    });
  });
}
