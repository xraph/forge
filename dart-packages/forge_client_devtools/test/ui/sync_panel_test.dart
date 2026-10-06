import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

Json _marker(int seq, int session) => {
  'kind': 'principal',
  'seq': seq,
  'at': seq,
  'session': session,
};

Json _answer({required Object? detail, List<Json> entities = const []}) => {
  'sources': [
    {
      'type': 'GroveSyncSource',
      'entities': ['Doc'],
      'detail': detail,
    },
  ],
  'entities': entities,
};

void main() {
  testWidgets('shows each source, its replica detail and the status of each '
      'entity', (tester) async {
    await pumpPanel(tester);
    await openTab(tester, 'Sync');

    expect(find.text('GroveSyncSource'), findsOneWidget);
    // What GroveSyncSource.describeForDevtools returns: the clock is an
    // object and the peers are a list.
    expect(find.text('protocol: grove-crdt'), findsOneWidget);
    expect(find.text('node: replica-a'), findsOneWidget);
    expect(find.text('hlc: 1700000000000:42:replica-a'), findsOneWidget);
    expect(find.text('peers: 2 (replica-b, replica-c)'), findsOneWidget);
    expect(find.byKey(const ValueKey('sync-entity-Doc')), findsOneWidget);
    expect(find.textContaining('pending, 3 changes'), findsOneWidget);
  });

  testWidgets('keeps the full description under a fold', (tester) async {
    await pumpPanel(tester);
    await openTab(tester, 'Sync');

    expect(find.text('detail {5}'), findsOneWidget);
    await tester.tap(find.text('entities {1}'));
    await tester.pumpAndSettle();
    expect(find.text('Doc {5}'), findsOneWidget);
  });

  testWidgets('says so when the cache has no sync sources', (tester) async {
    final fake = FakeForgeBackend();
    fake.overrides[ForgeDevtoolsProtocol.sync] = (_) => {
      'sources': <Object?>[],
      'entities': <Object?>[],
    };
    await pumpPanel(tester, fake);
    await openTab(tester, 'Sync');

    expect(
      find.textContaining('No sync sources on this cache'),
      findsOneWidget,
    );
  });

  testWidgets('an answer taken while the app changes account says '
      '"switching account", not that there are no sync sources', (
    tester,
  ) async {
    final fake = FakeForgeBackend();
    fake.overrides[ForgeDevtoolsProtocol.sync] = (_) => {
      'sources': <Object?>[],
      'entities': <Object?>[],
      'stale': true,
    };
    await pumpPanel(tester, fake);
    await openTab(tester, 'Sync');

    expect(find.text('switching account'), findsWidgets);
    expect(find.textContaining('No sync sources'), findsNothing);
  });

  testWidgets('says when a source does not describe itself', (tester) async {
    final fake = FakeForgeBackend();
    fake.overrides[ForgeDevtoolsProtocol.sync] = (_) => _answer(detail: null);
    await pumpPanel(tester, fake);
    await openTab(tester, 'Sync');

    expect(find.textContaining('does not describe itself'), findsOneWidget);
  });

  testWidgets('shows a source that has not started, and one that failed to '
      'describe itself', (tester) async {
    final fake = FakeForgeBackend();
    fake.overrides[ForgeDevtoolsProtocol.sync] = (_) => _answer(
      detail: {
        'protocol': 'grove-crdt',
        'nodeId': null,
        'hlc': {'ts': '0', 'counter': 0, 'nodeId': ''},
        'entities': <String, Object?>{},
        'peers': <Object?>[],
      },
    );
    await pumpPanel(tester, fake);
    await openTab(tester, 'Sync');

    expect(find.text('node: not running'), findsOneWidget);
    expect(find.text('peers: 0'), findsOneWidget);

    fake.overrides[ForgeDevtoolsProtocol.sync] = (_) =>
        _answer(detail: {'error': 'describe blew up'});
    await tester.tap(find.byKey(const ValueKey('status-refresh')));
    await tester.pump(const Duration(seconds: 1));
    fake.emit({'cache': '1', 'skipped': 0, 'entries': <Object?>[]});
    await tester.pump(const Duration(seconds: 1));
    await tester.pumpAndSettle();

    expect(find.text('error: "describe blew up"'), findsOneWidget);
    expect(find.textContaining('node:'), findsNothing);
  });

  testWidgets('shows a failed entity with its error, and a single change in '
      'the singular', (tester) async {
    final fake = FakeForgeBackend();
    fake.overrides[ForgeDevtoolsProtocol.sync] = (_) => {
      'sources': <Object?>[],
      'entities': [
        {'entity': 'Doc', 'status': 'pending', 'pending': 1, 'error': null},
        {'entity': 'Note', 'status': 'failed', 'pending': 0, 'error': 'boom'},
      ],
    };
    await pumpPanel(tester, fake);
    await openTab(tester, 'Sync');

    expect(find.text('pending, 1 change'), findsOneWidget);
    expect(find.text('failed: boom'), findsOneWidget);
  });

  group('nothing crosses principals', () {
    const secret = 'alice-doc';

    FakeForgeBackend alice() => FakeForgeBackend()
      ..overrides[ForgeDevtoolsProtocol.sync] = (_) => {
        'sources': [
          {
            'type': 'GroveSyncSource',
            'entities': [secret],
            'detail': {
              'protocol': 'grove-crdt',
              'nodeId': 'node-$secret',
              'peers': ['peer-$secret'],
            },
          },
        ],
        'entities': [
          {'entity': secret, 'status': 'synced', 'pending': 0, 'error': null},
        ],
      };

    void becomeBob(FakeForgeBackend fake) {
      fake.session = 1;
      fake.overrides[ForgeDevtoolsProtocol.sync] = (_) => {
        'sources': <Object?>[],
        'entities': <Object?>[],
      };
    }

    testWidgets('a principal marker clears the sync view', (tester) async {
      final fake = alice();
      await pumpPanel(tester, fake);
      await openTab(tester, 'Sync');
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
      expect(find.textContaining('No sync sources'), findsOneWidget);
    });

    testWidgets('an isolate change clears the sync view', (tester) async {
      final fake = alice();
      await pumpPanel(tester, fake);
      await openTab(tester, 'Sync');
      expect(find.textContaining(secret), findsWidgets);

      becomeBob(fake);
      fake.swapIsolate();
      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();

      expect(find.textContaining(secret), findsNothing);
    });
  });
}
