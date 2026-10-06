// A writer for process_kill_test.dart, run as its own `dart` process. It
// imports no Flutter library, so a plain Dart VM can load it.
//
// Usage: dart run test/support/kill_writer.dart <directory>
//
// It prints `pid <n>`, then runs [_rounds] rounds through the adapter: each
// enqueues one outbox record, then writes one batch of [_batchSize] key-value
// rows of about 20 KB in a single transaction, and prints `committed <round>`
// once both have returned. Then it opens a raw transaction on a second
// connection and writes the next round's rows until the transaction spills
// and its rollback journal is hot, prints `hot`, and blocks on stdin with the
// transaction open. It never closes anything. The test kills it with SIGKILL
// on the `hot` line, so the kill always lands mid-transaction.
import 'dart:io';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/src/storage/database_files_native.dart';
import 'package:forge_client_offline/src/storage/encrypted_storage.dart';
import 'package:forge_client_offline/src/storage/raw_key.dart';
import 'package:sqlite3/sqlite3.dart';

import 'fixed_keys.dart';

const _batchSize = 200;
const _rounds = 30;

/// Makes each row about 20 KB, so a batch (about 4 MB) outgrows SQLite's
/// default 2 MB page cache and spills to the file mid-transaction.
final _padding = 'x' * 20000;

/// SQLite's rollback journal header magic, written once the journal is
/// synced and the database file is about to change.
const _journalMagic = [0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7];

bool _isHot(File journal) {
  if (!journal.existsSync()) return false;
  final file = journal.openSync();
  try {
    final head = file.readSync(_journalMagic.length);
    if (head.length < _journalMagic.length) return false;
    for (var i = 0; i < _journalMagic.length; i++) {
      if (head[i] != _journalMagic[i]) return false;
    }
    return true;
  } finally {
    file.closeSync();
  }
}

Future<void> main(List<String> args) async {
  stdout.writeln('pid $pid');

  final directory = args.single;
  final keys = FixedKeys(7);
  final storage = EncryptedSqliteStorage(
    keys: keys,
    files: NativeDatabaseFiles(directory, labels: keys),
  );
  final session = await storage.open('alice');
  final rows = session.namespace('rounds');

  for (var round = 0; round < _rounds; round++) {
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

  // The adapter's connection is idle between calls, so a second one can
  // take the write lock. Rows go in one at a time until the transaction has
  // spilled and the journal is hot; that point is reached for certain, not
  // raced against.
  final path = await nativeDatabasePath(directory, keys, 'alice');
  final journal = File('$path-journal');
  final raw = sqlite3.open(path);
  applyRawKey(raw, keys.bytes);
  raw.execute('PRAGMA busy_timeout = 5000');
  raw.execute('BEGIN IMMEDIATE');
  final insert = raw.prepare(
    'INSERT INTO kv (namespace, key, value) VALUES (?, ?, ?)',
  );
  for (var i = 0; !_isHot(journal); i++) {
    if (i >= 100000) {
      stderr.writeln('the raw transaction never spilled');
      exit(2);
    }
    insert.execute(['rounds', 'r$_rounds/$i', '$_rounds:$_padding']);
  }
  stdout.writeln('hot');

  // Synchronous, so nothing runs that could finish or roll back the
  // transaction before the kill.
  stdin.readLineSync();
}
