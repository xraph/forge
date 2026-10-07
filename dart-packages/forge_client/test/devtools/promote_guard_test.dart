// New in Dart: the devtools' "promote this layer" honours the guard the cache
// applies when a mutation settles. An embedded entity a stream frame wrote
// after the layer was pushed, or one a sync source owns, is not the
// promotion's to write.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'harness.dart';

/// A layer that renames `Order:1`'s embedded customer through the order, as
/// a typed `OptimisticUpdate` does.
int renameThroughOrder(Harness h) => h.cache.overlays.add({
  'Order:1': MergePatch.computed(
    (previous) => {
      ...previous,
      'customer': {'id': 'c1', 'name': 'Ada L.'},
    },
  ),
});

void main() {
  test(
    'leaves an embedded entity a frame wrote after the push alone',
    () async {
      final h = Harness();
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final id = renameThroughOrder(h);

      h.cache.store.write(
        {'id': 'c1', 'name': 'From a frame'},
        h.cache.entities,
        'Customer',
        CommitOptions(frameAt: h.cache.store.nextFrame()),
      );

      expect(h.dev.promoteOverlay(id), isTrue);
      expect(h.dev.record('Customer:c1')!.data['name'], 'From a frame');
      expect(h.dev.record('Order:1')!.data['customer'], isA<EntityRef>());

      await sub.cancel();
    },
  );

  test('writes the embedded edit when nothing overtook it', () async {
    final h = Harness();
    final sub = h.mount(Ops.orderList);
    await h.settle();

    final id = renameThroughOrder(h);

    expect(h.dev.promoteOverlay(id), isTrue);
    expect(h.dev.record('Customer:c1')!.data['name'], 'Ada L.');

    await sub.cancel();
  });
}
