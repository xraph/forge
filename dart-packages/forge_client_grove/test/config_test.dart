import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:test/test.dart';

void main() {
  const foundry = SyncDeclaration(
    protocol: 'grove-crdt',
    entity: 'DatasetRow',
    table: null,
    pull: '/twinos/api/v1/datasets/{id}/sync/pull',
    push: '/twinos/api/v1/datasets/{id}/sync/push',
    stream: '/twinos/api/v1/datasets/{id}/sync/stream',
    socket: '/twinos/api/v1/datasets/{id}/sync/ws',
    dataset: '{id}',
  );
  const grove = SyncDeclaration(
    protocol: 'grove-crdt',
    entity: 'Note',
    table: 'notes',
    pull: '/sync/pull',
    push: '/sync/push',
  );

  test('substitutes the dataset placeholder with the encoded id', () {
    final e = resolveEndpoints(foundry, 'a b');
    expect(e.pull, '/twinos/api/v1/datasets/a%20b/sync/pull');
    expect(e.socket, '/twinos/api/v1/datasets/a%20b/sync/ws');
  });

  test('an empty dataset id on a datasetted declaration is refused', () {
    expect(() => resolveEndpoints(foundry, ''), throwsArgumentError);
  });

  test('a declaration without a dataset keeps its paths and has no param', () {
    final e = resolveEndpoints(grove, '');
    expect(e.pull, '/sync/pull');
    expect(e.stream, isNull);
    expect(datasetParam(grove), isNull);
    expect(datasetParam(foundry), 'id');
  });

  test('composite ids survive dataset ids containing a colon', () {
    final id = compositeId('team:a', 'r:1');
    expect(id, 'team%3Aa:r:1');
    expect(splitCompositeId(id), (datasetId: 'team:a', pk: 'r:1'));
    expect(splitCompositeId('plain'), isNull);
  });

  test('replica keys separate datasets and implicit datasets', () {
    expect(replicaKey(datasetId: 'ds1', pullPath: '/x'), 'ds1');
    expect(
      replicaKey(datasetId: '', pullPath: '/sync/pull'),
      '~%2Fsync%2Fpull',
    );
  });

  test('an omitted table is left to each dataset', () {
    expect(declaredTable(foundry), isNull);
    expect(declaredTable(grove), 'notes');
  });

  test('GroveChangeRejected exposes the first reason', () {
    const r = GroveChangeRejected([
      GroveRejectedChange(
        key: 'k',
        entity: 'Note',
        id: 'n1',
        field: 'locked',
        kind: 'hook',
        reason: 'locked is locked',
      ),
    ]);
    expect(r.reason, 'locked is locked');
  });
}
