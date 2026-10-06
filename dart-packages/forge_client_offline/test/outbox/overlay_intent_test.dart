import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

const _patch = OperationMeta(
  id: 'patch',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}'],
);
const _put = OperationMeta(
  id: 'put',
  method: 'PUT',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}'],
);
const _delete = OperationMeta(
  id: 'delete',
  method: 'DELETE',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Order[]'],
);
const _create = OperationMeta(
  id: 'create',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  invalidates: ['Order[]'],
);
const _ambiguous = OperationMeta(
  id: 'move',
  method: 'PATCH',
  path: '/orders/{id}/customer/{customerId}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Customer:{customerId}'],
);

void main() {
  test('a PATCH or PUT with a map body merges into its target', () {
    for (final meta in [_patch, _put]) {
      final intent = deriveOverlayIntent(
        meta,
        const TagContext(path: {'id': '7'}, body: {'note': 'x'}),
      );

      expect(
        intent,
        isA<MergeOverlay>().having((i) => i.key, 'key', 'Order:7').having(
          (i) => i.patch,
          'patch',
          {'note': 'x'},
        ),
      );
    }
  });

  test('a DELETE removes its target', () {
    expect(
      deriveOverlayIntent(_delete, const TagContext(path: {'id': '7'})),
      isA<DeleteOverlay>().having((i) => i.key, 'key', 'Order:7'),
    );
  });

  test('a create, a non-map body and an ambiguous target draw nothing', () {
    expect(
      deriveOverlayIntent(_create, const TagContext(body: {'total': 1})),
      isA<NoOverlay>(),
    );
    expect(
      deriveOverlayIntent(
        _patch,
        const TagContext(path: {'id': '7'}, body: 'text'),
      ),
      isA<NoOverlay>(),
    );
    expect(
      deriveOverlayIntent(
        _ambiguous,
        const TagContext(path: {'id': '7', 'customerId': '3'}, body: {'a': 1}),
      ),
      isA<NoOverlay>(),
    );
  });

  test('intents round-trip through encode and decode', () {
    expect(OverlayIntent.decode(const NoOverlay().encode()), isA<NoOverlay>());
    expect(
      OverlayIntent.decode(
        const MergeOverlay('Order:7', {'note': 'x'}).encode(),
      ),
      isA<MergeOverlay>().having((i) => i.key, 'key', 'Order:7').having(
        (i) => i.patch,
        'patch',
        {'note': 'x'},
      ),
    );
    expect(
      OverlayIntent.decode(const DeleteOverlay('Order:7').encode()),
      isA<DeleteOverlay>().having((i) => i.key, 'key', 'Order:7'),
    );
    expect(
      () => OverlayIntent.decode('{"kind":"mystery"}'),
      throwsFormatException,
    );
  });

  test('a merge becomes an update that patches the previous record', () {
    final optimistic =
        const MergeOverlay('Order:7', {'note': 'x'}).toOptimistic()
            as OptimisticUpdate<Object?>;

    expect(optimistic.key, 'Order:7');
    expect(
      optimistic.update(<String, Object?>{
        'id': '7',
        'note': null,
        'total': 10,
      }),
      {'id': '7', 'note': 'x', 'total': 10},
    );
    expect(optimistic.update(null), isNull);
  });

  test('a delete becomes an optimistic delete and no overlay becomes null', () {
    expect(
      const DeleteOverlay('Order:7').toOptimistic(),
      isA<OptimisticDelete<Object?>>().having((o) => o.key, 'key', 'Order:7'),
    );
    expect(const NoOverlay().toOptimistic(), isNull);
  });

  test('a malformed merge or delete is a FormatException, not a TypeError', () {
    for (final source in [
      '{"kind":"merge","patch":{"a":1}}',
      '{"kind":"merge","key":"Order:7","patch":[1]}',
      '{"kind":"delete","key":7}',
      '[]',
    ]) {
      expect(
        () => OverlayIntent.decode(source),
        throwsFormatException,
        reason: source,
      );
    }
  });
}
