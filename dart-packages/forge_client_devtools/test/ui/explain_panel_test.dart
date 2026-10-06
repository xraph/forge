import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

void main() {
  testWidgets(
    'lists tags with how many queries carry each and how many an invalidation reaches',
    (tester) async {
      await pumpPanel(tester);
      await openTab(tester, 'Tags');

      expect(find.byKey(const ValueKey('tag-Order[]')), findsOneWidget);
      expect(find.text('carried by 1, mounted 1'), findsOneWidget);
      expect(find.text('carried by 1, mounted 0'), findsOneWidget);
    },
  );

  testWidgets('invalidates a tag by hand', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Tags');

    await tester.tap(find.byKey(const ValueKey('tag-Order[]')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('tag-invalidate')));
    await tester.pumpAndSettle();

    expect(fake.actions, ['invalidateTag Order[]']);
    expect(fake.callsTo(ForgeDevtoolsProtocol.action).single['session'], '0');
  });

  testWidgets(
    'explains why a query did not refetch, naming the near miss and the fix',
    (tester) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      await tester.enterText(
        find.byKey(const ValueKey('explain-key')),
        'GET /orders',
      );
      await tester.tap(find.byKey(const ValueKey('explain-question')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('why not refetched').last);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('explain-run')));
      await tester.pumpAndSettle();

      final call = fake.callsTo(ForgeDevtoolsProtocol.explain).single;
      expect(call['key'], 'GET /orders');
      expect(call['question'], 'whyNotRefetched');
      expect(find.byKey(const ValueKey('report-outcome')), findsOneWidget);
      expect(find.text('missed'), findsOneWidget);
      expect(find.text('Order:9 vs Order[]'), findsOneWidget);
      expect(
        find.textContaining("Add `Order[]` to the operation's Invalidates"),
        findsWidgets,
      );
      expect(find.textContaining('disjoint'), findsOneWidget);
    },
  );

  testWidgets(
    'previews what an operation would invalidate without running it',
    (tester) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      await tester.enterText(
        find.byKey(const ValueKey('would-response')),
        '{"id":9}',
      );
      await tester.tap(find.byKey(const ValueKey('would-run')));
      await tester.pumpAndSettle();

      final call = fake.callsTo(ForgeDevtoolsProtocol.wouldInvalidate).single;
      expect(call['operation'], 'op_order_create');
      expect(call['response'], '{"id":9}');
      expect(find.text('missed: Order:9'), findsOneWidget);
      expect(fake.actions, isEmpty);
    },
  );

  testWidgets('refuses malformed JSON before calling the app', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Tags');

    await tester.enterText(
      find.byKey(const ValueKey('would-response')),
      '{nope',
    );
    await tester.tap(find.byKey(const ValueKey('would-run')));
    await tester.pumpAndSettle();

    expect(find.text('response is not valid JSON'), findsOneWidget);
    expect(fake.callsTo(ForgeDevtoolsProtocol.wouldInvalidate), isEmpty);
  });

  group('the list', () {
    testWidgets('filters by tag on submit', (tester) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      await tester.enterText(find.byKey(const ValueKey('tags-filter')), '[]');
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();

      expect(fake.callsTo(ForgeDevtoolsProtocol.tags).last['filter'], '[]');
      expect(find.byKey(const ValueKey('tag-Order:1')), findsNothing);
      expect(find.byKey(const ValueKey('tag-Order[]')), findsOneWidget);
      expect(find.text('1 tags'), findsOneWidget);
    });

    testWidgets('re-reads in place when the app reports activity', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');
      final before = fake.callsTo(ForgeDevtoolsProtocol.tags).length;

      fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
      await tester.pump(const Duration(milliseconds: 600));
      await tester.pumpAndSettle();

      expect(
        fake.callsTo(ForgeDevtoolsProtocol.tags).length,
        greaterThan(before),
      );
      expect(find.byKey(const ValueKey('tag-Order[]')), findsOneWidget);
      expect(find.text('2 tags'), findsOneWidget);
    });

    testWidgets('keeps its rows on screen while a refresh is on its way', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      // The next read waits, as a slow app would make it.
      final gate = Completer<void>();
      fake.overrides[ForgeDevtoolsProtocol.tags] = (params) async {
        await gate.future;
        return {
          'total': 2,
          'offset': 0,
          'truncated': false,
          'items': [
            {
              'tag': 'Order[]',
              'carriers': ['GET /orders'],
              'carriersTotal': 1,
              'mounted': ['GET /orders'],
              'mountedTotal': 1,
            },
            {
              'tag': 'Order:1',
              'carriers': ['GET /orders'],
              'carriersTotal': 1,
              'mounted': <String>[],
              'mountedTotal': 0,
            },
          ],
        };
      };

      fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
      await tester.pump(const Duration(milliseconds: 600));

      expect(find.byKey(const ValueKey('tag-Order[]')), findsOneWidget);
      expect(find.text('Loading...'), findsNothing);

      gate.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('tag-Order[]')), findsOneWidget);
    });

    testWidgets('counts with the totals the app sends, not the capped lists', (
      tester,
    ) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.tags] = (params) => {
          'total': 1,
          'offset': 0,
          'truncated': false,
          'items': [
            {
              'tag': 'Order[]',
              'carriers': ['GET /orders'],
              'carriersTotal': 1500,
              'mounted': <String>[],
              'mountedTotal': 12,
            },
          ],
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      expect(find.text('carried by 1500, mounted 12'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('tag-Order[]')));
      await tester.pumpAndSettle();

      expect(find.text('showing the first 1'), findsOneWidget);
    });

    testWidgets(
      'shows the selected tag from a fresh read, and follows activity',
      (tester) async {
        var carriers = ['GET /orders'];
        final fake = FakeForgeBackend()
          ..overrides[ForgeDevtoolsProtocol.tags] = (params) => {
            'total': 1,
            'offset': 0,
            'truncated': false,
            'items': [
              {
                'tag': 'Order[]',
                'carriers': carriers,
                'carriersTotal': carriers.length,
                'mounted': <String>[],
                'mountedTotal': 0,
              },
            ],
          };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Tags');

        await tester.tap(find.byKey(const ValueKey('tag-Order[]')));
        await tester.pumpAndSettle();

        // The detail asked for exactly this tag, as the app gave it.
        expect(
          fake.callsTo(ForgeDevtoolsProtocol.tags).last['filter'],
          'Order[]',
        );
        expect(find.text('carried by: GET /orders'), findsOneWidget);

        carriers = ['GET /orders', 'GET /orders/{id}'];
        fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
        await tester.pump(const Duration(milliseconds: 600));
        await tester.pumpAndSettle();

        expect(
          find.text('carried by: GET /orders, GET /orders/{id}'),
          findsOneWidget,
        );
      },
    );

    testWidgets('says so when no query carries the selected tag any more', (
      tester,
    ) async {
      var gone = false;
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.tags] = (params) => {
          'total': gone ? 0 : 1,
          'offset': 0,
          'truncated': false,
          'items': gone
              ? <Object?>[]
              : [
                  {
                    'tag': 'Order[]',
                    'carriers': ['GET /orders'],
                    'carriersTotal': 1,
                    'mounted': <String>[],
                    'mountedTotal': 0,
                  },
                ],
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');
      await tester.tap(find.byKey(const ValueKey('tag-Order[]')));
      await tester.pumpAndSettle();

      gone = true;
      fake.emit({'cache': '1', 'entries': <Object?>[], 'skipped': 0});
      await tester.pump(const Duration(milliseconds: 600));
      await tester.pumpAndSettle();

      expect(find.text('No query carries this tag any more.'), findsOneWidget);
    });

    testWidgets('shows a refusal of the invalidate as the app said it', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');
      await tester.tap(find.byKey(const ValueKey('tag-Order[]')));
      await tester.pumpAndSettle();

      // The app switched account; the panel has not heard yet.
      fake.session = 1;
      await tester.tap(find.byKey(const ValueKey('tag-invalidate')));
      await tester.pumpAndSettle();

      expect(fake.actions, isEmpty);
      expect(find.textContaining('the principal changed'), findsOneWidget);
    });

    testWidgets(
      'a principal marker drops the open tag, the report and the preview',
      (tester) async {
        final fake = await pumpPanel(tester);
        await openTab(tester, 'Tags');

        await tester.tap(find.byKey(const ValueKey('tag-Order[]')));
        await tester.pumpAndSettle();
        await tester.enterText(
          find.byKey(const ValueKey('explain-key')),
          'GET /orders',
        );
        await tester.tap(find.byKey(const ValueKey('explain-run')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const ValueKey('would-run')));
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('tag-invalidate')), findsOneWidget);
        expect(find.byKey(const ValueKey('report-outcome')), findsOneWidget);
        expect(find.text('missed: Order:9'), findsOneWidget);

        fake.session = 1;
        fake.emit({
          'cache': '1',
          'skipped': 0,
          'entries': [
            {'kind': 'principal', 'seq': 7, 'at': 7, 'session': 1},
          ],
        });
        await tester.pump(const Duration(milliseconds: 600));
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('tag-invalidate')), findsNothing);
        expect(find.byKey(const ValueKey('report-outcome')), findsNothing);
        expect(find.text('missed: Order:9'), findsNothing);
        expect(find.textContaining('Order:9'), findsNothing);
      },
    );
  });

  group('explain', () {
    testWidgets('sends the key as typed and the first question by default', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      const key = ' GET /orders/{id}|{"path":{"id":"a b"}} ';
      await tester.enterText(find.byKey(const ValueKey('explain-key')), key);
      await tester.tap(find.byKey(const ValueKey('explain-run')));
      await tester.pumpAndSettle();

      final call = fake.callsTo(ForgeDevtoolsProtocol.explain).single;
      expect(call['key'], key);
      expect(call['question'], 'explain');
    });

    testWidgets('asks for a key instead of calling the app with none', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      await tester.tap(find.byKey(const ValueKey('explain-run')));
      await tester.pumpAndSettle();

      expect(find.text('enter a query key'), findsOneWidget);
      expect(fake.callsTo(ForgeDevtoolsProtocol.explain), isEmpty);
    });

    testWidgets(
      'says the log holds no request when why-refetched has nothing',
      (tester) async {
        final fake = FakeForgeBackend()
          ..overrides[ForgeDevtoolsProtocol.explain] = (params) => {
            'report': null,
          };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Tags');

        await tester.enterText(
          find.byKey(const ValueKey('explain-key')),
          'GET /orders',
        );
        await tester.tap(find.byKey(const ValueKey('explain-run')));
        await tester.pumpAndSettle();

        expect(
          find.text('The log holds no request for this query.'),
          findsOneWidget,
        );
      },
    );

    testWidgets('shows why a query refetched, with its cause', (tester) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.explain] = (params) => {
          'report': {
            'kind': 'refetch',
            'query': params['key'],
            'at': 5,
            'reason': 'mutation',
            'cause': {
              'label': 'mutation POST /orders',
              'seq': 4,
              'tags': capped(['Order[]']),
              'unresolved': capped(<String>[]),
            },
            'matched': capped(['Order[]']),
            'summary': 'It refetched because POST /orders raised Order[].',
          },
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      await tester.enterText(
        find.byKey(const ValueKey('explain-key')),
        'GET /orders',
      );
      await tester.tap(find.byKey(const ValueKey('explain-run')));
      await tester.pumpAndSettle();

      expect(find.text('mutation'), findsOneWidget);
      expect(
        find.text('It refetched because POST /orders raised Order[].'),
        findsOneWidget,
      );
      expect(find.text('cause: mutation POST /orders'), findsOneWidget);
      expect(find.text('matched: Order[]'), findsOneWidget);
    });

    testWidgets('shows the app\'s refusal as it said it', (tester) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.explain] = (params) =>
            throw const BackendError(
              ForgeDevtoolsProtocol.explain,
              'cause must be an object',
            );
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      await tester.enterText(
        find.byKey(const ValueKey('explain-key')),
        'GET /orders',
      );
      await tester.tap(find.byKey(const ValueKey('explain-run')));
      await tester.pumpAndSettle();

      expect(find.textContaining('cause must be an object'), findsOneWidget);
    });
  });

  group('would invalidate', () {
    testWidgets('says how to get operations when the app knows none', (
      tester,
    ) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.operations] = (params) => {
          'operations': <Object?>[],
          'total': 0,
          'truncated': false,
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      expect(
        find.textContaining('registerForgeServiceExtensions'),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('would-operation')), findsNothing);
    });

    testWidgets('says how many operations it is not showing', (tester) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.operations] = (params) => {
          'operations': [
            {
              'id': 'op_order_create',
              'method': 'POST',
              'path': '/orders',
              'provides': <String>[],
              'invalidates': <String>[],
            },
          ],
          'total': 2500,
          'truncated': true,
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      expect(
        find.text('showing the first 1 of 2500 operations'),
        findsOneWidget,
      );
    });

    testWidgets('refuses malformed args JSON before calling the app', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      await tester.enterText(find.byKey(const ValueKey('would-args')), '{nope');
      await tester.tap(find.byKey(const ValueKey('would-run')));
      await tester.pumpAndSettle();

      expect(find.text('args is not valid JSON'), findsOneWidget);
      expect(fake.callsTo(ForgeDevtoolsProtocol.wouldInvalidate), isEmpty);
    });

    testWidgets(
      'sends the operation id and the args as typed, and lists what each tag reaches',
      (tester) async {
        final fake = FakeForgeBackend()
          ..overrides[ForgeDevtoolsProtocol.wouldInvalidate] = (params) => {
            'preview': {
              'operation': 'POST /orders',
              'templates': ['Order:{res.id}', 'Order[]'],
              'tags': capped(['Order:9', 'Order[]']),
              'unresolved': capped(<String>[]),
              'hits': capped([
                {'tag': 'Order:9', 'queries': capped(<String>[])},
                {
                  'tag': 'Order[]',
                  'queries': capped(['GET /orders']),
                },
              ]),
              'missed': capped(['Order:9']),
            },
          };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Tags');

        await tester.enterText(
          find.byKey(const ValueKey('would-args')),
          '{"path":{"id":"9"}}',
        );
        await tester.tap(find.byKey(const ValueKey('would-run')));
        await tester.pumpAndSettle();

        final call = fake.callsTo(ForgeDevtoolsProtocol.wouldInvalidate).single;
        expect(call['args'], '{"path":{"id":"9"}}');
        expect(call.containsKey('response'), isFalse);
        expect(find.text('Order:9 reaches nothing mounted'), findsOneWidget);
        expect(find.text('Order[] reaches GET /orders'), findsOneWidget);
      },
    );

    testWidgets('shows the app\'s refusal and drops the last preview', (
      tester,
    ) async {
      final fake = await pumpPanel(tester);
      await openTab(tester, 'Tags');

      await tester.tap(find.byKey(const ValueKey('would-run')));
      await tester.pumpAndSettle();
      expect(find.text('missed: Order:9'), findsOneWidget);

      fake.overrides[ForgeDevtoolsProtocol.wouldInvalidate] = (params) =>
          throw const BackendError(
            ForgeDevtoolsProtocol.wouldInvalidate,
            'args must be a JSON object',
          );
      await tester.enterText(find.byKey(const ValueKey('would-args')), '[1]');
      await tester.tap(find.byKey(const ValueKey('would-run')));
      await tester.pumpAndSettle();

      expect(find.textContaining('args must be a JSON object'), findsOneWidget);
      expect(find.text('missed: Order:9'), findsNothing);
    });
  });

  // Final fix M2: the lists arrive capped, and the panel says how many of how
  // many it shows.
  group('capped lists', () {
    testWidgets('a report says how many of each list it is showing', (
      tester,
    ) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.explain] = (params) => {
          'report': {
            'kind': 'miss',
            'query': params['key'],
            'outcome': 'missed',
            'reason': 'disjoint',
            'cause': {
              'label': 'what if',
              'seq': null,
              'tags': capped(['Order:0'], total: 1500),
              'unresolved': capped(<String>[]),
            },
            'mounts': 1,
            'settled': true,
            'invalidated': capped(['Order:0', 'Order:1'], total: 1500),
            'carried': capped(['Order[]']),
            'matched': capped(<String>[]),
            'nearest': capped(<Object?>[]),
            'suggestions': capped(<String>[]),
          },
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      await tester.enterText(
        find.byKey(const ValueKey('explain-key')),
        'GET /orders',
      );
      await tester.tap(find.byKey(const ValueKey('explain-run')));
      await tester.pumpAndSettle();

      expect(
        find.text(
          'invalidated: Order:0, Order:1 (showing the first 2 of 1500)',
        ),
        findsOneWidget,
      );
      expect(find.text('carried: Order[]'), findsOneWidget);
    });

    testWidgets('a preview says how many queries of how many a tag reaches', (
      tester,
    ) async {
      final fake = FakeForgeBackend()
        ..overrides[ForgeDevtoolsProtocol.wouldInvalidate] = (params) => {
          'preview': {
            'operation': 'PATCH /orders/{id}',
            'templates': ['Order[]'],
            'tags': capped(['Order[]']),
            'unresolved': capped(<String>[]),
            'hits': capped([
              {
                'tag': 'Order[]',
                'queries': capped(['GET /orders?p=0'], total: 1005),
              },
            ]),
            'missed': capped(<String>[]),
          },
        };
      await pumpPanel(tester, fake);
      await openTab(tester, 'Tags');

      await tester.tap(find.byKey(const ValueKey('would-run')));
      await tester.pumpAndSettle();

      expect(
        find.text(
          'Order[] reaches GET /orders?p=0 (showing the first 1 of 1005)',
        ),
        findsOneWidget,
      );
      expect(find.text('missed: none'), findsOneWidget);
    });
  });

  // Final fix P1: what the app sends while it changes account is marked
  // stale, and each part of the panel says so instead of showing it.
  group('while the app changes account', () {
    testWidgets(
      'the list, the report, the preview and the operations say "switching account"',
      (tester) async {
        final fake = FakeForgeBackend();
        fake.overrides[ForgeDevtoolsProtocol.tags] = (_) => {
          'total': 0,
          'offset': 0,
          'truncated': false,
          'stale': true,
          'items': <Object?>[],
        };
        fake.overrides[ForgeDevtoolsProtocol.explain] = (params) => {
          'report': {
            'kind': 'miss',
            'query': params['key'],
            'outcome': 'not-tracked',
            'reason': 'nothing to say',
            'cause': {'label': 'nothing'},
          },
          'stale': true,
        };
        fake.overrides[ForgeDevtoolsProtocol.operations] = (_) => {
          'operations': [
            {
              'id': 'op_order_create',
              'method': 'POST',
              'path': '/orders',
              'provides': <String>[],
              'invalidates': ['Order:{res.id}'],
            },
          ],
          'total': 1,
          'truncated': false,
          'stale': true,
        };
        fake.overrides[ForgeDevtoolsProtocol.wouldInvalidate] = (_) => {
          'preview': {
            'operation': 'POST /orders',
            'templates': ['Order:{res.id}'],
            'tags': capped(['Order:9']),
            'unresolved': capped(<String>[]),
            'hits': capped([
              {'tag': 'Order:9', 'queries': capped(<String>[])},
            ]),
            'missed': capped(['Order:9']),
          },
          'stale': true,
        };
        await pumpPanel(tester, fake);
        await openTab(tester, 'Tags');

        expect(find.byKey(const ValueKey('tags-switching')), findsOneWidget);
        expect(
          find.byKey(const ValueKey('operations-switching')),
          findsOneWidget,
        );

        await tester.enterText(
          find.byKey(const ValueKey('explain-key')),
          'GET /orders',
        );
        await tester.tap(find.byKey(const ValueKey('explain-run')));
        await tester.tap(find.byKey(const ValueKey('would-run')));
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('explain-switching')), findsOneWidget);
        expect(find.byKey(const ValueKey('would-switching')), findsOneWidget);
        expect(find.byKey(const ValueKey('report-outcome')), findsNothing);
        expect(find.textContaining('reaches'), findsNothing);

        // Settled, the same panel shows the answers again.
        fake.overrides.clear();
        await tester.tap(find.byKey(const ValueKey('explain-run')));
        await tester.tap(find.byKey(const ValueKey('would-run')));
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('explain-switching')), findsNothing);
        expect(find.byKey(const ValueKey('would-switching')), findsNothing);
        expect(find.byKey(const ValueKey('report-outcome')), findsOneWidget);
      },
    );
  });
}
