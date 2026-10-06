import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

Map<String, Object?> _frame(int seq, int id) => {
  'seq': seq,
  'at': seq,
  'channel': '/ws/orders',
  'message': 'order.updated',
  'intent': 'upsert',
  'entity': 'Order',
  'payload': {'id': id},
};

void main() {
  testWidgets('is off until switched on, and switches capture over the wire', (
    tester,
  ) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Frames');

    expect(find.textContaining('Frame capture is off'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('frames-capture')));
    await tester.pumpAndSettle();

    expect(
      fake.callsTo(ForgeDevtoolsProtocol.capture).single,
      containsPair('enabled', 'true'),
    );
    expect(
      fake.callsTo(ForgeDevtoolsProtocol.capture).single,
      containsPair('limit', '200'),
    );
    expect(find.text('capacity 200'), findsOneWidget);
  });

  // Review Focus 5, from the panel's side.
  testWidgets('shows the ring overflow, keeping the newest frames in order', (
    tester,
  ) async {
    final fake = FakeForgeBackend()
      ..capturing = true
      ..frameCapacity = 2
      ..framesDropped = 3
      ..frames = [_frame(4, 4), _frame(5, 5)];
    await pumpPanel(tester, fake);
    await openTab(tester, 'Frames');

    expect(find.byKey(const ValueKey('frames-dropped')), findsOneWidget);
    expect(find.text('3 dropped'), findsOneWidget);
    expect(find.text('capacity 2'), findsOneWidget);

    final rows = tester
        .widgetList<ListTile>(
          find.byWidgetPredicate(
            (w) => w is ListTile && w.key.toString().contains('frame-'),
          ),
        )
        .toList();
    expect(rows.map((r) => (r.key! as ValueKey<String>).value), [
      'frame-4-0',
      'frame-5-1',
    ]);
  });

  testWidgets('opens a frame to read its payload', (tester) async {
    final fake = FakeForgeBackend()
      ..capturing = true
      ..frameCapacity = 10
      ..frames = [_frame(4, 42)];
    await pumpPanel(tester, fake);
    await openTab(tester, 'Frames');

    await tester.tap(find.byKey(const ValueKey('frame-4-0')));
    await tester.pumpAndSettle();

    expect(find.text('id: 42'), findsOneWidget);
  });

  testWidgets('shows what the app left out of a payload as markers', (
    tester,
  ) async {
    final fake = FakeForgeBackend()
      ..capturing = true
      ..frameCapacity = 10
      ..frames = [
        {
          ..._frame(4, 1),
          'payload': {
            'items': [1, 2, '[48 more]'],
            'self': '[cycle]',
            'deep': {'a': '[deeper]'},
            'huge': '[truncated]',
            '[more]': '[3 more]',
            'note': '${'x' * 1000}...',
          },
        },
      ];
    await pumpPanel(tester, fake);
    await openTab(tester, 'Frames');

    await tester.tap(find.byKey(const ValueKey('frame-4-0')));
    await tester.pumpAndSettle();

    expect(find.text('self: [cycle]'), findsOneWidget);
    expect(find.text('huge: [truncated]'), findsOneWidget);
    expect(find.text('[more]: [3 more]'), findsOneWidget);
    expect(find.textContaining('x...'), findsOneWidget);

    await tester.tap(find.textContaining('items ['));
    await tester.pumpAndSettle();
    expect(find.text('2: [48 more]'), findsOneWidget);
  });

  testWidgets('keeps the frame open when newer frames push the ring along', (
    tester,
  ) async {
    final fake = FakeForgeBackend()
      ..capturing = true
      ..frameCapacity = 10
      ..frames = [_frame(4, 42), _frame(5, 43)];
    await pumpPanel(tester, fake);
    await openTab(tester, 'Frames');

    await tester.tap(find.byKey(const ValueKey('frame-5-1')));
    await tester.pumpAndSettle();
    expect(find.text('id: 43'), findsOneWidget);

    // The oldest frame leaves the ring and a newer one arrives.
    fake.frames = [_frame(5, 43), _frame(6, 44)];
    fake.emit({'cache': '1', 'skipped': 0, 'entries': <Object?>[]});
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('frame-5-0')), findsOneWidget);
    expect(find.text('id: 43'), findsOneWidget);
    expect(find.text('id: 44'), findsNothing);
  });

  testWidgets('shows the principal marker as a marker, with no payload', (
    tester,
  ) async {
    final fake = FakeForgeBackend()
      ..capturing = true
      ..frameCapacity = 10
      ..frames = [
        {
          'seq': 7,
          'at': 7,
          'channel': '',
          'message': '',
          'intent': 'principal',
          'entity': '',
          'payload': null,
        },
      ];
    await pumpPanel(tester, fake);
    await openTab(tester, 'Frames');

    expect(find.text('identity changed'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('frame-7-0')));
    await tester.pumpAndSettle();

    expect(find.textContaining('Nothing before this was kept'), findsOneWidget);
  });

  testWidgets('shows a frame the app would not send as too large', (
    tester,
  ) async {
    final fake = FakeForgeBackend()
      ..capturing = true
      ..frameCapacity = 10
      ..overrides[ForgeDevtoolsProtocol.frames] = (_) => {
        'capturing': true,
        'capacity': 10,
        'dropped': 0,
        'entries': [
          {'oversized': true, 'field': 'channel', 'seq': 4},
        ],
      };
    await pumpPanel(tester, fake);
    await openTab(tester, 'Frames');

    expect(find.textContaining('too large to show (channel'), findsOneWidget);
  });
}
