import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';

import '../support/fake_backend.dart';
import '../support/pump.dart';

Map<String, Object?> _request({
  int id = 1,
  String operation = 'GET /orders',
  String args = '',
  int? status,
  String outcome = 'ok',
  int? duration = 12,
  int refreshes = 0,
  int authMs = 0,
  bool joined = false,
  bool marker = false,
}) => {
  'id': id,
  'operation': operation,
  'method': 'GET',
  'args': args,
  'at': 1,
  'duration': duration,
  'attempts': 1,
  'limit': 3,
  'status': status,
  'outcome': outcome,
  'retries': <Object?>[],
  'refreshes': refreshes,
  'joined': joined,
  'authMs': authMs,
  'marker': marker,
};

FakeForgeBackend _with(List<Map<String, Object?>> entries) => FakeForgeBackend()
  ..overrides[ForgeDevtoolsProtocol.requests] = (_) => {
    'watching': true,
    'dropped': 0,
    'entries': entries,
  };

void main() {
  testWidgets(
    'lists each request with its outcome, attempts, retries and auth wait',
    (tester) async {
      await pumpPanel(tester);
      await openTab(tester, 'Requests');

      expect(find.textContaining('GET /orders'), findsWidgets);
      expect(find.textContaining('attempts 2 of 3'), findsOneWidget);
      expect(find.textContaining('503 after 200ms'), findsOneWidget);
      expect(
        find.textContaining('No headers or bodies are recorded'),
        findsOneWidget,
      );
      expect(find.textContaining('Authorization'), findsNothing);
    },
  );

  testWidgets('says nothing is recording rather than showing an empty table', (
    tester,
  ) async {
    await pumpPanel(tester, FakeForgeBackend()..watchingRequests = false);
    await openTab(tester, 'Requests');

    expect(
      find.textContaining('Nothing is recording requests'),
      findsOneWidget,
    );
  });

  testWidgets('shows the path, the bounded query, the status and the timing, '
      'newest first', (tester) async {
    await pumpPanel(
      tester,
      _with([
        _request(
          id: 1,
          operation: 'GET /orders',
          args: '{"query":{"page":"2"}}',
          status: 200,
          duration: 31,
        ),
        _request(
          id: 2,
          operation: 'POST /orders',
          status: 500,
          outcome: 'failed',
          duration: 7,
        ),
        _request(
          id: 3,
          operation: 'GET /slow',
          outcome: 'pending',
          duration: null,
        ),
      ]),
    );
    await openTab(tester, 'Requests');

    expect(
      find.textContaining('GET /orders  ok 200  {"query":{"page":"2"}}'),
      findsOneWidget,
    );
    expect(find.textContaining('31ms'), findsOneWidget);
    expect(find.textContaining('POST /orders  failed 500'), findsOneWidget);
    expect(find.textContaining('GET /slow  pending'), findsOneWidget);
    expect(find.textContaining('in flight'), findsOneWidget);

    final ids = tester
        .widgetList<ListTile>(
          find.byWidgetPredicate(
            (w) => w is ListTile && w.key.toString().contains('request-'),
          ),
        )
        .map((tile) => (tile.key! as ValueKey<String>).value);
    expect(ids, ['request-3', 'request-2', 'request-1']);
  });

  testWidgets('says how long a request waited on the auth refresh', (
    tester,
  ) async {
    await pumpPanel(
      tester,
      _with([_request(refreshes: 1, authMs: 40, joined: true)]),
    );
    await openTab(tester, 'Requests');

    expect(find.textContaining('auth refresh 40ms (joined)'), findsOneWidget);
  });

  testWidgets('shows the principal marker as a marker, not as a request', (
    tester,
  ) async {
    await pumpPanel(
      tester,
      _with([
        _request(
          id: -1,
          operation: 'principal changed',
          marker: true,
          duration: null,
        ),
      ]),
    );
    await openTab(tester, 'Requests');

    expect(find.text('identity changed'), findsOneWidget);
    expect(find.textContaining('attempts'), findsNothing);
  });

  testWidgets('shows a request the app would not send as too large', (
    tester,
  ) async {
    await pumpPanel(
      tester,
      _with([
        {'oversized': true, 'field': 'operation', 'seq': 1},
      ]),
    );
    await openTab(tester, 'Requests');

    expect(find.textContaining('too large to show (operation'), findsOneWidget);
  });
}
