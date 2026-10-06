@TestOn('vm')
@Tags(['conformance'])
library;

import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:http/http.dart' as http;
import 'package:test/test.dart';

import '../support/harness.dart' show SlowStop;
import '../support/kit.dart';
import 'grove_server.dart';

/// grove's native protocol, the table named up front.
const noteSync = SyncDeclaration(
  protocol: 'grove-crdt',
  entity: 'Note',
  table: 'notes',
  pull: '/sync/pull',
  push: '/sync/push',
  stream: '/sync/stream',
  socket: '/sync/ws',
);

/// foundry's dialect: one table per dataset (`ds1` is `ds_notes`).
const rowsSync = SyncDeclaration(
  protocol: 'grove-crdt',
  entity: 'Note',
  table: null,
  pull: '/api/v1/datasets/{id}/sync/pull',
  push: '/api/v1/datasets/{id}/sync/push',
  dataset: '{id}',
);

/// Records every request and can hold a push before it reaches the server.
final class _Wire extends http.BaseClient {
  _Wire() : _inner = http.Client();

  final http.Client _inner;

  /// Every request made, in order, recorded as it arrives (before a hold): path, authorization and body.
  final List<({String path, String? authorization, String body})> sent = [];

  /// When set, the next push waits for it before it is sent.
  Completer<void>? holdPush;

  /// Completes when a push is waiting on [holdPush].
  Completer<void>? heldPush;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final bytes = await request.finalize().toBytes();
    final copy = http.Request(request.method, request.url)
      ..headers.addAll(request.headers)
      ..bodyBytes = bytes;
    final path = request.url.path;
    final hold = holdPush;

    sent.add((
      path: path,
      authorization: request.headers['authorization'],
      body: utf8.decode(bytes),
    ));

    if (hold != null && path.endsWith('/push')) {
      holdPush = null;
      heldPush?.complete();
      await hold.future;
    }

    return _inner.send(copy);
  }

  @override
  void close() => _inner.close();
}

/// A push debounce so long that a device stays offline until `syncNow`.
const offline = Duration(hours: 1);

final class _Device {
  _Device(this.cache, this.source, [this.wire]);

  final QueryCache cache;
  final GroveSyncSource source;
  final _Wire? wire;
}

