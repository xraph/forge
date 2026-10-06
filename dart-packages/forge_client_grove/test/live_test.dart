import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart'
    show CrdtError, CrdtErrorCode, SseResponse;
import 'package:test/test.dart';

import 'support/harness.dart';

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
}
