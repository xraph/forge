// A fold memoized for one identity must never answer for the next: the stack's
// memo has to go with the records it was computed from, on a principal
// change, on `clear()` and on a store clear. These read through the public
// surface as well as the stack itself.
import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

const _secret = 'alice-ssn';

const _orderList = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);

const _orderPatch = OperationMeta(
  id: 'orderPatch',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Order[]'],
);

({EntityStore store, OverlayStack stack}) _host() {
  final store = EntityStore();
  final stack = OverlayStack(store);

  store.overlays = stack;

  return (store: store, stack: stack);
}

QueryCache _cache(
  FutureOr<Object?> Function(TransportRequest request, int call) handler,
) => QueryCache(
  transport: FakeTransport(handler),
  entities: schema,
  scheduler: ManualScheduler(),
);

/// Every way a caller can read an order out of the cache, as one string.
String _everything(QueryCache cache) => jsonEncode([
  cache.getState(_orderList, TagContext.empty).dataOrNull,
  cache.store.read(makeRef('Order:7')),
  cache.store.getRecord('Order:7')?.data,
  cache.overlays.effective('Order:7')?.data,
], toEncodable: (Object? o) => '$o');

void main() {
  group('the overlay stack\'s fold memo', () {
    test('does not keep a base record past a store clear', () {
      final (:store, :stack) = _host();

      store.put('Order:7', {'id': 7, 'ssn': _secret});

      expect(stack.effective('Order:7')!.data['ssn'], _secret);

      store.clear();

      expect(stack.effective('Order:7'), isNull);
    });

    test('does not keep a held record past a store clear either', () {
      final (:store, :stack) = _host();

      store.put('Order:7', {'id': 7, 'ssn': _secret});
      stack.add({
        'Order:7': const MergePatch({'status': 'shipped'}),
      });

      expect(stack.effective('Order:7')!.data['ssn'], _secret);

      store.clear();

      // A merge over a hole patches nothing.
      expect(stack.effective('Order:7'), isNull);
    });
  });

  group(
    'no public read serves the previous identity after the memo was used',
    () {
      for (final (name, switchAway) in <(String, void Function(QueryCache))>[
        ('setPrincipal', (cache) => cache.setPrincipal('bob')),
        ('clear', (cache) => cache.clear()),
      ]) {
        test('with no layer pending, across $name', () async {
          final cache = _cache(
            (request, call) => call == 0
                ? [
                    {'id': 7, 'ssn': _secret},
                  ]
                : <Object?>[],
          );

          cache.setPrincipal('alice');
          await cache.fetch(_orderList, TagContext.empty);

          // The read a folded view makes, on a record nothing overlays.
          expect(_everything(cache), contains(_secret));

          switchAway(cache);
          await settle();

          expect(_everything(cache), isNot(contains(_secret)));
          expect(cache.overlays.effective('Order:7'), isNull);
        });

        test('with a layer pending, across $name', () async {
          final gate = Completer<Object?>();
          final cache = _cache(
            (request, call) => request.meta.method == 'GET'
                ? (call == 0
                      ? [
                          {'id': 7, 'ssn': _secret},
                        ]
                      : <Object?>[])
                : gate.future,
          );

          cache.setPrincipal('alice');
          await cache.fetch(_orderList, TagContext.empty);

          final seen = <Object?>[];
          final sub = cache
              .watch(_orderList, TagContext.empty)
              .listen((state) => seen.add(state.dataOrNull));
          await settle();

          final pending = cache.mutate(
            _orderPatch,
            const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
            options: MutateOptions(
              optimistic: OptimisticUpdate<Object?>(
                (_) => {'status': 'shipped', 'ssn': _secret},
              ),
            ),
          );

          expect(_everything(cache), contains(_secret));

          seen.clear();
          switchAway(cache);
          gate.complete({'id': 7, 'status': 'shipped', 'ssn': _secret});
          await pending;
          await settle();

          // The watcher the switch re-mounted, and every direct read.
          expect(
            jsonEncode(seen, toEncodable: (Object? o) => '$o'),
            isNot(contains(_secret)),
          );
          expect(_everything(cache), isNot(contains(_secret)));
          expect(cache.overlays.effective('Order:7'), isNull);

          await sub.cancel();
        });
      }
    },
  );
}