Future<void> until(
  bool Function() ok, {
  Duration timeout = const Duration(seconds: 5),
}) async {
  final end = DateTime.now().add(timeout);

  while (!ok()) {
    if (DateTime.now().isAfter(end)) {
      throw StateError('condition not met within $timeout');
    }

    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
}

void main() {
  group(
    'two devices through the Go server',
    skip: GroveServer.skipReason(),
    () {
      late GroveServer server;
      final devices = <_Device>[];

      _Device device(
        Uri root, {
        LiveChannel live = LiveChannel.sse,
        int Function()? nowMs,
        Map<String, CrdtType> types = const {},
        Duration pushDebounce = Duration.zero,
      }) {
        final source = GroveSyncSource(
          declarations: const [noteSync],
          entities: noteEntities,
          bindings: {
            'Note': GroveEntity(codec: const NoteCodec(), types: types),
          },
          baseUrl: root,
          live: live,
          pollInterval: const Duration(hours: 1),
          pushDebounce: pushDebounce,
          nowMs: nowMs,
        );
        final cache = QueryCache(
          transport: NoRest(),
          entities: noteEntities,
          syncSources: [source],
          storage: memoryStorage(),
        )..setPrincipal('user');
        final d = _Device(cache, source);

        devices.add(d);

        return d;
      }

      String? title(_Device d, String id) =>
          d.cache.store.getRecord('Note:$id')?.data['title'] as String?;

      setUp(() async => server = await GroveServer.start());
      tearDown(() async {
        for (final d in devices) {
          await d.cache.dispose();
          d.wire?.close();
        }

        devices.clear();
        await server.stop();
      });

      test('an edit on one device reaches the other over SSE', () async {
        final a = device(server.baseUrl);
        final b = device(server.baseUrl);

        await a.cache.idle;
        await b.cache.idle;
        // B's first sync and SSE connect.
        await Future<void>.delayed(const Duration(milliseconds: 300));
        await a.cache.mutate(
          opUpdateNote,
          const TagContext(path: {'noteId': 'n1'}, body: {'title': 'from a'}),
        );
        await until(() => title(b, 'n1') == 'from a');
      });

      test(
        'the same field edited offline converges to the later write on both',
        () async {
          final now = DateTime.now().millisecondsSinceEpoch;
          final a = device(
            server.baseUrl,
            live: LiveChannel.poll,
            pushDebounce: offline,
            nowMs: () => now,
          );
          final b = device(
            server.baseUrl,
            live: LiveChannel.poll,
            pushDebounce: offline,
            nowMs: () => now + 5000,
          );

          await a.cache.idle;
          await b.cache.idle;
          await Future<void>.delayed(const Duration(milliseconds: 200));
          await a.cache.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'title': 'a'}),
          );
          await b.cache.mutate(
            opUpdateNote,
            const TagContext(
              path: {'noteId': 'n1'},
              body: {'title': 'b later'},
            ),
          );
          await a.source.syncNow();
          await b.source.syncNow();
          await a.source.syncNow();

          expect(title(a, 'n1'), 'b later');
          expect(title(b, 'n1'), 'b later');
          expect(
            resolveFieldValue(
              (await server.state('notes', 'n1'))!.fields['title']!,
            ),
            'b later',
          );
        },
      );

      test(
        'a hook rejection reaches the entity status with the server reason',
        () async {
          await server.rejectField('title');

          final a = device(server.baseUrl, live: LiveChannel.poll);

          await a.cache.idle;
          await Future<void>.delayed(const Duration(milliseconds: 200));
          await a.cache.mutate(
            opUpdateNote,
            const TagContext(
              path: {'noteId': 'n1'},
              body: {'title': 'refused'},
            ),
          );
          await a.source.syncNow();

          final status = await a.source.status('Note').first;

          expect(
            status,
            isA<SyncFailed>().having(
              (f) => (f.error as GroveChangeRejected).reason,
              'reason',
              'title is locked',
            ),
          );
          expect(title(a, 'n1'), 'refused');
        },
      );

      test(
        'counters set offline on two devices add up on both',
        () async {
          final base = DateTime.now().millisecondsSinceEpoch;
          const types = {'view_count': CrdtType.counter};
          final a = device(
            server.baseUrl,
            live: LiveChannel.poll,
            pushDebounce: offline,
            nowMs: () => base,
            types: types,
          );
          final b = device(
            server.baseUrl,
            live: LiveChannel.poll,
            pushDebounce: offline,
            nowMs: () => base + 1000,
            types: types,
          );

          await a.cache.idle;
          await b.cache.idle;
          await Future<void>.delayed(const Duration(milliseconds: 200));
          await a.cache.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'viewCount': 3}),
          );
          await b.cache.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'viewCount': 7}),
          );
          // B syncs first with the later stamp. A then pushes an earlier one,
          // and B's pull cursor is already past it.
          await b.source.syncNow();
          await a.source.syncNow();
          await b.source.syncNow();

          int? views(_Device d) =>
              d.cache.store.getRecord('Note:n1')?.data['viewCount'] as int?;

          expect(views(a), 10);
          expect(views(b), 10);
        },
        // Go parity: crdt.SyncController.HandlePull returns the rows whose
        // stored HLC is past the cursor.
        skip:
            'grove v1.7.0 limitation, not changed by fix/crdt-sync-defects: a '
            'pull returns the rows whose stored HLC is past the cursor, so a '
            'counter pushed with an older stamp than a device has already '
            'synced past is never delivered to it. See grove_crdt '
            'doc/go-parity.md and its convergence_test.dart.',
      );

      test(
        'the devices converge in stamp order, the earlier edit syncing first',
        () async {
          final base = DateTime.now().millisecondsSinceEpoch;
          const types = {'view_count': CrdtType.counter};
          final a = device(
            server.baseUrl,
            live: LiveChannel.poll,
            pushDebounce: offline,
            nowMs: () => base,
            types: types,
          );
          final b = device(
            server.baseUrl,
            live: LiveChannel.poll,
            pushDebounce: offline,
            nowMs: () => base + 1000,
            types: types,
          );

          await a.cache.idle;
          await b.cache.idle;
          await Future<void>.delayed(const Duration(milliseconds: 200));
          await a.cache.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'viewCount': 3}),
          );
          await b.cache.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'viewCount': 7}),
          );
          await a.source.syncNow();
          await b.source.syncNow();
          await a.source.syncNow();

          int? views(_Device d) =>
              d.cache.store.getRecord('Note:n1')?.data['viewCount'] as int?;

          expect(views(a), 10);
          expect(views(b), 10);
        },
      );

      test(
        "a push in flight across a principal switch never moves alice's data "
        "under bob's node",
        () async {
          final wire = _Wire();
          var token = 'alice-token';
          final slow = SlowStop();
          final source = GroveSyncSource(
            declarations: const [rowsSync],
            entities: noteEntities,
            bindings: const {'Note': GroveEntity(codec: NoteCodec())},
            baseUrl: server.baseUrl,
            envelope: camelDtoEnvelope,
            auth: AuthProvider.callbacks(
              credentials: (_) => {'authorization': 'Bearer $token'},
              refresh: () {},
            ),
            httpClient: wire,
            live: LiveChannel.poll,
            pollInterval: const Duration(hours: 1),
            pushDebounce: Duration.zero,
          );
          final cache = QueryCache(
            transport: NoRest(),
            entities: noteEntities,
            // SlowStop keeps alice's fenced run alive while bob is served.
            syncSources: [slow, source],
            storage: memoryStorage(),
          );

          devices.add(_Device(cache, source, wire));

          const dataset = GroveDataset('ds1', table: 'ds_notes');
          List<String> noteKeys() => [
            for (final k in cache.store.keys)
              if (k.startsWith('Note:')) k,
          ];

          cache.setPrincipal('alice');
          await cache.idle;
          await source.join(dataset);
          await until(() => wire.sent.any((r) => r.path.endsWith('/pull')));

          // Alice writes; her push is held before it reaches the server.
          final release = Completer<void>();

          addTearDown(() {
            if (!release.isCompleted) release.complete();

            final held = slow.hold;

            if (held != null && !held.isCompleted) held.complete();
          });
          wire.holdPush = release;
          wire.heldPush = Completer<void>();
          unawaited(
            cache.mutate(
              opUpdateNote,
              const TagContext(
                path: {'noteId': 'ds1:r1'},
                body: {'title': 'alice secret'},
              ),
            ),
          );
          await wire.heldPush!.future.timeout(const Duration(seconds: 5));

          final aliceNode =
              (jsonDecode(wire.sent.last.body)
                      as Map<String, Object?>)['nodeId']!
                  as String;

          expect(noteKeys(), ['Note:ds1:r1']);

          // The switch, with the window held open.
          slow.hold = Completer<void>();
          token = 'bob-token';
          cache.setPrincipal('bob');
          expect(
            noteKeys(),
            isEmpty,
            reason: "no row of alice's in bob's store",
          );

          // Alice's push now reaches the server.
          release.complete();

          // The push landed on the server, attributed to alice's node.
          for (var i = 0; i < 250; i++) {
            if ((await server.state('ds_notes', 'r1'))?.fields['title'] !=
                null) {
              break;
            }

            await Future<void>.delayed(const Duration(milliseconds: 20));
          }

          expect(
            (await server.state('ds_notes', 'r1'))!.fields['title']!.nodeId,
            aliceNode,
          );
          expect(noteKeys(), isEmpty);

          slow.hold!.complete();
          await cache.idle;
          await source.join(dataset);
          await until(
            () =>
                wire.sent.where((r) => r.path.endsWith('/pull')).length >= 2 &&
                wire.sent.any(
                  (r) =>
                      r.authorization == 'Bearer bob-token' &&
                      r.path.endsWith('/pull'),
                ),
          );

          // Bob writes a field of his own.
          await cache.mutate(
            opUpdateNote,
            const TagContext(
              path: {'noteId': 'ds1:r1'},
              body: {'viewCount': 5},
            ),
          );
          await source.syncNow();
          await until(
            () => wire.sent.any(
              (r) =>
                  r.authorization == 'Bearer bob-token' &&
                  r.path.endsWith('/push'),
            ),
          );

          final bobNode =
              (jsonDecode(
                    wire.sent
                        .firstWhere(
                          (r) =>
                              r.authorization == 'Bearer bob-token' &&
                              r.path.endsWith('/push'),
                        )
                        .body,
                  ) as Map<String, Object?>)['nodeId']!
                  as String;

          expect(bobNode, isNot(aliceNode));

          // On the wire: nothing of alice's under bob's credentials or node,
          // nothing of bob's under alice's.
          for (final r in wire.sent) {
            final node = (jsonDecode(r.body) as Map<String, Object?>)['nodeId'];

            if (r.authorization == 'Bearer bob-token') {
              if (node != null) expect(node, bobNode, reason: r.path);
              expect(r.body, isNot(contains('alice secret')), reason: r.path);
            }

            if (node == aliceNode) {
              expect(r.authorization, 'Bearer alice-token', reason: r.path);
            }
          }

          // On the server: every field is attributed to the node that wrote it.
          // The shared dataset has no account scoping, so this checks the
          // attribution, not who may read what.
          final state = (await server.state('ds_notes', 'r1'))!;
          final titleField = state.fields['title']!;

          expect(titleField.nodeId, aliceNode);
          expect(resolveFieldValue(titleField), 'alice secret');

          for (final f in state.fields.entries) {
            if (f.value.nodeId == bobNode) {
              expect(
                resolveFieldValue(f.value),
                isNot('alice secret'),
                reason: "bob's node carries alice's data in ${f.key}",
              );
            }
          }

          expect(state.fields['view_count']?.nodeId, bobNode);
        },
      );
    },
  );
}
