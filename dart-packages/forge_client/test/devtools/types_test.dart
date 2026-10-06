import 'dart:convert';

import 'package:forge_client/src/devtools/tag.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

void main() {
  test('a record snapshot serialises exactly its seven cheap fields', () {
    const record = RecordSnapshot(
      key: 'GET /orders',
      status: 'success',
      fetching: false,
      settled: true,
      inflight: false,
      restart: false,
      frameRestarts: 0,
    );

    expect(record.toJson().keys.toList()..sort(), [
      'fetching',
      'frameRestarts',
      'inflight',
      'key',
      'restart',
      'settled',
      'status',
    ]);
  });

  test('log entries carry their kind and stamps on the wire', () {
    const entry = MutationLog(
      seq: 3,
      at: 7,
      session: 0,
      operation: 'PATCH /orders/{id}',
      args: '{"path":{"id":1}}',
      tags: ['Order:1', 'Order[]'],
      unresolved: [],
    );

    expect(entry.kind, 'mutation');
    expect(entry.toJson(), {
      'kind': 'mutation',
      'seq': 3,
      'at': 7,
      'session': 0,
      'operation': 'PATCH /orders/{id}',
      'args': '{"path":{"id":1}}',
      'tags': ['Order:1', 'Order[]'],
      'unresolved': <String>[],
    });
    expect(
      const FetchLog(
        seq: 1,
        at: 1,
        session: 0,
        query: 'q',
        reason: FetchReason.invalidation,
        cause: 3,
      ).toJson()['reason'],
      'invalidation',
    );
    expect(
      const ActionLog(
        seq: 1,
        at: 1,
        session: 0,
        action: ActionKind.invalidateTag,
        target: 'Order[]',
      ).toJson()['action'],
      'invalidateTag',
    );
    expect(
      const OutboxLog(
        seq: 1,
        at: 1,
        session: 0,
        phase: OutboxPhase.failed,
        mutationId: 'm1',
        operation: null,
        failure: 'x',
      ).toJson()['phase'],
      'failed',
    );
  });

  test('outcomes and relations use the TS wire names', () {
    expect(MissOutcome.staleWhileUnmounted.wire, 'stale-while-unmounted');
    expect(MissOutcome.notTracked.wire, 'not-tracked');
    expect(NearMissRelation.letterCase.wire, 'case');
    expect(
      NearMissRelation.instanceVsCollection.wire,
      'instance-vs-collection',
    );
  });

  test('a miss report encodes to JSON without a custom encoder', () {
    const report = MissReport(
      query: 'GET /orders',
      outcome: MissOutcome.missed,
      reason: 'disjoint',
      cause: CauseSummary(
        label: 'mutation POST /orders',
        seq: 4,
        tags: ['Order:9'],
        unresolved: [],
      ),
      mounts: 1,
      settled: true,
      invalidated: ['Order:9'],
      carried: ['Order[]'],
      matched: [],
      nearest: [
        NearMiss(
          invalidated: 'Order:9',
          carried: 'Order[]',
          relation: NearMissRelation.instanceVsCollection,
          hint: 'h',
        ),
      ],
      suggestions: ['h'],
    );

    final decoded =
        jsonDecode(jsonEncode(report.toJson())) as Map<String, Object?>;

    expect(decoded['outcome'], 'missed');
    expect(
      (decoded['nearest']! as List<Object?>).single,
      containsPair('relation', 'instance-vs-collection'),
    );
    expect(decoded['kind'], 'miss');
  });
}
