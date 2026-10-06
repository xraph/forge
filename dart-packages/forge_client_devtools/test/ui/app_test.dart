import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';
import 'package:forge_client_devtools/src/backend/isolate_watch.dart';

import '../support/fake_backend.dart';
import '../support/fake_hooks.dart';
import '../support/pump.dart';

void main() {
  // Review Focus 4.
  testWidgets(
    'explains what to check when the app has no forge_client, and calls nothing',
    (tester) async {
      final fake = await pumpPanel(tester, FakeForgeBackend(available: false));

      expect(find.byKey(const ValueKey('forge-unavailable')), findsOneWidget);
      expect(find.textContaining('release build'), findsOneWidget);
      expect(find.textContaining('forge.devtools=false'), findsOneWidget);
      expect(
        find.textContaining('registerForgeServiceExtensions'),
        findsOneWidget,
      );
      expect(fake.calls, isEmpty);
    },
  );

  testWidgets(
    'connects by itself when the extension appears later, as after a hot restart',
    (tester) async {
      final fake = await pumpPanel(tester, FakeForgeBackend(available: false));

      fake.isAvailable = true;
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('forge-unavailable')), findsNothing);
      expect(find.widgetWithText(Tab, 'Queries'), findsOneWidget);
      expect(fake.callsTo(ForgeDevtoolsProtocol.hello), hasLength(1));
    },
  );

  testWidgets('drops back to the empty state when the app goes away', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);

    fake.isAvailable = false;
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('forge-unavailable')), findsOneWidget);
  });

  testWidgets(
    'refuses a protocol it does not speak, and says to update both packages',
    (tester) async {
      await pumpPanel(
        tester,
        FakeForgeBackend(protocol: ForgeDevtoolsProtocol.version + 1),
      );

      expect(
        find.textContaining(
          'Update forge_client and forge_client_devtools together',
        ),
        findsOneWidget,
      );
      expect(find.widgetWithText(Tab, 'Queries'), findsNothing);
    },
  );

  testWidgets('says so when no cache is attached', (tester) async {
    await pumpPanel(tester, FakeForgeBackend()..caches = []);

    expect(find.textContaining('No cache is attached'), findsOneWidget);
  });

  testWidgets('shows a failed hello with a retry that recovers', (
    tester,
  ) async {
    final fake = FakeForgeBackend();
    fake.overrides[ForgeDevtoolsProtocol.hello] = (_) =>
        throw const BackendError('ext.forge.hello', 'isolate paused');
    await pumpPanel(tester, fake);

    expect(find.textContaining('isolate paused'), findsOneWidget);

    fake.overrides.clear();
    await tester.tap(find.widgetWithText(TextButton, 'Retry'));
    await tester.pumpAndSettle();

    expect(find.widgetWithText(Tab, 'Queries'), findsOneWidget);
  });

  testWidgets(
    'lets you pick between caches, and scopes every later call to the one picked',
    (tester) async {
      final fake = FakeForgeBackend()
        ..caches = [
          {'id': '1', 'label': 'cache 1'},
          {'id': '2', 'label': 'cache 2'},
        ];
      await pumpPanel(tester, fake);

      await tester.tap(find.byKey(const ValueKey('cache-picker')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('cache 1').last);
      await tester.pumpAndSettle();

      fake.calls.clear();
      await tester.tap(find.byKey(const ValueKey('status-refresh')));
      await tester.pumpAndSettle();

      expect(fake.calls, isNotEmpty);
      expect(fake.calls.every((c) => c.params['cache'] == '1'), isTrue);
    },
  );

  testWidgets('shows the status buckets from the snapshot', (tester) async {
    await pumpPanel(tester);

    final bar = find.byKey(const ValueKey('status-bar'));
    expect(
      find.descendant(of: bar, matching: find.text('success 1')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: bar, matching: find.text('stale 1')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: bar, matching: find.text('records 3')),
      findsOneWidget,
    );
  });

  testWidgets(
    'says the app is switching account instead of showing a stale snapshot as zeros',
    (tester) async {
      await pumpPanel(tester, FakeForgeBackend()..switching = true);

      final bar = find.byKey(const ValueKey('status-bar'));
      expect(
        find.descendant(of: bar, matching: find.text('switching account')),
        findsOneWidget,
      );
      expect(
        find.descendant(of: bar, matching: find.text('records 0')),
        findsNothing,
      );
      expect(
        find.descendant(of: bar, matching: find.text('success 0')),
        findsNothing,
      );
    },
  );

  testWidgets(
    'reads the status again from scratch after the principal changes',
    (tester) async {
      final fake = await pumpPanel(
        tester,
        FakeForgeBackend()..switching = true,
      );
      expect(find.text('switching account'), findsOneWidget);

      fake
        ..switching = false
        ..session = 1;
      fake.emit({
        'cache': '1',
        'skipped': 0,
        'entries': [
          {'kind': 'principal', 'seq': 1, 'at': 1, 'session': 1},
        ],
      });
      await tester.pumpAndSettle();

      final bar = find.byKey(const ValueKey('status-bar'));
      expect(find.text('switching account'), findsNothing);
      expect(
        find.descendant(of: bar, matching: find.text('records 3')),
        findsOneWidget,
      );
    },
  );

  // Final review I2: cache 1 disposed and cache 2 built in the same isolate.
  // The panel moves to cache 2, shows nothing it read from cache 1, and shows
  // cache 2's events.
  group('a cache replaced in the same isolate', () {
    Future<void> openEvents(WidgetTester tester) async {
      await tester.tap(find.widgetWithText(Tab, 'Events'));
      await tester.pumpAndSettle();
    }

    Json mutation(String cache, String operation) => {
      'cache': cache,
      'skipped': 0,
      'entries': [
        {
          'kind': 'mutation',
          'seq': 9,
          'at': 9,
          'session': 0,
          'operation': operation,
          'tags': <String>[],
          'unresolved': <String>[],
        },
      ],
    };

    void replace(FakeForgeBackend fake) => fake
      ..gone.add('1')
      ..caches = [
        {'id': '2', 'label': 'cache 2'},
      ];

    testWidgets(
      'on the lifecycle events, the panel moves to the new cache and keeps nothing of the old',
      (tester) async {
        final fake = await pumpPanel(tester);
        await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
        await tester.pumpAndSettle();
        expect(find.text('tags (2)'), findsOneWidget);
        await openEvents(tester);
        fake.emit(mutation('1', 'alice-op'));
        await tester.pumpAndSettle();
        expect(find.textContaining('alice-op'), findsOneWidget);

        replace(fake);
        final before = fake.calls.length;
        fake.emitLifecycle('1', ForgeDevtoolsProtocol.detached);
        fake.emitLifecycle('2', ForgeDevtoolsProtocol.attached);
        await tester.pumpAndSettle();

        expect(find.textContaining('alice-op'), findsNothing);
        final after = fake.calls.skip(before).toList();
        expect(
          after.where((c) => c.method == ForgeDevtoolsProtocol.hello),
          isNotEmpty,
        );
        expect(
          after
              .where((c) => c.method != ForgeDevtoolsProtocol.hello)
              .every((c) => c.params['cache'] == '2'),
          isTrue,
        );

        fake.emit(mutation('2', 'bob-op'));
        await tester.pumpAndSettle();
        await openEvents(tester);

        expect(find.textContaining('bob-op'), findsOneWidget);
        expect(find.textContaining('alice-op'), findsNothing);

        await tester.tap(find.widgetWithText(Tab, 'Queries'));
        await tester.pumpAndSettle();
        expect(find.text('tags (2)'), findsNothing);
        expect(find.text('Pick a query.'), findsOneWidget);
      },
    );

    testWidgets('with no lifecycle event, a refused read is enough to move', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openEvents(tester);
      fake.emit(mutation('1', 'alice-op'));
      await tester.pumpAndSettle();

      replace(fake);
      await tester.tap(find.byKey(const ValueKey('status-refresh')));
      await tester.pumpAndSettle();

      expect(fake.callsTo(ForgeDevtoolsProtocol.hello), hasLength(2));
      fake.emit(mutation('2', 'bob-op'));
      await tester.pumpAndSettle();
      await openEvents(tester);

      expect(find.textContaining('alice-op'), findsNothing);
      expect(find.textContaining('bob-op'), findsOneWidget);
    });
  });

  group('isolate changes, through the watch DevTools uses', () {
    (FakeServiceHooks, FakeForgeBackend) setUpRestart() {
      final hooks = FakeServiceHooks(registered: {ForgeDevtoolsProtocol.hello});
      final watch = IsolateWatch(hooks, ForgeDevtoolsProtocol.hello);
      addTearDown(watch.dispose);
      final fake = FakeForgeBackend(watch: watch)
        ..caches = [
          {'id': '4', 'label': 'cache 4'},
        ]
        ..session = 3;
      return (hooks, fake);
    }

    Future<void> openQuery(WidgetTester tester) async {
      await tester.tap(find.byKey(const ValueKey('query-GET /orders')));
      await tester.pumpAndSettle();
      expect(find.text('tags (2)'), findsOneWidget);
    }

    void becomeAnotherApp(FakeForgeBackend fake) {
      fake
        ..caches = [
          {'id': '1', 'label': 'cache 1'},
        ]
        ..session = 0;
    }

    testWidgets(
      'recovers to running after a hot restart and keeps nothing from the run before',
      (tester) async {
        final (hooks, fake) = setUpRestart();
        await pumpPanel(tester, fake);
        await openQuery(tester);

        hooks.closeIsolate();
        await tester.pumpAndSettle();
        expect(find.byKey(const ValueKey('forge-unavailable')), findsOneWidget);

        becomeAnotherApp(fake);
        final before = fake.calls.length;
        hooks.openIsolate('isolate-2');
        await tester.pumpAndSettle();
        hooks.register(ForgeDevtoolsProtocol.hello);
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('forge-unavailable')), findsNothing);
        expect(find.widgetWithText(Tab, 'Queries'), findsOneWidget);
        expect(find.text('Pick a query.'), findsOneWidget);
        final after = fake.calls.skip(before).toList();
        expect(
          after.where((c) => c.method == ForgeDevtoolsProtocol.hello),
          isNotEmpty,
        );
        expect(
          after.where((c) => c.method == ForgeDevtoolsProtocol.snapshot),
          isNotEmpty,
        );
        expect(after.any((c) => c.params['cache'] == '4'), isFalse);
      },
    );

    testWidgets('an isolate swap without a close drops what the panel showed', (
      tester,
    ) async {
      final (hooks, fake) = setUpRestart();
      await pumpPanel(tester, fake);
      await openQuery(tester);

      becomeAnotherApp(fake);
      final before = fake.calls.length;
      hooks.replaceIsolate('isolate-2');
      await tester.pumpAndSettle();

      expect(find.text('tags (2)'), findsNothing);
      expect(find.text('Pick a query.'), findsOneWidget);
      final after = fake.calls.skip(before).toList();
      expect(
        after.where((c) => c.method == ForgeDevtoolsProtocol.hello),
        hasLength(1),
      );
      expect(after.any((c) => c.params['cache'] == '4'), isFalse);

      // Acting now aims at the new isolate's session.
      await openQuery(tester);
      await tester.tap(find.byKey(const ValueKey('query-action-invalidate')));
      await tester.pumpAndSettle();
      expect(fake.actions, ['invalidate GET /orders']);
      expect(fake.callsTo(ForgeDevtoolsProtocol.action).single['session'], '0');
    });
  });
}
