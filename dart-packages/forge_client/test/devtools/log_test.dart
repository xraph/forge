import 'dart:convert';

import 'package:forge_client/src/devtools/log.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

import 'harness.dart';

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
