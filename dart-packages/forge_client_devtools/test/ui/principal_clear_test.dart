import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';
import 'package:forge_client_devtools/src/state/connection.dart';
import 'package:forge_client_devtools/src/ui/events_panel.dart';
import 'package:forge_client_devtools/src/ui/frames_panel.dart';
import 'package:forge_client_devtools/src/ui/requests_panel.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

// P4: nothing crosses principals. These tests put alice's events, frames and
// requests in the three panels, change the principal, and look for what is
// left of her. The planted `alice-ssn` is an identifier that must not outlive
// the switch anywhere: not in a widget, not in the connection's event list.

const _secret = 'alice-ssn';

Json _aliceEvent(int seq) => {
  'kind': 'mutation',
  'seq': seq,
  'at': seq,
  'session': 0,
  'operation': 'POST /users',
  'args': '{"ssn":"$_secret"}',
  'tags': ['User:$_secret'],
  'unresolved': <String>[],
};

Json _aliceFrame() => {
  'seq': 3,
  'at': 3,
  'channel': '/ws/users',
  'message': 'user.updated',
  'intent': 'upsert',
  'entity': 'User',
  'payload': {
    'ssn': _secret,
    'friends': ['bob', '[2 more]'],
  },
};

Json _aliceRequest() => {
  'id': 7,
  'operation': 'GET /users/$_secret',
  'method': 'GET',
  'args': '{"query":{"ssn":"$_secret"}}',
  'at': 1,
  'duration': 5,
  'attempts': 1,
  'limit': 3,
  'status': 200,
  'outcome': 'ok',
  'retries': <Object?>[],
  'refreshes': 0,
  'joined': false,
  'authMs': 0,
  'marker': false,
};

Json _marker(int seq, int session) => {
  'kind': 'principal',
  'seq': seq,
  'at': seq,
  'session': session,
};

/// A fake that is alice's: her log, her frames, her requests.
FakeForgeBackend _alice() => FakeForgeBackend()
  ..capturing = true
  ..frameCapacity = 10
  ..overrides[ForgeDevtoolsProtocol.log] = ((_) => {
    'entries': [_aliceEvent(1)],
    'dropped': 0,
    'sequence': 2,
    'session': 0,
    'truncated': false,
  })
  ..overrides[ForgeDevtoolsProtocol.frames] = ((_) => {
    'capturing': true,
    'capacity': 10,
    'dropped': 0,
    'entries': [_aliceFrame()],
  })
  ..overrides[ForgeDevtoolsProtocol.requests] = ((_) => {
    'watching': true,
    'dropped': 0,
    'entries': [_aliceRequest()],
  });

/// What the app serves once it is on session 1: a marker, and nothing else.
void _becomeBob(FakeForgeBackend fake) {
  fake.session = 1;
  fake.overrides[ForgeDevtoolsProtocol.log] = (_) => {
    'entries': [_marker(5, 1)],
    'dropped': 0,
    'sequence': 6,
    'session': 1,
    'truncated': false,
  };
  fake.overrides[ForgeDevtoolsProtocol.frames] = (_) => {
    'capturing': true,
    'capacity': 10,
    'dropped': 0,
    'entries': [
      {
        'seq': 5,
        'at': 5,
        'channel': '',
        'message': '',
        'intent': 'principal',
        'entity': '',
        'payload': null,
      },
    ],
  };
  fake.overrides[ForgeDevtoolsProtocol.requests] = (_) => {
    'watching': true,
    'dropped': 0,
    'entries': [
      {
        'id': -1,
        'operation': 'principal changed',
        'method': '',
        'args': '',
        'at': 5,
        'duration': null,
        'attempts': 0,
        'limit': 0,
        'status': null,
        'outcome': 'ok',
        'retries': <Object?>[],
        'refreshes': 0,
        'joined': false,
        'authMs': 0,
        'marker': true,
      },
    ],
  };
}

