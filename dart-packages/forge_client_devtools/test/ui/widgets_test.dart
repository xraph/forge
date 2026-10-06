import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';
import 'package:forge_client_devtools/src/ui/widgets.dart';

void main() {
  testWidgets(
    'a paged list loads the next page at the end and never asks past the total',
    (tester) async {
      final asked = <(int, int)>[];
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PagedList(
              pageSize: 20,
              totalLabel: (total) => '$total rows',
              fetch: (offset, limit) async {
                asked.add((offset, limit));
                final end = (offset + limit).clamp(0, 45);
                return (
                  total: 45,
                  items: [
                    for (var i = offset; i < end; i++) {'id': i},
                  ],
                );
              },
              // Tall rows, so the first page overfills the viewport and its cache
              // extent: the next page must wait for a scroll.
              itemBuilder: (context, row) =>
                  SizedBox(height: 100, child: Text('row ${row['id']}')),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('45 rows'), findsOneWidget);
      expect(asked, [(0, 20)]);

      for (var i = 0; i < 6; i++) {
        await tester.drag(find.byType(ListView), const Offset(0, -2000));
        await tester.pumpAndSettle();
      }

      expect(asked, [(0, 20), (20, 20), (40, 20)]);
      expect(find.text('row 44'), findsOneWidget);
    },
  );

  testWidgets(
    'a paged list reloads from the start when its reload token changes',
    (tester) async {
      final asked = <int>[];
      Widget build(Object token) => MaterialApp(
        home: Scaffold(
          body: PagedList(
            reloadToken: token,
            fetch: (offset, limit) async {
              asked.add(offset);
              return (
                total: 1,
                items: [
                  <String, Object?>{'id': token},
                ],
              );
            },
            itemBuilder: (context, row) => Text('row ${row['id']}'),
          ),
        ),
      );

      await tester.pumpWidget(build('a'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(build('b'));
      await tester.pumpAndSettle();

      expect(asked, [0, 0]);
      expect(find.text('row b'), findsOneWidget);
    },
  );

  testWidgets('a json view folds maps and lists and prints scalars', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: JsonView(
              {
                'id': 1,
                'customer': {'__ref': 'Customer:c1'},
                'items': [1, 2],
              },
              label: 'value',
              initiallyExpanded: true,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('id: 1'), findsOneWidget);
    expect(find.text('customer {1}'), findsOneWidget);
    expect(find.text('items [2]'), findsOneWidget);
  });

  test('a throttle runs at once, then at most once per interval with a trailing call', () async {
    var runs = 0;
    final throttle = Throttle(const Duration(milliseconds: 20), () => runs++);

    throttle();
    throttle();
    throttle();
    expect(runs, 1);

    await Future<void>.delayed(const Duration(milliseconds: 40));
    expect(runs, 2);

    throttle.dispose();
  });

  testWidgets(
    'a paged list re-reads the shown rows in place on a refresh token, without Loading',
    (tester) async {
      final asked = <(int, int)>[];
      var version = 'a';
      Widget build(Object token) => MaterialApp(
        home: Scaffold(
          body: PagedList(
            pageSize: 2,
            refreshToken: token,
            fetch: (offset, limit) async {
              asked.add((offset, limit));
              final end = (offset + limit).clamp(0, 3);
              return (
                total: 3,
                items: [
                  for (var i = offset; i < end; i++)
                    <String, Object?>{'id': '$version$i'},
                ],
              );
            },
            itemBuilder: (context, row) => Text('row ${row['id']}'),
          ),
        ),
      );

      await tester.pumpWidget(build(0));
      await tester.pumpAndSettle();
      expect(find.text('row a2'), findsOneWidget);
      expect(asked, [(0, 2), (2, 2)]);

      version = 'b';
      await tester.pumpWidget(build(1));
      // The old rows stay up while the new ones load.
      expect(find.text('Loading...'), findsNothing);
      expect(find.text('row a0'), findsOneWidget);
      await tester.pumpAndSettle();

      expect(find.text('row b0'), findsOneWidget);
      expect(find.text('row b2'), findsOneWidget);
      expect(find.text('row a0'), findsNothing);
      expect(asked, [(0, 2), (2, 2), (0, 2), (2, 2)]);
    },
  );

  testWidgets(
    'a paged list shows an oversized marker row as too large, not blank',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PagedList(
              fetch: (offset, limit) async => (
                total: 2,
                items: <Json>[
                  {'key': 'GET /a'},
                  {'oversized': true, 'field': 'key'},
                ],
              ),
              itemBuilder: (context, row) => Text('row ${row['key']}'),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('row GET /a'), findsOneWidget);
      expect(
        find.text('too large to show (key is over 64 KB)'),
        findsOneWidget,
      );
      expect(find.text('row null'), findsNothing);
    },
  );

  group('a paged list', () {
    Future<({int total, List<Json> items})> rows(
      int offset,
      int limit,
      int total, [
      String tag = '',
    ]) async => (
      total: total,
      items: [
        for (var i = offset; i < (offset + limit).clamp(0, total); i++)
          <String, Object?>{'id': '$tag$i'},
      ],
    );

    Widget host(PagedList list) => MaterialApp(home: Scaffold(body: list));

    testWidgets(
      'retries a first load that failed when a refresh token arrives',
      (tester) async {
        var calls = 0;
        Widget build(Object token) => host(
          PagedList(
            refreshToken: token,
            fetch: (offset, limit) async {
              calls++;
              if (calls == 1) throw StateError('the app was not ready');
              return rows(offset, limit, 3);
            },
            itemBuilder: (context, row) => Text('row ${row['id']}'),
          ),
        );

        await tester.pumpWidget(build(0));
        await tester.pumpAndSettle();
        expect(find.text('Bad state: the app was not ready'), findsOneWidget);
        expect(find.text('row 0'), findsNothing);

        await tester.pumpWidget(build(1));
        await tester.pumpAndSettle();

        expect(calls, 2);
        expect(find.text('row 0'), findsOneWidget);
        expect(find.byKey(const ValueKey('paged-error')), findsNothing);
      },
    );

    testWidgets('retries a first load that failed when Retry is pressed', (
      tester,
    ) async {
      var calls = 0;
      await tester.pumpWidget(
        host(
          PagedList(
            fetch: (offset, limit) async {
              calls++;
              if (calls == 1) throw StateError('no');
              return rows(offset, limit, 1);
            },
            itemBuilder: (context, row) => Text('row ${row['id']}'),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const ValueKey('paged-retry')));
      await tester.pumpAndSettle();

      expect(find.text('row 0'), findsOneWidget);
    });

    testWidgets('does not ask again while the first load is still on its way', (
      tester,
    ) async {
      final slow = Completer<({int total, List<Json> items})>();
      var calls = 0;
      Widget build(Object token) => host(
        PagedList(
          refreshToken: token,
          fetch: (offset, limit) {
            calls++;
            return slow.future;
          },
          itemBuilder: (context, row) => Text('row ${row['id']}'),
        ),
      );

      await tester.pumpWidget(build(0));
      await tester.pumpWidget(build(1));
      await tester.pumpWidget(build(2));
      await tester.pump();

      expect(calls, 1);
      slow.complete((total: 0, items: const <Json>[]));
      await tester.pumpAndSettle();
    });

    testWidgets(
      'shows a refresh error while rows are shown and still loads more',
      (tester) async {
        var refreshFails = false;
        final asked = <int>[];
        Widget build(Object token) => host(
          PagedList(
            pageSize: 20,
            refreshToken: token,
            fetch: (offset, limit) async {
              asked.add(offset);
              if (refreshFails && offset == 0) {
                throw StateError('connection lost');
              }
              return rows(offset, limit, 60);
            },
            itemBuilder: (context, row) =>
                SizedBox(height: 100, child: Text('row ${row['id']}')),
          ),
        );

        await tester.pumpWidget(build(0));
        await tester.pumpAndSettle();
        expect(find.text('row 0'), findsOneWidget);

        refreshFails = true;
        await tester.pumpWidget(build(1));
        await tester.pumpAndSettle();

        // The error is up, and so are the rows.
        expect(
          find.text('Could not refresh: Bad state: connection lost'),
          findsOneWidget,
        );
        expect(find.text('row 0'), findsOneWidget);

        // Loading more is not blocked by it.
        asked.clear();
        await tester.drag(find.byType(ListView), const Offset(0, -2000));
        await tester.pumpAndSettle();

        expect(asked, [20]);
        expect(find.text('row 20'), findsOneWidget);
        expect(
          find.text('Could not refresh: Bad state: connection lost'),
          findsOneWidget,
        );

        // The next refresh that works clears it.
        refreshFails = false;
        await tester.pumpWidget(build(2));
        await tester.pumpAndSettle();

        expect(find.byKey(const ValueKey('paged-refresh-error')), findsNothing);
      },
    );

    testWidgets(
      'a page that fails to load is offered again, and is not retried every frame',
      (tester) async {
        var failing = true;
        final asked = <int>[];
        await tester.pumpWidget(
          host(
            PagedList(
              pageSize: 20,
              fetch: (offset, limit) async {
                asked.add(offset);
                if (offset == 20 && failing) throw StateError('timed out');
                return rows(offset, limit, 60);
              },
              itemBuilder: (context, row) =>
                  SizedBox(height: 100, child: Text('row ${row['id']}')),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.drag(find.byType(ListView), const Offset(0, -2000));
        await tester.pumpAndSettle();
        await tester.pump(const Duration(seconds: 2));

        expect(asked, [0, 20]);
        expect(find.byKey(const ValueKey('paged-failed-1')), findsOneWidget);

        failing = false;
        await tester.tap(find.byKey(const ValueKey('paged-failed-1')));
        await tester.pumpAndSettle();

        expect(asked, [0, 20, 20]);
        expect(find.text('row 20'), findsOneWidget);
      },
    );

    testWidgets('a refresh that works offers the failed pages again', (
      tester,
    ) async {
      var failing = true;
      final asked = <int>[];
      Widget build(Object token) => host(
        PagedList(
          pageSize: 20,
          refreshToken: token,
          fetch: (offset, limit) async {
            asked.add(offset);
            if (offset == 20 && failing) throw StateError('timed out');
            return rows(offset, limit, 60);
          },
          itemBuilder: (context, row) =>
              SizedBox(height: 100, child: Text('row ${row['id']}')),
        ),
      );

      await tester.pumpWidget(build(0));
      await tester.pumpAndSettle();
      await tester.drag(find.byType(ListView), const Offset(0, -2000));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('paged-failed-1')), findsOneWidget);

      failing = false;
      await tester.pumpWidget(build(1));
      await tester.pumpAndSettle();

      expect(find.text('row 20'), findsOneWidget);
    });

    testWidgets('drops a reply that arrives after a reload', (tester) async {
      final slow = Completer<({int total, List<Json> items})>();
      Widget build(Object token) => host(
        PagedList(
          reloadToken: token,
          fetch: (offset, limit) =>
              token == 'a' ? slow.future : rows(offset, limit, 1, '$token'),
          itemBuilder: (context, row) => Text('row ${row['id']}'),
        ),
      );

      await tester.pumpWidget(build('a'));
      await tester.pumpWidget(build('b'));
      await tester.pumpAndSettle();
      expect(find.text('row b0'), findsOneWidget);

      slow.complete((
        total: 1,
        items: [
          <String, Object?>{'id': 'a-late'},
        ],
      ));
      await tester.pumpAndSettle();

      expect(find.text('row b0'), findsOneWidget);
      expect(find.text('row a-late'), findsNothing);
    });

    testWidgets(
      'holds at most maxPages pages, and a refresh re-reads exactly those',
      (tester) async {
        final asked = <int>[];
        Widget build(Object token) => host(
          PagedList(
            pageSize: 10,
            maxPages: 3,
            itemExtent: 50,
            refreshToken: token,
            fetch: (offset, limit) {
              asked.add(offset);
              return rows(offset, limit, 1000);
            },
            itemBuilder: (context, row) => Text('row ${row['id']}'),
          ),
        );

        await tester.pumpWidget(build(0));
        await tester.pumpAndSettle();
        final scroll = tester.state<ScrollableState>(find.byType(Scrollable));

        // Walk through ten pages, one at a time.
        for (var page = 1; page < 10; page++) {
          scroll.position.jumpTo(page * 10 * 50.0);
          await tester.pumpAndSettle();
        }
        asked.clear();

        await tester.pumpWidget(build(1));
        await tester.pumpAndSettle();

        expect(asked.length, lessThanOrEqualTo(3));
        expect(asked.length, greaterThanOrEqualTo(1));
      },
    );

    testWidgets('shrinks with the total on a refresh', (tester) async {
      var total = 45;
      Widget build(Object token) => host(
        PagedList(
          pageSize: 20,
          refreshToken: token,
          totalLabel: (n) => '$n rows',
          fetch: (offset, limit) => rows(offset, limit, total),
          itemBuilder: (context, row) =>
              SizedBox(height: 100, child: Text('row ${row['id']}')),
        ),
      );

      await tester.pumpWidget(build(0));
      await tester.pumpAndSettle();
      expect(find.text('45 rows'), findsOneWidget);

      total = 3;
      await tester.pumpWidget(build(1));
      await tester.pumpAndSettle();

      expect(find.text('3 rows'), findsOneWidget);
      expect(find.text('row 2'), findsOneWidget);
      expect(find.text('row 3'), findsNothing);
    });
  });
}
