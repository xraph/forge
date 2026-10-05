@TestOn('vm')
library;

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sqlite3/sqlite3.dart';

const _plainHeader = 'SQLite format 3\u0000';

void main() {
  late Directory dir;
  late String path;

  setUp(() {
    dir = Directory.systemTemp.createTempSync('forge_offline_smoke_');
    path = '${dir.path}/smoke.db';
  });

  tearDown(() {
    if (dir.existsSync()) dir.deleteSync(recursive: true);
  });

  test('the native SQLite is SQLite3MultipleCiphers and encrypts the file', () {
    // A cipher refuses a key on an in-memory database, so this uses a file.
    final db = sqlite3.open(path);
    var closed = false;
    addTearDown(() {
      if (!closed) db.close();
    });

    var hasCipher = db.select('PRAGMA cipher').isNotEmpty;
    if (!hasCipher) {
      try {
        db.select("SELECT sqlite3mc_config('cipher')");
        hasCipher = true;
      } on SqliteException {
        hasCipher = false;
      }
    }

    expect(hasCipher, isTrue, reason: 'plain SQLite is linked: the hooks user_defines block is not in effect');

    final key = '00' * 32;
    db.execute("PRAGMA hexkey = '$key'");
    db.execute('CREATE TABLE t (v TEXT NOT NULL)');
    db.execute("INSERT INTO t (v) VALUES ('secret-marker')");
    expect(db.select('SELECT v FROM t').first['v'], 'secret-marker');
    db.close();
    closed = true;

    final bytes = File(path).readAsBytesSync();
    expect(bytes.length, greaterThanOrEqualTo(16));
    expect(String.fromCharCodes(bytes.sublist(0, 16)), isNot(_plainHeader), reason: 'the file header is plain SQLite');
    expect(String.fromCharCodes(bytes), isNot(contains('secret-marker')));

    final right = sqlite3.open(path);
    addTearDown(right.close);
    right.execute("PRAGMA hexkey = '$key'");
    expect(right.select('SELECT v FROM t').first['v'], 'secret-marker');

    final wrong = sqlite3.open(path);
    addTearDown(wrong.close);
    wrong.execute("PRAGMA hexkey = '${'11' * 32}'");
    expect(() => wrong.select('SELECT v FROM t'), throwsA(isA<SqliteException>()));

    final keyless = sqlite3.open(path);
    addTearDown(keyless.close);
    expect(() => keyless.select('SELECT v FROM t'), throwsA(isA<SqliteException>()));
  });
}
