import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/seams.dart';
import 'package:test/test.dart';

import 'harness.dart';

void main() {
  group('frame capture', () {
    // TS `frames: {}` is `const FrameOptions()` here: presence switches capture
    // on at the default limit, rather than reading as a limit of zero.
    test('reads a bare `frames: {}` as on, not as a limit of zero', () {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(),
      );

      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      expect(devtools.capturing, isTrue);
      expect(devtools.frames(), hasLength(1));

      devtools.dispose();
    });

    test('is off by default and captures nothing', () {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      expect(devtools.capturing, isFalse);
      expect(devtools.frames(), isEmpty);

      devtools.dispose();
    });

    test('records the channel, the message and the payload when asked', () {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );

      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      final captured = devtools.frames();

      expect(captured, hasLength(1));
      expect(captured.first.channel, '/ws/orders');
      expect(captured.first.message, 'order.updated');
      expect(captured.first.intent, 'upsert');
      expect(captured.first.entity, 'Order');
      expect(captured.first.payload, {'id': 1, 'total': 5});

      devtools.dispose();
    });

    test('copies the payload, so nothing it holds can move the store', () {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );
      final payload = <String, Object?>{'id': 1, 'total': 5};

      debugApplyFrames(h.cache, orderBinding, payload);

      expect(identical(devtools.frames().first.payload, payload), isFalse);

      devtools.dispose();
    });

    test('truncates a deep payload, and says where it stopped', () {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );

      // Eight levels against a depth cap of six.
      var payload = <String, Object?>{'id': 1, 'bottom': true};
      for (var i = 0; i < 8; i++) {
        payload = {'id': 1, 'nested': payload};
      }

      debugApplyFrames(h.cache, orderBinding, payload);

      var node = devtools.frames().first.payload;
      var depth = 0;
      while (node is Map<String, Object?> && node['nested'] != null) {
        node = node['nested'];
        depth++;
      }

      expect(node, '[deeper]');
      expect(depth, lessThanOrEqualTo(7));

      devtools.dispose();
    });

    test('truncates a wide array and a wide object alike', () {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 10),
      );

      // The width cap is 50 in both directions.
      final wide = <String, Object?>{'id': 1};
      for (var i = 0; i < 60; i++) {
        wide['field$i'] = i;
      }

      debugApplyFrames(h.cache, orderBinding, {
        'id': 1,
        'rows': [for (var i = 0; i < 60; i++) i],
        'wide': wide,
      });

      final captured = devtools.frames().first.payload! as Map<String, Object?>;
      final rows = captured['rows']! as List<Object?>;
      final kept = captured['wide']! as Map<String, Object?>;

      // 50 elements plus the marker.
      expect(rows, hasLength(51));
      expect(rows[50], '[10 more]');

      // 61 keys in, 50 kept plus one marker key out.
      expect(kept.keys, hasLength(51));
      expect(kept['[more]'], '[11 more]');
      expect(kept.containsKey('field49'), isFalse);

      devtools.dispose();
    });

    test('is a bounded ring, oldest first, dropping the excess', () {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        frames: const FrameOptions(limit: 2),
      );

      for (final id in [1, 2, 3]) {
        debugApplyFrames(h.cache, orderBinding, {'id': id, 'total': id});
      }

      final captured = devtools.frames();

      expect(captured, hasLength(2));
      expect((captured[0].payload! as Map<String, Object?>)['id'], 2);
      expect((captured[1].payload! as Map<String, Object?>)['id'], 3);
      // New in Dart: the overflow is counted, so a panel can say what it lost.
      expect(devtools.framesDropped, 1);

      devtools.dispose();
    });

    test('turns capture on and off at runtime, which is what the panel toggle does', () {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      devtools.setCapture(const FrameOptions(limit: 5));
      debugApplyFrames(h.cache, orderBinding, {'id': 1});
      expect(devtools.frames(), hasLength(1));
      expect(devtools.framesCapacity, 5);

      devtools.setCapture(null);
      expect(devtools.capturing, isFalse);
      expect(devtools.frames(), isEmpty);

      devtools.dispose();
    });
  });
}
