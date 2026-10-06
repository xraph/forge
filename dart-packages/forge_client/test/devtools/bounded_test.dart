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

  // Devtools runs inside the user's app, so no value the app hands over may
  // make a capture hang or exhaust memory.
  group('hostile values', () {
    test(
      'a list that contains itself ends at the repeat with a cycle marker',
      () {
        final cyclic = <Object?>[];
        for (var i = 0; i < 50; i++) {
          cyclic.add(cyclic);
        }

        final watch = Stopwatch()..start();
        final copy = capture(cyclic)! as List<Object?>;

        expect(watch.elapsed, lessThan(const Duration(seconds: 2)));
        expect(copy, hasLength(50));
        expect(copy, everyElement('[cycle]'));
      },
    );

    test(
      'a map that contains itself, and a cycle through a child, are cut too',
      () {
        final map = <String, Object?>{};
        map['self'] = map;
        final parent = <String, Object?>{};
        final child = <Object?>[parent];
        parent['child'] = child;

        expect(capture(map), {'self': '[cycle]'});
        expect(capture(parent), {
          'child': ['[cycle]'],
        });
      },
    );

    test('the same container twice, with no cycle, is copied both times', () {
      final shared = [1, 2];

      expect(capture([shared, shared]), [
        [1, 2],
        [1, 2],
      ]);
    });

    test('a shared subtree is charged each time it is visited, so a wide DAG stays inside the budget', () {
      // Seven levels of fifty references to one child: about 7.8e11 nodes
      // walked naively, and an out of memory abort at width 20.
      Object? level = [1, 2, 3];
      for (var i = 0; i < 7; i++) {
        level = [for (var j = 0; j < 50; j++) level];
      }
      final watch = Stopwatch()..start();
      final wide = bounded(level, 20)!;
      final small = capture(level)!;

      expect(watch.elapsed, lessThan(const Duration(seconds: 2)));
      expect(_nodes(wide), lessThanOrEqualTo(5000 + 5000 ~/ 2));
      expect(_nodes(small), lessThanOrEqualTo(5000 + 5000 ~/ 2));
    });

    test('a four level DAG, fifty wide, is cut at the budget', () {
      Object? level = 'leaf';
      for (var i = 0; i < 4; i++) {
        level = [for (var j = 0; j < 50; j++) level];
      }

      final watch = Stopwatch()..start();
      final copy = capture(level)!;

      expect(watch.elapsed, lessThan(const Duration(seconds: 2)));
      expect(_nodes(copy), lessThanOrEqualTo(5000 + 100));
      expect(jsonEncode(copy), contains('more]'));
    });

    test('running out of budget leaves an N-more marker where the walk stopped', () {
      final copy =
          bounded([for (var i = 0; i < 10; i++) i], 100, 0, 4)!
              as List<Object?>;

      // The root costs one, three elements cost three, then the budget is gone.
      expect(copy, [0, 1, 2, '[7 more]']);

      final map =
          bounded({for (var i = 0; i < 10; i++) 'k$i': i}, 100, 0, 3)!
              as Map<String, Object?>;

      expect(map.keys, ['k0', 'k1', '[more]']);
      expect(map['[more]'], '[8 more]');
    });

    test('a five megabyte string is cut and its tail is not retained', () {
      final big = 'x' * (5 * 1024 * 1024);
      final copy =
          capture({'blob': big, big.substring(0, 2000): 1})!
              as Map<String, Object?>;
      final blob = copy['blob']! as String;

      expect(blob.length, 1003);
      expect(blob, endsWith('...'));
      expect(copy.keys.last.length, 1003);
      expect(capture('short'), 'short');
      expect(capture('y' * 1000), 'y' * 1000);
    });

    test('a number JSON cannot carry becomes null rather than breaking the encoder', () {
      final copy = capture({'a': double.nan, 'b': double.infinity, 'c': 1.5});

      expect(jsonEncode(copy), '{"a":null,"b":null,"c":1.5}');
    });
  });

  group('the copy cannot be written to', () {
    test('every list and map in it is unmodifiable', () {
      final copy =
          capture({
                'list': [
                  1,
                  {'inner': 1},
                ],
                'ref': const EntityRef('Customer:c1'),
              })!
              as Map<String, Object?>;

      expect(() => copy['x'] = 1, throwsUnsupportedError);
      expect(
        () => (copy['list']! as List<Object?>).add(1),
        throwsUnsupportedError,
      );
      expect(
        () =>
            ((copy['list']! as List<Object?>)[1]!
                    as Map<String, Object?>)['inner'] =
                2,
        throwsUnsupportedError,
      );
      expect(
        () => (copy['ref']! as Map<String, Object?>)['__ref'] = 'x',
        throwsUnsupportedError,
      );
    });

    test('a payload returned by the ring cannot be tampered with, and the ring is unchanged', () {
      final ring = FrameRing(2)
        ..push(
          _frame(1, {
            'id': 7,
            'lines': [1, 2],
          }),
        );

      final payload = ring.entries().single.payload! as Map<String, Object?>;

      expect(() => payload['id'] = 'TAMPERED', throwsUnsupportedError);
      expect(
        () => (payload['lines']! as List<Object?>).add(99),
        throwsUnsupportedError,
      );
      expect(ring.entries().single.payload, {
        'id': 7,
        'lines': [1, 2],
      });
    });

    test(
      'push never keeps a live payload, and keeps an existing capture as it is',
      () {
        final live = {
          'id': 1,
          'lines': [1],
        };
        final captured = capture({'id': 2})!;
        final ring = FrameRing(3)
          ..push(_frame(1, live))
          ..push(_frame(2, captured));

        live['id'] = 99;
        (live['lines']! as List<int>).add(5);

        final held = ring.entries();

        expect(held[0].payload, {
          'id': 1,
          'lines': [1],
        });
        expect(identical(held[1].payload, captured), isTrue);
      },
    );

    test('a prior capture nested in a new value is walked and counted like any other', () {
      Object? grid = 'leaf';
      for (var i = 0; i < 4; i++) {
        grid = [for (var j = 0; j < 50; j++) grid];
      }
      final prior = capture(grid)!;
      final wrapped = [for (var i = 0; i < 50; i++) prior];

      final watch = Stopwatch()..start();
      final copy = capture(wrapped)!;

      expect(watch.elapsed, lessThan(const Duration(seconds: 2)));
      expect(_nodes(copy), lessThanOrEqualTo(5000 + 100));

      Object? deep = 'bottom';
      for (var i = 0; i < 6; i++) {
        deep = {'n': deep};
      }
      var nested = <String, Object?>{'n': capture(deep)};
      for (var i = 0; i < 4; i++) {
        nested = {'n': nested};
      }

      var node = capture(nested);
      var levels = 0;
      while (node is Map<String, Object?>) {
        node = node['n'];
        levels++;
      }

      expect(levels, 7);
      expect(node, '[deeper]');
    });

    test(
      'capturing a capture changes nothing, truncation markers included',
      () {
        final once = capture([for (var i = 0; i < 60; i++) i])!;

        expect(identical(capture(once), once), isTrue);
        expect((once as List<Object?>).last, '[10 more]');
      },
    );
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

    test('keeps working afterwards, and overwriting the marker counts as a drop like any entry', () {
      final ring = FrameRing(2)
        ..purge(seq: 1, at: 1)
        ..push(_frame(2, null))
        ..push(_frame(3, null));

      expect([for (final f in ring.entries()) f.seq], [2, 3]);
      expect(ring.dropped, 1);

      ring.push(_frame(4, null));

      expect([for (final f in ring.entries()) f.seq], [3, 4]);
      expect(ring.dropped, 2);
    });

    test('a ring of one holds the marker, then the next frame replaces it', () {
      final ring = FrameRing(1)
        ..push(_frame(1, null))
        ..purge(seq: 2, at: 2);

      expect(ring.entries().single.intent, 'principal');

      ring.push(_frame(3, null));

      expect(ring.entries().single.seq, 3);
      expect(ring.dropped, 1);
    });
  });
}

/// How many values a copy holds, markers included.
int _nodes(Object? value) => switch (value) {
  final List<Object?> list => 1 + list.fold(0, (sum, e) => sum + _nodes(e)),
  final Map<Object?, Object?> map =>
    1 + map.values.fold(0, (sum, e) => sum + _nodes(e)),
  _ => 1,
};

final class _Huge {
  @override
  String toString() => 'x' * 100000;
}
