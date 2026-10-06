import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/kit.dart';

void main() {
  test('describeForDevtools returns the Sync panel shape, JSON-safe', () async {
    final h = Harness();

    await h.signIn();
    h.server.remote('n9', 'title', 'peer edit', 3);
    await h.source.syncNow();
    h.server.gone = true;
    await h.mutate(
      opUpdateNote,
      const TagContext(path: {'noteId': 'n1'}, body: {'title': 'mine'}),
    );
    await pumpEventQueue(times: 50);

    final d = await h.source.describeForDevtools();

    expect(() => jsonEncode(d), returnsNormally);
    expect(d['protocol'], 'grove-crdt');
    expect(d['nodeId'], startsWith('dart-'));

    final hlc = d['hlc']! as Map<String, Object?>;

    expect(hlc['ts'], isA<String>());
    expect(hlc['counter'], isA<int>());
    expect(hlc['nodeId'], d['nodeId']);

    final note =
        (d['entities']! as Map<String, Object?>)['Note']!
            as Map<String, Object?>;

    expect(note['table'], 'notes');
    expect(note['pending'], 1);
    expect(note['status'], 'failed');
    expect(note['error'], contains('GroveDatasetGone'));
    expect(note['lastPull'], isA<String>());
    expect(
      (note['datasets']! as List<Object?>).single,
      containsPair('dataset', ''),
    );
    expect(d['peers'], ['other']);
  });
}
