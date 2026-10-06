@TestOn('vm')
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/src/keys/principal_hash.dart';
import 'package:forge_client_offline/src/storage/raw_key.dart';
import 'package:sqlite3/sqlite3.dart';

const _plainHeader = 'SQLite format 3\u0000';

/// sqlite3mc's ChaCha20 default: PBKDF2-HMAC-SHA256 over the passphrase with
/// the 16-byte salt at the start of the file.
const _chacha20KdfIterations = 64007;

final _key = Uint8List.fromList(List.generate(32, (i) => (i * 37 + 11) % 256));

void main() {
  late Directory dir;
  late String path;

  setUp(() {
    dir = Directory.systemTemp.createTempSync('forge_raw_key_');
    path = '${dir.path}/k.db';
  });

  tearDown(() {
    if (dir.existsSync()) dir.deleteSync(recursive: true);
  });

  void create(void Function(Database db) unlock) {
    final db = sqlite3.open(path);
    try {
      unlock(db);
      db.execute('CREATE TABLE t (v TEXT NOT NULL)');
      db.execute("INSERT INTO t (v) VALUES ('secret-marker')");
    } finally {
      db.close();
    }
  }

  bool opens(void Function(Database db) unlock) {
    final db = sqlite3.open(path);
    try {
      unlock(db);
      return db.select('SELECT v FROM t').single['v'] == 'secret-marker';
    } on SqliteException catch (e) {
      // 26 is SQLITE_NOTADB: the key does not decrypt the file.
      expect(e.resultCode, 26);
      return false;
    } finally {
      db.close();
    }
  }

  void hexkey(Database db, List<int> bytes) =>
      db.execute("PRAGMA hexkey = '${hex(bytes)}'");

  test('a raw-keyed database is encrypted and opens with the raw key', () {
    create((db) => applyRawKey(db, _key));

    final bytes = File(path).readAsBytesSync();
    expect(String.fromCharCodes(bytes.sublist(0, 16)), isNot(_plainHeader));
    expect(String.fromCharCodes(bytes), isNot(contains('secret-marker')));
    expect(opens((db) => applyRawKey(db, _key)), isTrue);
    expect(opens((db) => applyRawKey(db, Uint8List(32))), isFalse);
  });

  test('the 32 bytes are the cipher key, not a passphrase', () {
    create((db) => applyRawKey(db, _key));

    // The passphrase form of the same bytes runs them through the KDF, so it
    // derives a different key and cannot read the file.
    expect(opens((db) => hexkey(db, _key)), isFalse);
  });

  test('no key derivation runs: the KDF iteration count has no effect', () {
    create((db) => applyRawKey(db, _key));

    expect(
      opens((db) {
        db.execute('PRAGMA kdf_iter = 1');
        applyRawKey(db, _key);
      }),
      isTrue,
    );

    // The passphrase form depends on it, which is what bypassing it means.
    File(path).deleteSync();
    create((db) => hexkey(db, _key));
    expect(
      opens((db) {
        db.execute('PRAGMA kdf_iter = 1');
        hexkey(db, _key);
      }),
      isFalse,
    );
  });

  test(
    'hexkey is a passphrase: PBKDF2 of it opens the file as a raw key',
    () async {
      create((db) => hexkey(db, _key));

      final salt = File(path).readAsBytesSync().sublist(0, 16);
      final derived = await Pbkdf2(
        macAlgorithm: Hmac.sha256(),
        iterations: _chacha20KdfIterations,
        bits: 256,
      ).deriveKey(secretKey: SecretKey(_key), nonce: salt);
      final bytes = Uint8List.fromList(await derived.extractBytes());

      expect(opens((db) => applyRawKey(db, bytes)), isTrue);
      expect(opens((db) => applyRawKey(db, _key)), isFalse);
    },
  );

  test('the cipher is ChaCha20, whose key is 32 bytes', () {
    final db = sqlite3.open(path);
    addTearDown(db.close);

    applyRawKey(db, _key);

    expect(db.select('PRAGMA cipher').single.values.single, rawKeyCipher);
    expect(rawKeyCipher, 'chacha20');
  });

  test('a key that is not 32 bytes is refused before reaching sqlite3mc', () {
    // sqlite3mc treats a raw: value of the wrong length as a passphrase and
    // silently runs it through the KDF, so the length is checked first.
    final db = sqlite3.open(path);
    addTearDown(db.close);

    expect(() => applyRawKey(db, Uint8List(31)), throwsArgumentError);
    expect(() => applyRawKey(db, Uint8List(33)), throwsArgumentError);
  });
}
