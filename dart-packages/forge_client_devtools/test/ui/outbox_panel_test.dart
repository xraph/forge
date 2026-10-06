import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';
import 'package:forge_client_devtools/src/state/connection.dart';
import 'package:forge_client_devtools/src/ui/control_rail.dart';
import 'package:forge_client_devtools/src/ui/outbox_panel.dart';
import 'package:forge_client_devtools/src/ui/sync_panel.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

Json _row(String id, String state, {String? failure, int? since}) => {
  'id': id,
  'operation': 'op_$id',
  'createdAt': 0,
  'state': state,
  'failure': failure,
  'since': since,
  'at': null,
};

Json _answer(List<Json> rows, {String source = 'session'}) => {
  'wired': true,
  'source': source,
  'entries': rows,
  'total': rows.length,
  'truncated': false,
};

Json _marker(int seq, int session) => {
  'kind': 'principal',
  'seq': seq,
  'at': seq,
  'session': session,
};

void main() {
  testWidgets('lists queued and failed writes with the failure', (
    tester,
  ) async {
    await pumpPanel(tester);
    await openTab(tester, 'Outbox');

    expect(find.byKey(const ValueKey('outbox-m1')), findsOneWidget);
    expect(find.byKey(const ValueKey('outbox-m2')), findsOneWidget);
    // The failure is what the runtime sends: a kind and a status, no body.
    expect(find.text('failed: conflict 409'), findsOneWidget);
    expect(find.textContaining('OutboxConflict'), findsNothing);
  });

  testWidgets('shows an uncertain write and an unreadable one as the runtime '
      'words them', (tester) async {
    final fake = FakeForgeBackend()
      ..overrides[ForgeDevtoolsProtocol.outbox] = (_) => _answer([
        _row('u1', 'failed', failure: 'uncertain: connection reset'),
        _row('u2', 'failed', failure: 'unreadable'),
      ]);
    await pumpPanel(tester, fake);
    await openTab(tester, 'Outbox');

    expect(find.text('failed: uncertain: connection reset'), findsOneWidget);
    expect(find.text('failed: unreadable'), findsOneWidget);
  });

  testWidgets('replays and discards through the app, each aimed at the '
      'session', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Outbox');

    await tester.tap(find.byKey(const ValueKey('outbox-replay-m2')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('outbox-discard-m1')));
    await tester.pumpAndSettle();

    expect(fake.actions, ['replay m2', 'discard m1']);
    for (final call in fake.callsTo(ForgeDevtoolsProtocol.outboxAction)) {
      expect(call['session'], '0');
    }
  });

  testWidgets('reads the outbox again after an action', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Outbox');
    final before = fake.callsTo(ForgeDevtoolsProtocol.outbox).length;

    await tester.tap(find.byKey(const ValueKey('outbox-replay-m1')));
    await tester.pumpAndSettle();

    expect(
      fake.callsTo(ForgeDevtoolsProtocol.outbox).length,
      greaterThan(before),
    );
  });

  testWidgets('a write being sent has nothing to replay or discard', (
    tester,
  ) async {
    await pumpPanel(tester);
    await openTab(tester, 'Outbox');

    final replay = tester.widget<TextButton>(
      find.byKey(const ValueKey('outbox-replay-m3')),
    );
    final discard = tester.widget<TextButton>(
      find.byKey(const ValueKey('outbox-discard-m3')),
    );
    expect(replay.onPressed, isNull);
    expect(discard.onPressed, isNull);
  });

  testWidgets('disables replay and discard when no inspector is wired, and '
      'says why', (tester) async {
    await pumpPanel(tester, FakeForgeBackend()..outboxWired = false);
    await openTab(tester, 'Outbox');

    final replay = tester.widget<TextButton>(
      find.byKey(const ValueKey('outbox-replay-m1')),
    );
    expect(replay.onPressed, isNull);
    expect(find.textContaining('OutboxInspector'), findsOneWidget);
  });

  testWidgets('says when it can only show writes seen since DevTools '
      'attached', (tester) async {
    await pumpPanel(tester, FakeForgeBackend()..outboxSource = 'events');
    await openTab(tester, 'Outbox');

    expect(find.textContaining('only writes seen since'), findsOneWidget);
  });

  testWidgets('says how many writes it is not showing', (tester) async {
    final fake = FakeForgeBackend()
      ..overrides[ForgeDevtoolsProtocol.outbox] = (_) => {
        'wired': true,
        'source': 'session',
        'entries': [_row('a', 'queued'), _row('b', 'queued')],
        'total': 450,
        'truncated': true,
      };
    await pumpPanel(tester, fake);
    await openTab(tester, 'Outbox');

    expect(find.text('Showing the first 2 of 450 writes.'), findsOneWidget);
  });

  testWidgets('an answer taken while the app changes account says '
      '"switching account", not that there is no storage session', (
    tester,
  ) async {
    // The runtime's stale answer: empty, `source: events`, `stale: true`.
    final fake = FakeForgeBackend()
      ..overrides[ForgeDevtoolsProtocol.outbox] = (_) => {
        'wired': true,
        'source': 'events',
        'entries': <Object?>[],
        'stale': true,
      };
    await pumpPanel(tester, fake);
    await openTab(tester, 'Outbox');

    expect(find.text('switching account'), findsWidgets);
    expect(find.textContaining('no storage session'), findsNothing);
    expect(find.text('The outbox is empty.'), findsNothing);
  });

  testWidgets('an action aimed at a session the app left is refused, says '
      'so, and reloads the view onto the new session', (tester) async {
    final fake = await pumpPanel(tester);
    await openTab(tester, 'Outbox');
    final before = fake.callsTo(ForgeDevtoolsProtocol.outbox).length;

    // The app changed account behind the panel's back.
    fake.session = 1;
    await tester.tap(find.byKey(const ValueKey('outbox-replay-m2')));
    await tester.pumpAndSettle();

    expect(fake.actions, isEmpty);
    expect(find.textContaining('aimed at session 0'), findsOneWidget);
    expect(
      fake.callsTo(ForgeDevtoolsProtocol.outbox).length,
      greaterThan(before),
    );

    // The panel now aims at session 1, so the same click goes through.
    await tester.tap(find.byKey(const ValueKey('outbox-replay-m2')));
    await tester.pumpAndSettle();

    expect(fake.actions, ['replay m2']);
    expect(
      fake.callsTo(ForgeDevtoolsProtocol.outboxAction).last['session'],
      '1',
    );
  });

  testWidgets('an inspector that refuses is shown as it said it', (
    tester,
  ) async {
    final fake = FakeForgeBackend()
      ..overrides[ForgeDevtoolsProtocol.outboxAction] = (_) =>
          throw const BackendError(
            ForgeDevtoolsProtocol.outboxAction,
            'the write belongs to another principal',
          );
    await pumpPanel(tester, fake);
    await openTab(tester, 'Outbox');

    await tester.tap(find.byKey(const ValueKey('outbox-replay-m1')));
    await tester.pumpAndSettle();

    expect(find.text('the write belongs to another principal'), findsOneWidget);
  });

  group('nothing crosses principals', () {
    const secret = 'alice-write';

    FakeForgeBackend alice() =>
        FakeForgeBackend()
          ..overrides[ForgeDevtoolsProtocol.outbox] = (_) =>
              _answer([_row(secret, 'queued')]);

    void becomeBob(FakeForgeBackend fake) {
      fake.session = 1;
      fake.overrides[ForgeDevtoolsProtocol.outbox] = (_) => _answer([]);
    }

    testWidgets('a principal marker clears the outbox', (tester) async {
      final fake = alice();
      await pumpPanel(tester, fake);
      await openTab(tester, 'Outbox');
      expect(find.textContaining(secret), findsWidgets);

      becomeBob(fake);
      fake.emit({
        'cache': '1',
        'skipped': 0,
        'entries': [_marker(5, 1)],
      });
      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();

      expect(find.textContaining(secret), findsNothing);
      expect(find.text('The outbox is empty.'), findsOneWidget);
    });

    testWidgets('an isolate change clears the outbox', (tester) async {
      final fake = alice();
      await pumpPanel(tester, fake);
      await openTab(tester, 'Outbox');
      expect(find.textContaining(secret), findsWidgets);

      becomeBob(fake);
      fake.swapIsolate();
      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();

      expect(find.textContaining(secret), findsNothing);
    });

    testWidgets('a read that was on its way when the principal changed is '
        'dropped', (tester) async {
      final fake = alice();
      await pumpPanel(tester, fake);
      await openTab(tester, 'Outbox');

      // The next read is slow and brings alice's data; every read after it is
      // answered at once, for bob.
      final late = Completer<Json>();
      final aliceAnswer = _answer([_row(secret, 'queued')]);
      var reads = 0;
      fake.overrides[ForgeDevtoolsProtocol.outbox] = (_) =>
          reads++ == 0 ? late.future : _answer([]);

      fake.emit({'cache': '1', 'skipped': 0, 'entries': <Object?>[]});
      await tester.pump(const Duration(seconds: 1));
      expect(reads, 1);

      // The principal changes while that read is out.
      fake.session = 1;
      fake.emit({
        'cache': '1',
        'skipped': 0,
        'entries': [_marker(5, 1)],
      });
      await tester.pump(const Duration(seconds: 1));

      late.complete(aliceAnswer);
      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();

      expect(find.textContaining(secret), findsNothing);
    });

    testWidgets('the panels drop it on their own, without the workspace '
        'rebuilding them', (tester) async {
      final fake = alice()
        ..overrides[ForgeDevtoolsProtocol.sync] = (_) => {
          'sources': <Object?>[],
          'entities': [
            {'entity': secret, 'status': 'synced', 'pending': 0, 'error': null},
          ],
        };
      await tester.binding.setSurfaceSize(const Size(2400, 1000));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final connection = ForgeConnection(fake);
      addTearDown(connection.dispose);
      await tester.pumpAndSettle();

      // No keys on the generation: the same widgets live through the switch.
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Row(
              children: [
                Expanded(child: OutboxPanel(connection: connection)),
                Expanded(child: SyncPanel(connection: connection)),
                ControlRail(connection: connection),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining(secret), findsNWidgets(2));

      expect(find.byKey(const ValueKey('control-mode')), findsOneWidget);

      // The app is slow to answer for the new principal: until it does, the
      // panels hold nothing, rather than what they held for alice.
      fake.session = 1;
      for (final method in [
        ForgeDevtoolsProtocol.outbox,
        ForgeDevtoolsProtocol.sync,
        ForgeDevtoolsProtocol.control,
      ]) {
        fake.overrides[method] = (_) => Completer<Json>().future;
      }
      final before = fake.callsTo(ForgeDevtoolsProtocol.control).length;
      fake.emit({
        'cache': '1',
        'skipped': 0,
        'entries': [_marker(5, 1)],
      });
      await tester.pump(const Duration(seconds: 1));
      await tester.pump(const Duration(seconds: 1));

      expect(find.textContaining(secret), findsNothing);
      expect(find.byKey(const ValueKey('control-mode')), findsNothing);
      expect(
        fake.callsTo(ForgeDevtoolsProtocol.control).length,
        greaterThan(before),
      );
    });
  });
}
