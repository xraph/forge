import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart'
    show CrdtError, CrdtErrorCode, SseResponse;
import 'package:http/http.dart' as http;
import 'package:test/test.dart';

import 'support/fake_grove_http.dart';
import 'support/harness.dart';
import 'support/kit.dart';

const _streamSync = SyncDeclaration(
  protocol: 'grove-crdt',
  entity: 'Note',
  table: 'notes',
  pull: '/sync/pull',
  push: '/sync/push',
  stream: '/sync/stream',
);

/// An SSE server: every connection stays open, silent, until the client
/// aborts it or a test hangs it up.
final class _FakeSse {
  final List<Map<String, String>> connects = [];
  final List<StreamController<List<int>>> bodies = [];
  int status = 200;

  Future<SseResponse> connect(
    Uri url,
    Map<String, String> headers,
    Future<void> abort,
  ) async {
    connects.add(headers);

    final body = StreamController<List<int>>();

    bodies.add(body);
    unawaited(abort.then((_) => body.close()));

    if (status != 200) unawaited(body.close());

    return SseResponse(
      status,
      body.stream,
      headers: const {'content-type': 'text/event-stream'},
    );
  }
}

void main() {
  test('an idle recycle of the change stream starts no sync; a drop does', () {
    fakeAsync((async) {
      final sse = _FakeSse();
      final h = Harness(
        declarations: const [_streamSync],
        live: LiveChannel.sse,
        sseConnect: sse.connect,
      );

      int pulls() => h.server.paths.where((p) => p.endsWith('/pull')).length;

      h.cache.setPrincipal('alice');
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 1));
      expect(sse.connects, hasLength(1), reason: 'opens after the first sync');

      final before = pulls();

      // Silent past the 45 s idle timeout, then reconnected.
      async.elapse(const Duration(seconds: 60));
      expect(sse.connects, hasLength(greaterThanOrEqualTo(2)));
      expect(pulls(), before, reason: 'an idle recycle loses nothing');

      // The server hangs up: a real reconnect, which pulls what it missed.
      final connects = sse.connects.length;

      sse.bodies.last.close();
      async.elapse(const Duration(seconds: 40));
      expect(sse.connects.length, greaterThan(connects));
      expect(pulls(), greaterThan(before));
    });
  });

  test('a stream failure is reported; a cancellation at a switch is not', () {
    fakeAsync((async) {
      final sse = _FakeSse();
      final slow = SlowStop();
      var token = 'alice-token';
      final h = Harness(
        declarations: const [_streamSync],
        live: LiveChannel.sse,
        sseConnect: sse.connect,
        before: [slow],
        auth: AuthProvider.callbacks(
          credentials: (_) => {'authorization': 'Bearer $token'},
        ),
      );

      h.cache.setPrincipal('alice');
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 1));
      expect(sse.connects.single['authorization'], 'Bearer alice-token');

      // A refused connection is a failure worth reporting.
      sse.status = 503;
      sse.bodies.last.close();
      async.elapse(const Duration(seconds: 20));
      expect([
        for (final (e, c) in h.errors)
          if (c == 'grove') e,
      ], isNotEmpty);
      sse.status = 200;
      async.elapse(const Duration(seconds: 40));

      final reported = h.errors.length;
      final connected = sse.connects.length;

      // Drop the stream and switch while it waits to reconnect: the reconnect
      // reads credentials for a fenced context and ends quietly.
      sse.bodies.last.close();
      slow.hold = Completer<void>();
      token = 'bob-token';
      h.cache.setPrincipal('bob');
      async.elapse(const Duration(seconds: 60));
      expect(
        sse.connects.length,
        connected,
        reason: 'no reconnect in the window',
      );
      expect(h.errors.length, reported, reason: 'the cancellation is expected');

      slow.hold!.complete();
      async.elapse(const Duration(seconds: 2));

      for (final (e, _) in h.errors) {
        expect(
          e is CrdtError && e.code == CrdtErrorCode.cancelled,
          isFalse,
          reason: 'never reported',
        );
      }

      for (final headers in sse.connects.skip(connected)) {
        expect(headers['authorization'], 'Bearer bob-token');
      }
    });
  });

  group('under a live channel', () {
    int pushes(Harness h) =>
        h.server.paths.where((p) => p.endsWith('/push')).length;

    test('a push refused with a 503 is retried with backoff once the server '
        'recovers', () {
      fakeAsync((async) {
        final sse = _FakeSse();
        final h = Harness(
          declarations: const [_streamSync],
          live: LiveChannel.sse,
          sseConnect: sse.connect,
        );

        h.cache.setPrincipal('alice');
        async.flushMicrotasks();
        async.elapse(const Duration(seconds: 1));
        expect(sse.connects, hasLength(1));

        var failing = true;

        h.server.intercept = (r) => failing && r.isPush
            ? http.Response('busy', 503, headers: fakeHeaders)
            : null;
        unawaited(
          h.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'title': 'x'}),
          ),
        );
        async.elapse(const Duration(seconds: 30));

        final refused = pushes(h);

        expect(refused, greaterThan(0));
        failing = false;
        // The retry delay is capped at five minutes.
        async.elapse(const Duration(minutes: 6));
        expect(pushes(h), greaterThan(refused));
        expect(h.source.replica('Note')!.pendingCount, 0);
        expect(h.server.log.single.field, 'title');
      });
    });

    test('a typed write through replica() is pushed', () {
      fakeAsync((async) {
        final sse = _FakeSse();
        final h = Harness(
          declarations: const [_streamSync],
          live: LiveChannel.sse,
          sseConnect: sse.connect,
        );

        h.cache.setPrincipal('alice');
        async.flushMicrotasks();
        async.elapse(const Duration(seconds: 1));

        final before = pushes(h);

        h.source
            .replica('Note')!
            .incrementCounter('notes', 'n1', 'view_count', 1);
        async.elapse(const Duration(seconds: 1));
        expect(pushes(h), before + 1);
        expect(h.source.replica('Note')!.pendingCount, 0);
        expect(h.server.log.single.field, 'view_count');
        expect(h.record('n1')?['viewCount'], 1);
      });
    });

    test('a remote change alone schedules no push', () {
      fakeAsync((async) {
        final sse = _FakeSse();
        final h = Harness(
          declarations: const [_streamSync],
          live: LiveChannel.sse,
          sseConnect: sse.connect,
        );

        h.cache.setPrincipal('alice');
        async.flushMicrotasks();
        async.elapse(const Duration(seconds: 1));
        h.server.remote('n1', 'title', 'from afar', 1);

        final before = pushes(h);

        // A server hang-up: the reconnect pulls the remote change.
        sse.bodies.last.close();
        async.elapse(const Duration(seconds: 40));
        expect(h.record('n1')?['title'], 'from afar');
        expect(pushes(h), before);
      });
    });

    test('an armed retry never sends anything for alice after the switch to '
        'bob', () {
      fakeAsync((async) {
        final sse = _FakeSse();
        final slow = SlowStop();
        final h = Harness(
          declarations: const [_streamSync],
          live: LiveChannel.sse,
          sseConnect: sse.connect,
          before: [slow],
        );

        h.cache.setPrincipal('alice');
        async.flushMicrotasks();
        async.elapse(const Duration(seconds: 1));
        // The transport tries a push three times (two retries of its own).
        // The third answer is held, so the test knows the moment the run
        // gives up: no request in flight, and the backoff retry just armed.
        var attempts = 0;
        final third = Completer<http.Response?>();

        h.server.intercept = (r) {
          if (!r.isPush) return null;

          attempts++;

          return attempts == 3
              ? third.future
              : http.Response('busy', 503, headers: fakeHeaders);
        };
        unawaited(
          h.mutate(
            opUpdateNote,
            const TagContext(path: {'noteId': 'n1'}, body: {'title': 'x'}),
          ),
        );

        for (var i = 0; i < 600 && attempts < 3; i++) {
          async.elapse(const Duration(milliseconds: 100));
        }

        expect(attempts, 3);

        final statuses = <SyncStatus>[];

        h.source.status('Note').listen(statuses.add);
        third.complete(http.Response('busy', 503, headers: fakeHeaders));
        // No time passes: the retry timer is armed and has not fired.
        async.flushMicrotasks();
        expect(
          statuses.last,
          const Offline(),
          reason: 'the run was interrupted',
        );
        expect(h.server.requests.last.isPush, isTrue, reason: 'none in flight');

        final alice = h.server.requests.firstWhere((r) => r.isPush).nodeId;

        expect(alice, isNotNull);
        h.server.intercept = null;
        slow.hold = Completer<void>();
        h.cache.setPrincipal('bob');

        final sent = h.server.requests.length;

        // Alice's run stays alive, fenced, while the window is held.
        async.elapse(const Duration(minutes: 10));
        expect(
          h.server.requests.skip(sent),
          isEmpty,
          reason: 'nothing is sent in the window',
        );
        slow.hold!.complete();
        async.elapse(const Duration(minutes: 10));

        for (final r in h.server.requests.skip(sent)) {
          expect(r.nodeId, isNot(alice), reason: "bob's run only");
        }

        expect(h.server.log, isEmpty, reason: "alice's edit never pushed");
      });
    });
  });
}
