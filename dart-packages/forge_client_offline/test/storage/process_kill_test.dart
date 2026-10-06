@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:forge_client_offline/src/storage/database_files_native.dart'
    show NativeDatabaseFiles, nativeDatabasePath;
import 'package:forge_client_offline/src/storage/raw_key.dart';
import 'package:sqlite3/sqlite3.dart';

import '../support/fixed_keys.dart';

/// Rows per batch in test/support/kill_writer.dart.
const _batchSize = 200;

/// The Dart VM of the Flutter SDK running this test. `bin/dart` is a shell
/// wrapper; the VM itself is started so the PID it reports is the writer's.
String _dart() {
  final root = Platform.environment['FLUTTER_ROOT'];
  if (root == null) {
    fail('FLUTTER_ROOT is not set, so the Dart VM cannot be found');
  }
  final sep = Platform.pathSeparator;
  final exe = Platform.isWindows ? 'dart.exe' : 'dart';
  return [root, 'bin', 'cache', 'dart-sdk', 'bin', exe].join(sep);
}

void main() {
  test('the database survives a writer process killed mid-write', () async {
    final dir = await Directory.systemTemp.createTemp('forge_offline_kill_');
    addTearDown(() => dir.delete(recursive: true));

    final keys = FixedKeys(7);
    final path = await nativeDatabasePath(dir.path, keys, 'alice');
    final journal = File('$path-journal');

    final writer = await Process.start(_dart(), [
      'run',
      'test/support/kill_writer.dart',
      dir.path,
    ]);
    // Only ever kill the PIDs this test started.
    addTearDown(() => writer.kill(ProcessSignal.sigkill));

    final stderrText = StringBuffer();
    final errors = writer.stderr
        .transform(utf8.decoder)
        .listen(stderrText.write);

    int? writerPid;
    var committed = -1;
    final warmedUp = Completer<void>();
    final lines = writer.stdout
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen((line) {
          if (line.startsWith('pid ')) writerPid = int.parse(line.substring(4));
          if (!line.startsWith('committed ')) return;
          committed = int.parse(line.substring(10));
          if (committed >= 30 && !warmedUp.isCompleted) warmedUp.complete();
        });

    await Future.any([
      warmedUp.future,
      writer.exitCode.then(
        (code) =>
            fail('the writer exited ($code) before it was killed: $stderrText'),
      ),
    ]);

    // The rollback journal exists only while a write transaction is open, so
    // waiting for it lands the kill inside one: no close, no commit, no
    // signal handler. The writer keeps writing, so it appears within ms.
    final watch = Stopwatch()..start();
    while (!journal.existsSync() &&
        watch.elapsed < const Duration(seconds: 10)) {}
    Process.killPid(writerPid!, ProcessSignal.sigkill);

    await writer.exitCode.timeout(const Duration(seconds: 20));
    await lines.cancel();
    await errors.cancel();
    final lastReported = committed;
    final hotJournal = journal.existsSync();
    expect(
      hotJournal,
      isTrue,
      reason: 'the kill landed inside a write transaction',
    );

    // A plain connection, keyed raw, reads the file as SQLite left it.
    final raw = sqlite3.open(path);
    applyRawKey(raw, keys.bytes);
    expect(raw.select('PRAGMA integrity_check').single.values.single, 'ok');
    raw.close();

    final storage = EncryptedSqliteStorage(
      keys: keys,
      files: NativeDatabaseFiles(dir.path, labels: keys),
    );
    final session = await storage.open('alice');
    final outbox = await session.readOutbox();
    final rows = await session.namespace('rounds').scan('r');
    await session.close();

    // Every round the writer reported is there: a commit that returned is
    // durable. The outbox is a gapless prefix, each record whole.
    final ids = outbox.map((r) => r.id).toList();
    expect(ids.length, greaterThan(lastReported));
    expect(ids, [for (var i = 0; i < ids.length; i++) 'm$i']);
    for (final record in outbox) {
      expect(record.idempotencyKey, 'key-${record.id}');
      expect(record.argsJson, '{"round":${record.id.substring(1)}}');
    }

    // Each batch is all there or not there at all; a batch interrupted by
    // the kill was rolled back from the journal when the file was reopened.
    final perRound = <int, int>{};
    for (final entry in rows.entries) {
      final round = int.parse(entry.key.substring(1, entry.key.indexOf('/')));
      expect(entry.value, '$round:${'x' * 4000}');
      perRound[round] = (perRound[round] ?? 0) + 1;
    }
    expect(perRound.values.toSet(), {_batchSize});
    final rounds = perRound.keys.toList()..sort();
    expect(rounds, [for (var i = 0; i < rounds.length; i++) i]);
    expect(rounds.length, anyOf(ids.length, ids.length - 1));

    printOnFailure(
      'killed after round $lastReported; outbox ${ids.length}, batches '
      '${rounds.length}, hot journal: $hotJournal',
    );
    // ignore: avoid_print
    print(
      'process kill: dart run pid ${writer.pid}, writer pid $writerPid killed after round $lastReported; '
      'outbox ${ids.length}, batches ${rounds.length}, hot journal $hotJournal',
    );
  });
}
