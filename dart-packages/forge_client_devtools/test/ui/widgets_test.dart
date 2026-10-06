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
}
