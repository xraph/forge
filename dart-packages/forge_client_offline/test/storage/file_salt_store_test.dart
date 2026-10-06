@TestOn('vm')
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:forge_client_offline/src/storage/database_files_native.dart'
    show FilePassphraseSaltStore, nativeStoreDirectory;

final _label = 'ab' * 32;

Uint8List _salt(int fill) => Uint8List(16)..fillRange(0, 16, fill);

void main() {
  late Directory dir;
  late PassphraseSaltStore salts;

  setUp(() {
    dir = Directory.systemTemp.createTempSync('forge_salt_');
    salts = platformPassphraseSaltStore(directory: dir.path);
  });

  tearDown(() {
    if (dir.existsSync()) dir.deleteSync(recursive: true);
  });

  String store() => nativeStoreDirectory(dir.path);

  File sidecar() => File('${store()}/$_label.salt');

  /// What is in the package's subdirectory, or nothing when it is absent.
  List<String> names() => [
    if (Directory(store()).existsSync())
      for (final entity in Directory(store()).listSync())
        entity.path.split(Platform.pathSeparator).last,
  ];

  test('is a FilePassphraseSaltStore beside the databases', () {
    expect(salts, isA<FilePassphraseSaltStore>());
    expect((salts as FilePassphraseSaltStore).directory, dir.path);
    expect(() => platformPassphraseSaltStore(), throwsArgumentError);
  });

  test('round-trips a salt through the <label>.salt sidecar', () async {
    expect(await salts.get(_label), isNull);

    await salts.put(_label, _salt(7));

    expect(await salts.get(_label), _salt(7));
    expect(sidecar().readAsBytesSync(), _salt(7));
    expect(names(), ['$_label.salt'], reason: 'no temporary file is left');
  });

  test('put never replaces an existing sidecar', () async {
    await salts.put(_label, _salt(1));

    await expectLater(salts.put(_label, _salt(2)), throwsStateError);

    expect(await salts.get(_label), _salt(1));
    expect(names(), ['$_label.salt']);
  });

  test('of concurrent creators exactly one wins and keeps its salt', () async {
    final stores = [
      for (var i = 0; i < 8; i++) FilePassphraseSaltStore(dir.path),
    ];
    final outcomes = await Future.wait([
      for (var i = 0; i < stores.length; i++)
        stores[i]
            .put(_label, _salt(i + 1))
            .then<int?>((_) => i + 1, onError: (Object _) => null),
    ]);

    final winners = outcomes.whereType<int>().toList();
    expect(winners, hasLength(1));
    expect(await salts.get(_label), _salt(winners.single));
    expect(names(), ['$_label.salt']);
  });

  test('hasData answers from whether the database file exists', () async {
    expect(await salts.hasData(_label), isFalse);

    File('${store()}/$_label.db')
      ..createSync(recursive: true)
      ..writeAsStringSync('db');

    expect(await salts.hasData(_label), isTrue);
  });

  test('delete removes the sidecar, and deleting none is fine', () async {
    await salts.put(_label, _salt(3));

    await salts.delete(_label);
    await salts.delete(_label);

    expect(await salts.get(_label), isNull);
    expect(names(), isEmpty);
  });

  test('a passphrase key keeps its salt here and refuses a lost one', () async {
    final keys = PassphraseKey.weakForTesting(
      () => 'secret',
      salts: salts,
      labels: _FixedLabel(),
      memoryKiB: 64,
      iterations: 1,
      parallelism: 1,
    );

    final first = await keys.obtain('alice');
    expect(sidecar().lengthSync(), 16);
    expect((await keys.obtain('alice')).bytes, first.bytes);

    // The database exists but its salt is gone: the key can never be derived
    // again, so the provider fails closed instead of minting a new salt.
    File('${store()}/$_label.db')
      ..createSync(recursive: true)
      ..writeAsStringSync('db');
    sidecar().deleteSync();
    await expectLater(keys.obtain('alice'), throwsA(isA<KeyUnavailable>()));
    expect(sidecar().existsSync(), isFalse);
  });

  /// What a crash between the exclusive placeholder and the rename leaves.
  File leftover({Duration age = const Duration(minutes: 1)}) => sidecar()
    ..createSync(recursive: true)
    ..setLastModifiedSync(DateTime.now().subtract(age));

  test(
    'a crashed put leaves no brick: the empty leftover is replaced',
    () async {
      leftover();

      expect(await salts.get(_label), isNull, reason: 'reported as absent');
      await salts.put(_label, _salt(9));

      expect(await salts.get(_label), _salt(9));
      expect(names(), ['$_label.salt'], reason: 'nothing else left behind');
    },
  );

  test('a passphrase key recovers from the crashed put on its own', () async {
    leftover();
    final keys = PassphraseKey.weakForTesting(
      () => 'secret',
      salts: salts,
      labels: _FixedLabel(),
      memoryKiB: 64,
      iterations: 1,
      parallelism: 1,
    );

    await keys.obtain('alice');

    expect(sidecar().lengthSync(), 16);
  });

  test(
    'a fresh empty sidecar is a creator at work and is not replaced',
    () async {
      leftover(age: Duration.zero);

      await expectLater(salts.put(_label, _salt(9)), throwsStateError);

      expect(sidecar().lengthSync(), 0);
      expect(names(), ['$_label.salt']);
    },
  );

  test('an empty sidecar beside a database is never replaced', () async {
    leftover();
    File('${store()}/$_label.db').writeAsStringSync('db');

    expect(await salts.get(_label), isEmpty);
    await expectLater(salts.put(_label, _salt(9)), throwsStateError);
    expect(sidecar().lengthSync(), 0);
  });

  test('of concurrent replacers of a leftover exactly one wins', () async {
    leftover();
    final stores = [
      for (var i = 0; i < 8; i++) FilePassphraseSaltStore(dir.path),
    ];
    final outcomes = await Future.wait([
      for (var i = 0; i < stores.length; i++)
        stores[i]
            .put(_label, _salt(i + 1))
            .then<int?>((_) => i + 1, onError: (Object _) => null),
    ]);

    final winners = outcomes.whereType<int>().toList();
    expect(winners, hasLength(1));
    expect(await salts.get(_label), _salt(winners.single));
    expect(names(), ['$_label.salt']);
  });

  test('refuses a label that could leave the directory', () async {
    for (final label in ['../escape', 'AB' * 32, '']) {
      await expectLater(salts.get(label), throwsArgumentError);
      await expectLater(salts.put(label, _salt(1)), throwsArgumentError);
      await expectLater(salts.hasData(label), throwsArgumentError);
      await expectLater(salts.delete(label), throwsArgumentError);
    }
    expect(names(), isEmpty);
  });
}

final class _FixedLabel implements PrincipalLabeler {
  @override
  Future<String> principalLabel(String principal) async => _label;
}
