// A writer for process_kill_test.dart, run as its own `dart` process. It
// imports no Flutter library, so a plain Dart VM can load it.
//
// Usage: dart run test/support/kill_writer.dart <directory>
//
// It prints `pid <n>`, then writes forever: each round enqueues one outbox
// record, then writes one batch of [_batchSize] key-value rows of about 4 KB in a single
// transaction, and prints `committed <round>` once both have returned. It
// never closes the database. The test kills it with SIGKILL mid-write.
import 'dart:io';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/src/storage/database_files_native.dart';
import 'package:forge_client_offline/src/storage/encrypted_storage.dart';

import 'fixed_keys.dart';

const _batchSize = 200;

/// Makes each row about 4 KB, so most of a round is spent inside SQLite's
/// write transaction, where a kill leaves a hot journal behind.
final _padding = 'x' * 4000;

Future<void> main(List<String> args) async {
  stdout.writeln('pid $pid');

  final keys = FixedKeys(7);
  final storage = EncryptedSqliteStorage(
    keys: keys,
    files: NativeDatabaseFiles(args.single, labels: keys),
  );
  final session = await storage.open('alice');
  final rows = session.namespace('rounds');

  for (var round = 0; ; round++) {
    await session.enqueue(
      PendingMutationRecord(
        id: 'm$round',
        operationId: 'op_update_order',
        argsJson: '{"round":$round}',
        idempotencyKey: 'key-m$round',
        createdAt: DateTime.utc(2026, 10, 4),
        stateJson: '{"kind":"queued"}',
      ),
    );
    await rows.batch((batch) {
      for (var i = 0; i < _batchSize; i++) {
        batch.put('r$round/$i', '$round:$_padding');
      }
    });
    stdout.writeln('committed $round');
  }
}
