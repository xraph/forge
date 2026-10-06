import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/frames.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

const _unset = Object();

FrameCapture _frame(int seq, [Object? payload = _unset]) => FrameCapture(
  seq: seq,
  at: seq,
  channel: '/ws/orders',
  message: 'order.updated',
  intent: 'upsert',
  entity: 'Order',
  payload: identical(payload, _unset) ? {'id': seq} : payload,
);

void main() {
  group('bounded copies', () {
    test('passes primitives through and writes references as __ref', () {
      expect(bounded(7, 10), 7);
      expect(bounded('x', 10), 'x');
      expect(bounded(null, 10), isNull);
      expect(bounded(const EntityRef('Customer:c1'), 10), {
        '__ref': 'Customer:c1',
      });
    });

    test('caps lists and maps at the width and leaves a marker', () {
      final list =
          bounded([for (var i = 0; i < 5; i++) i], 3)! as List<Object?>;
      final map =
          bounded({for (var i = 0; i < 5; i++) 'f$i': i}, 3)!
              as Map<String, Object?>;

      expect(list, [0, 1, 2, '[2 more]']);
      expect(map.keys, ['f0', 'f1', 'f2', '[more]']);
      expect(map['[more]'], '[2 more]');
    });

    test('stops at the depth cap and says where', () {
      Object? nested = 'bottom';
      for (var i = 0; i < 10; i++) {
        nested = {'n': nested};
      }

      var node = bounded(nested, 50);
      var depth = 0;
      while (node is Map<String, Object?>) {
        node = node['n'];
        depth++;
      }

      expect(node, '[deeper]');
      expect(depth, 7);
    });

    test('turns a value JSON cannot carry into its string form', () {
      expect(
        bounded(DateTime.utc(2026, 10, 4), 10),
        DateTime.utc(2026, 10, 4).toString(),
      );
    });

    test('copies rather than aliases', () {
      final source = {'id': 1};

      expect(identical(capture(source), source), isFalse);
    });
  });

  group('the frame ring', () {
    test('keeps exactly its capacity, drops the oldest first, and counts the drops', () {
      final ring = FrameRing(2);

      for (var seq = 1; seq <= 5; seq++) {
        ring.push(_frame(seq));
      }

      expect([for (final f in ring.entries()) f.seq], [4, 5]);
      expect(ring.dropped, 3);
      expect(ring.capacity, 2);
    });

    test('clear empties the ring and resets the drop count', () {
      final ring = FrameRing(1)
        ..push(_frame(1))
        ..push(_frame(2))
        ..clear();

      expect(ring.entries(), isEmpty);
      expect(ring.dropped, 0);
    });

    test('a capacity below one holds one frame', () {
      final ring = FrameRing(0)
        ..push(_frame(1))
        ..push(_frame(2));

      expect([for (final f in ring.entries()) f.seq], [2]);
    });
  });

  group('detachment', () {
    test('mutating the source after a capture does not change the copy', () {
      final inner = {
        'name': 'Ada',
        'tags': ['a', 'b'],
      };
      final source = {
        'id': 1,
        'customer': inner,
        'items': [inner],
      };

      final copy = capture(source)! as Map<String, Object?>;

      inner['name'] = 'Grace';
      (inner['tags']! as List<String>).add('c');
      source['id'] = 2;
      source.remove('items');

      expect(copy, {
        'id': 1,
        'customer': {
          'name': 'Ada',
          'tags': ['a', 'b'],
        },
        'items': [
          {
            'name': 'Ada',
            'tags': ['a', 'b'],
          },
        ],
      });
    });

    test('a frame held by the ring does not move when the payload it came from does', () {
      final live = {
        'id': 7,
        'lines': [1, 2],
      };
      final ring = FrameRing(2)..push(_frame(1, capture(live)));

      live['id'] = 8;
      (live['lines']! as List<int>).clear();

      expect(ring.entries().single.payload, {
        'id': 7,
        'lines': [1, 2],
      });
    });

    test(
      'a value with an enormous string form is cut rather than carried whole',
      () {
        final copy = bounded(_Huge(), 10)! as String;

        expect(copy.length, lessThan(1100));
        expect(copy, endsWith('...'));
      },
    );

    test('a set is written as text, not walked', () {
      expect(bounded({1, 2}, 10), '{1, 2}');
    });
  });

  group('purging on a principal change', () {
    test(
      'drops every frame and leaves one marker with no payload and no names',
      () {
        final ring = FrameRing(3);

        for (var seq = 1; seq <= 5; seq++) {
          ring.push(_frame(seq, {'owner': 'alice', 'id': seq}));
        }

        ring.purge(seq: 9, at: 10);

        final held = ring.entries();

        expect(held, hasLength(1));
        expect(held.single.toJson(), {
          'seq': 9,
          'at': 10,
          'channel': '',
          'message': '',
          'intent': 'principal',
          'entity': '',
          'payload': null,
        });
        expect(ring.dropped, 0);
        expect(
          jsonEncode([for (final f in held) f.toJson()]),
          isNot(contains('alice')),
        );
        expect(
          jsonEncode([for (final f in held) f.toJson()]),
          isNot(contains('/ws/orders')),
        );
      },
    );

    test('keeps working afterwards, and overwriting the marker is not a dropped frame', () {
      final ring = FrameRing(2)
        ..purge(seq: 1, at: 1)
        ..push(_frame(2, null))
        ..push(_frame(3, null));

      expect([for (final f in ring.entries()) f.seq], [2, 3]);
      expect(ring.dropped, 0);

      ring.push(_frame(4, null));

      expect([for (final f in ring.entries()) f.seq], [3, 4]);
      expect(ring.dropped, 1);
    });

    test('a ring of one holds the marker, then the next frame replaces it', () {
      final ring = FrameRing(1)
        ..push(_frame(1, null))
        ..purge(seq: 2, at: 2);

      expect(ring.entries().single.intent, 'principal');

      ring.push(_frame(3, null));

      expect(ring.entries().single.seq, 3);
      expect(ring.dropped, 0);
    });
  });
}

final class _Huge {
  @override
  String toString() => 'x' * 100000;
}
