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

/// SQLite's rollback journal header magic.
const _journalMagic = [0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7];

/// Whether [journal] is a hot rollback journal: it exists and its header
/// carries the magic, which SQLite writes only once the journal is synced and
/// the database file is about to change.
bool _isHot(File journal) {
  try {
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
  } on FileSystemException {
    return false;
  }
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
    // Only ever kill the PIDs this test started, and reap the child even when
    // the test fails before its own kill.
    addTearDown(() async {
      writer.kill(ProcessSignal.sigkill);
      await writer.exitCode.timeout(const Duration(seconds: 20));
    });

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

    // A journal whose header carries SQLite's magic is hot: it was synced
    // and the transaction has begun overwriting the database file, so a kill
    // now leaves pages that only the journal can roll back. (Until then the
    // header is zeroed and the file untouched.) Each batch is larger than the
    // page cache, so it spills, and goes hot, long before it commits. No
    // close, no commit, no signal handler.
    final watch = Stopwatch()..start();
    while (!_isHot(journal) && watch.elapsed < const Duration(seconds: 10)) {}
    Process.killPid(writerPid!, ProcessSignal.sigkill);

    await writer.exitCode.timeout(const Duration(seconds: 20));
    await lines.cancel();
    await errors.cancel();
    final lastReported = committed;
    final hotJournal = _isHot(journal);
    expect(
      hotJournal,
      isTrue,
      reason: 'the kill landed after the transaction began writing the file',
    );

    // Reopen through the adapter first, so its unlock runs against the hot
    // journal the kill left, as an app's next launch would.
    final storage = EncryptedSqliteStorage(
      keys: keys,
      files: NativeDatabaseFiles(dir.path, labels: keys),
    );
    final session = await storage.open('alice');
    final outbox = await session.readOutbox();
    final rows = await session.namespace('rounds').scan('r');
    await session.close();
    expect(journal.existsSync(), isFalse, reason: 'the reopen rolled it back');

    // Then a plain connection, keyed raw, checks the file it left.
    final raw = sqlite3.open(path);
    applyRawKey(raw, keys.bytes);
    expect(raw.select('PRAGMA integrity_check').single.values.single, 'ok');
    raw.close();

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
      expect(entry.value, '$round:${'x' * 20000}');
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