/// The three panels on one connection, side by side, so all of them are
/// alive at once and none can be rebuilt by a tab switch.
Future<ForgeConnection> _pumpTogether(
  WidgetTester tester,
  FakeForgeBackend fake,
) async {
  await tester.binding.setSurfaceSize(const Size(2400, 1000));
  addTearDown(() => tester.binding.setSurfaceSize(null));

  final connection = ForgeConnection(fake);
  addTearDown(connection.dispose);
  await tester.pumpAndSettle();

  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Row(
          children: [
            Expanded(child: EventsPanel(connection: connection)),
            Expanded(child: FramesPanel(connection: connection)),
            Expanded(child: RequestsPanel(connection: connection)),
          ],
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();

  return connection;
}

Finder get _alice_ => find.textContaining(_secret, skipOffstage: false);

void main() {
  testWidgets('a marker on forge:event clears every panel and leaves only '
      'the marker', (tester) async {
    final fake = _alice();
    final connection = await _pumpTogether(tester, fake);

    // Alice's live event, and her frame opened in the detail pane.
    fake.emit({
      'cache': '1',
      'skipped': 0,
      'entries': [_aliceEvent(2)],
    });
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('frame-3-0')));
    await tester.pumpAndSettle();

    expect(_alice_, findsWidgets);
    expect(find.text('ssn: "$_secret"'), findsOneWidget);
    expect(jsonEncode(connection.events), contains(_secret));

    _becomeBob(fake);
    fake.emit({
      'cache': '1',
      'skipped': 0,
      'entries': [_marker(5, 1)],
    });
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(_alice_, findsNothing);
    // The connection's own list is the marker alone.
    expect(connection.events, hasLength(1));
    expect(connection.events.single['kind'], 'principal');
    expect(jsonEncode(connection.events), isNot(contains(_secret)));
    // And each panel says so.
    expect(find.text('identity changed, session 1'), findsOneWidget);
    expect(find.text('identity changed'), findsNWidgets(2));
    expect(find.text('Pick a frame to read its payload.'), findsOneWidget);
  });

  testWidgets('an isolate change clears every panel too', (tester) async {
    final fake = _alice();
    await _pumpTogether(tester, fake);
    expect(_alice_, findsWidgets);

    // Another app answers from now on, and reuses cache 1.
    _becomeBob(fake);
    fake.swapIsolate();
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(_alice_, findsNothing);
  });

  testWidgets('a read that was on its way when the principal changed is '
      'dropped, in every panel', (tester) async {
    final fake = _alice();
    final connection = await _pumpTogether(tester, fake);

    // From here the app answers late, with alice's data.
    final late = <String, Completer<Json>>{
      ForgeDevtoolsProtocol.log: Completer<Json>(),
      ForgeDevtoolsProtocol.frames: Completer<Json>(),
      ForgeDevtoolsProtocol.requests: Completer<Json>(),
    };
    final aliceAnswers = {
      for (final method in late.keys) method: fake.overrides[method]!({}),
    };
    for (final method in late.keys) {
      fake.overrides[method] = (_) => late[method]!.future;
    }

    // Activity makes the frames and requests panels read again.
    fake.emit({'cache': '1', 'skipped': 0, 'entries': <Object?>[]});
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 1));

    // The principal changes while those reads are out, and the panels' reads
    // after it are served at once.
    _becomeBob(fake);
    final bob = {
      for (final method in late.keys) method: fake.overrides[method]!,
    };
    for (final method in late.keys) {
      fake.overrides[method] = (params) => late[method]!.isCompleted
          ? bob[method]!(params)
          : late[method]!.future;
    }
    fake.emit({
      'cache': '1',
      'skipped': 0,
      'entries': [_marker(5, 1)],
    });
    await tester.pump(const Duration(seconds: 1));

    // The old reads come back with alice's data.
    for (final method in late.keys) {
      late[method]!.complete(await aliceAnswers[method]!);
    }
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(_alice_, findsNothing);
    expect(jsonEncode(connection.events), isNot(contains(_secret)));
  });

  testWidgets('through the workspace, no tab shows her after the switch', (
    tester,
  ) async {
    final fake = _alice();
    await pumpPanel(tester, fake);

    await openTab(tester, 'Events');
    expect(_alice_, findsWidgets);
    await openTab(tester, 'Frames');
    await tester.tap(find.byKey(const ValueKey('frame-3-0')));
    await tester.pumpAndSettle();
    expect(_alice_, findsWidgets);
    await openTab(tester, 'Requests');
    expect(_alice_, findsWidgets);
    await openTab(tester, 'Events');

    _becomeBob(fake);
    fake.emit({
      'cache': '1',
      'skipped': 0,
      'entries': [_marker(5, 1)],
    });
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    for (final tab in ['Events', 'Frames', 'Requests', 'Queries', 'Events']) {
      await openTab(tester, tab);
      expect(_alice_, findsNothing, reason: tab);
    }
  });
}
