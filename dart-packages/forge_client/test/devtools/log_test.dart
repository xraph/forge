import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/log.dart';
import 'package:forge_client/src/devtools/seams.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

import 'harness.dart';

const _one = TagContext(path: {'id': 1});

void main() {
  // An event log is a memory leak by default. These are the tests that say
  // this one is not.
  group('the log is bounded', () {
    test('holds exactly its capacity and counts what it dropped', () {
      final log = EventLog(capacity: 4, clock: CounterClock());

      for (var i = 0; i < 10; i++) {
        log.push(
          (seq, at) =>
              SettleLog(seq: seq, at: at, session: 0, query: 'q$i', version: i),
        );
      }

      final held = log.entries();

      expect(log.capacity, 4);
      expect(held, hasLength(4));
      expect(log.dropped, 6);

      // Oldest first, and the oldest is the seventh thing pushed: the ring is
      // a window on the recent past, not a recording.
      expect(
        [for (final entry in held) (entry as SettleLog).query],
        ['q6', 'q7', 'q8', 'q9'],
      );

      // The sequence keeps counting across the wrap, so a cause pointing at a
      // dropped entry is recognisable rather than pointing at its replacement.
      expect(held.first.seq, 7);
      expect(log.find(1), isNull);
    });

    test(
      'survives a capacity of zero rather than writing outside the ring',
      () {
        final log = EventLog(capacity: 0, clock: CounterClock());

        log.push((seq, at) => PrincipalLog(seq: seq, at: at, session: 1));
        log.push((seq, at) => PrincipalLog(seq: seq, at: at, session: 2));

        expect(log.capacity, 1);
        expect(log.entries(), hasLength(1));
        expect(log.dropped, 1);
      },
    );

    test(
      'stops growing under a load that would otherwise grow it without bound',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, limit: 32, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        for (var i = 0; i < 200; i++) {
          await h.cache.mutate(Ops.orderUpdate, _one);
          h.flush();
          await h.settle();
        }

        expect(devtools.log(), hasLength(32));
        expect(devtools.dropped, greaterThan(500));

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('keeps no response body, no error object and no rehydrated value', () async {
      final h = Harness()
        ..reply('POST /orders', {'id': 9, 'secret': 'x' * 5000});
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      await h.cache.mutate(
        Ops.orderCreate,
        TagContext(body: {'note': 'y' * 5000}),
      );
      h.flush();
      await h.settle();

      final serialised = jsonEncode([
        for (final entry in devtools.log()) entry.toJson(),
      ]);

      // The response never reaches the log, and the arguments are truncated to
      // a cache key.
      expect(serialised, isNot(contains('x' * 50)));
      expect(serialised.length, lessThan(3000));

      final mutation = devtools.log().whereType<MutationLog>().first;

      expect(mutation.args, endsWith('...'));
      expect(mutation.args.length, lessThan(220));

      await sub.cancel();
      devtools.dispose();
    });

    test('prunes its per-query bookkeeping instead of growing one entry per query key', () async {
      final h = Harness();
      final devtools = attach(h.cache, limit: 16, clock: CounterClock());

      // A search box: one distinct query key per keystroke, most of them
      // evicted by the cache's own LRU cap.
      for (var i = 0; i < 700; i++) {
        await h.cache.fetch(Ops.orderGet, TagContext(path: {'id': i}));
      }
      await h.settle();

      expect(devtools.capacity, 16);
      expect(devtools.log(), hasLength(16));
      // The cache's own cap held: the limit plus the record just asked for.
      expect(h.dev.tracked, lessThanOrEqualTo(129));

      devtools.dispose();
    });

    test('clear forgets the events and leaves the cache alone', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final records = h.dev.records;

      expect(devtools.log(), isNotEmpty);

      devtools.clear();

      expect(devtools.log(), isEmpty);
      expect(devtools.dropped, 0);
      expect(h.dev.records, records);

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('the identity boundary', () {
    // The standing rule is that nothing crosses principals. The plan this
    // case came from asserted that the first user's entries survive a switch,
    // as a timeline divided by session. That is the leak: the log holds
    // argument keys, operation names and query keys, which carry one user's
    // values into the next user's panel. A switch purges it.
    test('purges everything recorded for the previous principal and leaves one marker', () async {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );
      final sub = h.mount(Ops.orderList);
      await h.settle();
      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();
      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      expect(h.dev.records, greaterThan(0));

      // The premise: there is something of the first principal to purge.
      final before = devtools.log();

      expect(before.whereType<MutationLog>(), isNotEmpty);
      expect(before.whereType<InvalidatedLog>(), isNotEmpty);
      expect(devtools.frames(), hasLength(1));

      h.cache.setPrincipal('user-2');
      await h.settle();

      final log = devtools.log();
      final markers = log.whereType<PrincipalLog>().toList();

      expect(devtools.session, 1);
      expect(markers, hasLength(1));
      expect(log.first, same(markers.single));
      expect(log.every((entry) => entry.session == 1), isTrue);

      // Nothing from before the switch is reachable by sequence either.
      for (final entry in before) {
        expect(
          devtools.eventLog.find(entry.seq),
          isNull,
          reason: 'seq ${entry.seq} survived the switch',
        );
      }
      expect(log.whereType<MutationLog>(), isEmpty);
      expect(log.whereType<InvalidatedLog>(), isEmpty);
      expect(devtools.lastCause(), isNull);

      // The frame ring kept one marker and no payload.
      final frames = devtools.frames();

      expect(devtools.capturing, isTrue);
      expect(frames, hasLength(1));
      expect(frames.single.intent, 'principal');
      expect(frames.single.payload, isNull);
      expect(frames.single.channel, isEmpty);
      expect(frames.single.seq, markers.single.seq);

      // The mounted query re-fetched for the new principal, and is logged as
      // a mount rather than as a refetch of data that no longer exists.
      final after = log.whereType<FetchLog>().first;

      expect(after.reason, FetchReason.mount);
      expect(after.cause, isNull);

      // The recorder carries on for the second principal.
      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      expect(devtools.log().whereType<MutationLog>(), hasLength(1));
      expect(devtools.log().whereType<MutationLog>().single.session, 1);

      await sub.cancel();
      devtools.dispose();
    });

    test('purges before the cache empties, so nothing reads the previous principal back out', () async {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );
      final sub = h.mount(Ops.orderList);
      await h.settle();
      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      // Registered after the recorder's own, so it runs after it.
      var records = -1;
      var logged = -1;
      var captured = -1;
      final stop = h.cache.watchPrincipalChanging((_) {
        records = h.dev.records;
        logged = devtools.log().length;
        captured = devtools.frames().length;
      });

      h.cache.setPrincipal('user-2');
      stop();

      // The store still held the first principal's records at that moment,
      // and the recorder had already let go of everything it recorded.
      expect(records, greaterThan(0));
      expect(logged, 1);
      expect(captured, 1);
      expect(devtools.log().first, isA<PrincipalLog>());

      await sub.cancel();
      devtools.dispose();
    });

    test(
      'explains a miss for the second principal without quoting the first',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        await h.cache.mutate(
          Ops.orderCreate,
          const TagContext(body: {'total': 30}),
        );
        h.flush();
        await h.settle();

        expect(
          devtools.whyNotRefetched(h.key(Ops.orderList)).cause.label,
          'mutation POST /orders',
        );

        h.cache.setPrincipal('user-2');
        await h.settle();

        final report = devtools.whyNotRefetched(h.key(Ops.orderList));

        expect(report.cause.label, contains('no mutation or frame batch'));
        expect(report.cause.tags, isEmpty);

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('a secret the first principal wrote is reachable nowhere once the second is in', () async {
      const secret = 'alice-ssn';
      final h = Harness()..reply('POST /orders', {'id': 7, 'secret': secret});
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );
      final heard = <LogEntry>[];

      devtools.subscribe(heard.add);

      final sub = h.mount(Ops.orderList);
      await h.settle();

      // Alice writes a record carrying the secret, three ways: a request body,
      // a stream frame, and a query key built from a path value.
      await h.cache.mutate(
        Ops.orderCreate,
        const TagContext(body: {'id': 7, 'secret': secret}),
      );
      debugApplyFrames(h.cache, orderBinding, {'id': 7, 'secret': secret});
      await h.cache.fetch(Ops.orderGet, const TagContext(path: {'id': secret}));
      h.flush();
      await h.settle();

      String everything() => jsonEncode([
        [for (final entry in devtools.log()) entry.toJson()],
        [for (final entry in devtools.eventLog.entries()) entry.toJson()],
        [for (final frame in devtools.frames()) frame.toJson()],
        devtools.lastCause()?.toJson(),
        devtools.whyRefetched(h.key(Ops.orderList))?.toJson(),
        devtools.whyNotRefetched(h.key(Ops.orderList)).toJson(),
        devtools.explain(h.key(Ops.orderList)).toJson(),
        devtools
            .wouldInvalidate(Ops.orderCreate, const TagContext(body: {}))
            .toJson(),
      ]);

      // The premise: the planted string is in what the recorder holds, in the
      // log (request body and query key) and in the frame ring.
      final seen = devtools.log();

      expect(seen.whereType<MutationLog>().single.args, contains(secret));
      expect(
        seen.whereType<FetchLog>().any((entry) => entry.query.contains(secret)),
        isTrue,
      );
      expect(devtools.frames().single.payload, containsPair('secret', secret));
      expect(everything(), contains(secret));

      final sequences = [for (final entry in seen) entry.seq];
      final heardBefore = heard.length;

      h.cache.setPrincipal('bob');
      await h.settle();
      h.flush();
      await h.settle();

      // Not in the log, the ring, any report, or what a subscriber heard.
      expect(everything(), isNot(contains(secret)));
      expect(
        jsonEncode([
          for (final entry in heard.sublist(heardBefore)) entry.toJson(),
        ]),
        isNot(contains(secret)),
      );

      // Nor its entries, its frame, or its ids.
      for (final seq in sequences) {
        expect(devtools.eventLog.find(seq), isNull);
      }
      expect(devtools.log().whereType<MutationLog>(), isEmpty);
      expect(devtools.log().whereType<FramesLog>(), isEmpty);
      expect(
        devtools.frames().every((frame) => frame.intent == 'principal'),
        isTrue,
      );
      expect(devtools.frames().every((frame) => frame.payload == null), isTrue);
      expect(devtools.lastCause(), isNull);

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('attaching and detaching', () {
    // The core keeps no history when nobody reads one: run traffic with
    // nothing attached, attach afterwards, and find the log empty.
    test('finds nothing waiting, because the core buffered nothing', () async {
      final h = Harness();
      final sub = h.mount(Ops.orderList);
      await h.settle();

      for (var i = 0; i < 50; i++) {
        await h.cache.mutate(Ops.orderUpdate, _one);
        h.flush();
        await h.settle();
      }

      expect(h.cache.observer, isNull);

      final devtools = attach(h.cache, clock: CounterClock());

      expect(devtools.log(), isEmpty);
      expect(devtools.dropped, 0);

      await sub.cancel();
      devtools.dispose();
    });

    test('restores the previous observer and stops recording', () async {
      final h = Harness();
      final seen = <String>[];
      h.cache.observer = (event) => seen.add(event.runtimeType.toString());

      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      // Chained, not replaced: an existing observer keeps receiving events.
      expect(seen, isNotEmpty);
      expect(devtools.log(), isNotEmpty);

      final recorded = devtools.log().length;

      devtools.dispose();

      expect(h.cache.observer, isNotNull);

      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      expect(devtools.log(), hasLength(recorded));

      await sub.cancel();
      h.cache.observer = null;
    });

    test('does not unhook a second inspector that took the slot', () async {
      final h = Harness();
      final first = attach(h.cache, clock: CounterClock());
      final second = attach(h.cache, clock: CounterClock());

      first.dispose();

      final sub = h.mount(Ops.orderList);
      await h.settle();

      expect(second.log(), isNotEmpty);

      final held = second.log().length;

      second.dispose();

      // The first observer is still in the chain, inert, so neither ring grows.
      expect(h.cache.observer, isNotNull);

      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      expect(first.log(), isEmpty);
      expect(second.log(), hasLength(held));

      await sub.cancel();
    });

    test('stops listening for identity changes when disposed', () async {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );
      final sub = h.mount(Ops.orderList);
      await h.settle();
      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      final log = devtools.log();
      final frames = devtools.frames();

      devtools.dispose();
      h.cache.setPrincipal('user-2');
      await h.settle();

      // A recorder still subscribed would have purged what it holds, left a
      // marker, and started a second session. A detached one is inert.
      expect(devtools.session, 0);
      expect(devtools.log().map((entry) => entry.seq), [
        for (final entry in log) entry.seq,
      ]);
      expect(devtools.log().whereType<PrincipalLog>(), isEmpty);
      expect(devtools.frames(), hasLength(frames.length));
      expect(devtools.frames().single.intent, isNot('principal'));

      await sub.cancel();
    });

    test(
      'a disposed inspector among two does not stop the other from purging',
      () async {
        final h = Harness();
        final first = attach(h.cache, clock: CounterClock());
        final second = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        first.dispose();
        h.cache.setPrincipal('user-2');
        await h.settle();

        expect(second.log().first, isA<PrincipalLog>());
        expect(second.log().whereType<MutationLog>(), isEmpty);
        expect(second.session, 1);
        expect(first.session, 0);

        await sub.cancel();
        second.dispose();
      },
    );
  });

  group('reading the ring', () {
    test(
      'finds by sequence, searches backwards, and keeps counting after a clear',
      () {
        final log = EventLog(capacity: 3, clock: CounterClock());

        for (var i = 0; i < 5; i++) {
          log.push(
            (seq, at) => SettleLog(
              seq: seq,
              at: at,
              session: 0,
              query: 'q${i % 2}',
              version: i,
            ),
          );
        }

        expect(log.find(4)?.seq, 4);
        expect(
          (log.last(
            (entry) => entry is SettleLog && entry.query == 'q0',
          ) as SettleLog?)?.version,
          4,
        );
        expect(
          (log.last(
            (entry) => entry is SettleLog && entry.query == 'q1',
          ) as SettleLog?)?.version,
          3,
        );

        log.clear();

        expect(log.entries(), isEmpty);
        expect(log.dropped, 0);
        expect(
          log.push((seq, at) => PrincipalLog(seq: seq, at: at, session: 0)).seq,
          6,
        );
      },
    );

    test('a throwing listener neither stops the push nor starves the next listener', () {
      final log = EventLog(clock: CounterClock());
      final heard = <int>[];

      log.subscribe((_) => throw StateError('panel bug'));
      final stop = log.subscribe((entry) => heard.add(entry.seq));

      log.push((seq, at) => PrincipalLog(seq: seq, at: at, session: 0));
      stop();
      log.push((seq, at) => PrincipalLog(seq: seq, at: at, session: 0));

      expect(heard, [1]);
      expect(log.entries(), hasLength(2));
    });
  });

  group('what the ring holds is detached from what it was given', () {
    test(
      'application code mutating a list afterwards does not change the entry',
      () {
        final log = EventLog(clock: CounterClock());
        final tags = ['Order:1', 'Order[]'];
        final unresolved = ['Order[]:{res.ref}'];

        final returned = log.push(
          (seq, at) => MutationLog(
            seq: seq,
            at: at,
            session: 0,
            operation: 'PATCH /orders/{id}',
            args: '{}',
            tags: tags,
            unresolved: unresolved,
          ),
        );

        tags
          ..add('Customer:9')
          ..removeAt(0);
        unresolved.clear();

        final held = log.entries().single as MutationLog;

        expect(held.tags, ['Order:1', 'Order[]']);
        expect(held.unresolved, ['Order[]:{res.ref}']);
        expect(returned.tags, ['Order:1', 'Order[]']);
        expect(() => held.tags.add('x'), throwsUnsupportedError);
      },
    );

    test('the same holds for the matched tags of an invalidation and the tags of a frame batch', () {
      final log = EventLog(clock: CounterClock());
      final matched = ['Order[]'];
      final raised = ['Order:2'];

      log
        ..push(
          (seq, at) => InvalidatedLog(
            seq: seq,
            at: at,
            session: 0,
            query: 'q',
            matched: matched,
            cause: 1,
          ),
        )
        ..push(
          (seq, at) =>
              FramesLog(seq: seq, at: at, session: 0, frames: 1, tags: raised),
        );
      matched.add('Order:99');
      raised.clear();

      final held = log.entries();

      expect((held[0] as InvalidatedLog).matched, ['Order[]']);
      expect((held[1] as FramesLog).tags, ['Order:2']);
    });
  });

  // The standing rule is that nothing crosses principals. A recording of one
  // user's activity must not be readable by the next.
  group('purging on a principal change', () {
    test(
      'drops every earlier entry and leaves one marker with nothing in it',
      () {
        final log = EventLog(capacity: 4, clock: CounterClock());

        for (var i = 0; i < 6; i++) {
          log.push(
            (seq, at) => MutationLog(
              seq: seq,
              at: at,
              session: 0,
              operation: 'PATCH /orders/alice-$i',
              args: '{"path":{"id":"alice-$i"}}',
              tags: ['Order:alice-$i'],
              unresolved: const [],
            ),
          );
        }

        final marker = log.purge(session: 1);
        final held = log.entries();

        expect(held, hasLength(1));
        expect(held.single, same(marker));
        expect(marker, isA<PrincipalLog>());
        expect(marker.session, 1);
        expect(marker.toJson(), {
          'kind': 'principal',
          'seq': 7,
          'at': 7,
          'session': 1,
        });
        expect(log.size, 1);
        expect(log.dropped, 0);
      },
    );

    test(
      'nothing recorded before the switch is reachable through any ring api',
      () {
        final log = EventLog(capacity: 3, clock: CounterClock());

        for (var i = 0; i < 5; i++) {
          log.push(
            (seq, at) => SettleLog(
              seq: seq,
              at: at,
              session: 0,
              query: 'alice-$i',
              version: i,
            ),
          );
        }

        final before = [for (final entry in log.entries()) entry.seq];

        log.purge(session: 1);

        for (final seq in before) {
          expect(log.find(seq), isNull, reason: 'seq $seq survived the purge');
        }
        expect(log.last((entry) => entry is SettleLog), isNull);
        expect(log.last((entry) => true), isA<PrincipalLog>());
        expect(
          jsonEncode([for (final entry in log.entries()) entry.toJson()]),
          isNot(contains('alice')),
        );
      },
    );

    test('keeps counting, keeps its listeners, and only tells them about the marker', () {
      final log = EventLog(clock: CounterClock());
      final heard = <String>[];

      log.subscribe((entry) => heard.add(entry.kind));
      log
        ..push(
          (seq, at) =>
              SettleLog(seq: seq, at: at, session: 0, query: 'q', version: 1),
        )
        ..purge(session: 1)
        ..push(
          (seq, at) =>
              SettleLog(seq: seq, at: at, session: 1, query: 'q', version: 2),
        );

      expect(heard, ['settle', 'principal', 'settle']);
      expect([for (final entry in log.entries()) entry.seq], [2, 3]);
    });

    test(
      'a purge on an empty or tiny ring still leaves exactly the marker',
      () {
        final log = EventLog(capacity: 1, clock: CounterClock())
          ..purge(session: 1)
          ..purge(session: 2);

        expect(log.entries().map((entry) => entry.session), [2]);
      },
    );
  });
}
