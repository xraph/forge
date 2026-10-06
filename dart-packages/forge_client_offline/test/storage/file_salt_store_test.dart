@TestOn('vm')
library;

import 'dart:async';
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

  File sidecar() => File('${store()}/$_label.salt/salt');

  /// What is in the package's subdirectory, or nothing when it is absent.
  List<String> names() => [
    if (Directory(store()).existsSync())
      for (final entity in Directory(store()).listSync())
        entity.path.split(Platform.pathSeparator).last,
  ];

  /// What a creator that crashed before publishing leaves behind.
  Directory crashLeftover(int fill) {
    final temp = Directory('${store()}/$_label.salt.tmp-00112233445566ff');
    File('${temp.path}/salt')
      ..createSync(recursive: true)
      ..writeAsBytesSync(_salt(fill));
    return temp;
  }

  PassphraseKey keysOver(PassphraseSaltStore store) =>
      PassphraseKey.weakForTesting(
        () => 'secret',
        salts: store,
        labels: _FixedLabel(),
        memoryKiB: 64,
        iterations: 1,
        parallelism: 1,
      );

  test('is a FilePassphraseSaltStore beside the databases', () {
    expect(salts, isA<FilePassphraseSaltStore>());
    expect((salts as FilePassphraseSaltStore).directory, dir.path);
    expect(() => platformPassphraseSaltStore(), throwsArgumentError);
  });

  test('round-trips a salt through the <label>.salt/salt sidecar', () async {
    expect(await salts.get(_label), isNull);

    await salts.put(_label, _salt(7));

    expect(await salts.get(_label), _salt(7));
    expect(sidecar().readAsBytesSync(), _salt(7));
    expect(names(), ['$_label.salt'], reason: 'no temporary directory is left');
  });

  test('put never replaces a published salt', () async {
    await salts.put(_label, _salt(1));

    await salts.put(_label, _salt(2));

    expect(await salts.get(_label), _salt(1));
    expect(names(), ['$_label.salt']);
  });

  test(
    'a stalled creator never overwrites a salt another creator published',
    () async {
      final stalled = FilePassphraseSaltStore(dir.path);
      final prompt = FilePassphraseSaltStore(dir.path);
      final resume = Completer<void>();
      final reachedPublish = Completer<void>();
      stalled.beforePublish = () {
        reachedPublish.complete();
        return resume.future;
      };

      // A writes its salt, then stalls just before publishing it, for as
      // long as it likes: no clock is consulted.
      final a = stalled.put(_label, _salt(1));
      await reachedPublish.future;

      // B publishes for real meanwhile.
      await prompt.put(_label, _salt(2));
      expect(await salts.get(_label), _salt(2));

      // A wakes up and tries to publish: B's salt stands.
      resume.complete();
      await a;

      expect(await salts.get(_label), _salt(2));
      expect(names(), ['$_label.salt'], reason: "A's temporary directory went");
    },
  );

  test('a crash leftover is ignored by get and a later put succeeds', () async {
    crashLeftover(5);

    expect(await salts.get(_label), isNull);
    expect(await salts.hasData(_label), isFalse);

    await salts.put(_label, _salt(6));

    expect(await salts.get(_label), _salt(6));
  });

  test('a passphrase key recovers from a crash leftover on its own', () async {
    crashLeftover(5);

    final first = await keysOver(salts).obtain('alice');

    expect(sidecar().lengthSync(), 16);
    expect((await keysOver(salts).obtain('alice')).bytes, first.bytes);
  });

  test(
    'concurrent creators over separate instances converge on one salt',
    () async {
      final stores = [
        for (var i = 0; i < 8; i++) FilePassphraseSaltStore(dir.path),
      ];
      await Future.wait([
        for (var i = 0; i < stores.length; i++)
          stores[i].put(_label, _salt(i + 1)),
      ]);

      final stored = await salts.get(_label);
      expect(stored, isNotNull);
      for (final store in stores) {
        expect(await store.get(_label), stored);
      }
      expect(names(), ['$_label.salt']);
    },
  );

  test('concurrent passphrase keys over separate instances agree', () async {
    final keys = await Future.wait([
      for (var i = 0; i < 6; i++)
        keysOver(platformPassphraseSaltStore(directory: dir.path))
            .obtain('alice'),
    ]);

    for (final key in keys) {
      expect(key.bytes, keys.first.bytes);
    }
    expect(names(), ['$_label.salt']);
  });

  // Exclusivity at the file level is pinned by the stalled-creator and
  // put-never-replaces tests. This pins the layer above it: separate but
  // equal store instances share one in-flight attempt, so only one creator
  // ever tries to publish a salt.
  test(
    'concurrent passphrase keys over separate equal instances make one put',
    () async {
      final puts = <String>[];
      final keys = await Future.wait([
        for (var i = 0; i < 6; i++)
          keysOver(_CountingSaltStore(FilePassphraseSaltStore(dir.path), puts))
              .obtain('alice'),
      ]);

      expect(puts, [_label]);
      for (final key in keys) {
        expect(key.bytes, keys.first.bytes);
      }
    },
  );

  test('instances over one directory are equal, others are not', () {
    final a = FilePassphraseSaltStore(dir.path);
    final b = FilePassphraseSaltStore('${dir.path}${Platform.pathSeparator}');
    final c = FilePassphraseSaltStore('${dir.path}/x/..');
    final other = FilePassphraseSaltStore('${dir.path}/other');

    expect(a, b);
    expect(a, c);
    expect(a.hashCode, b.hashCode);
    expect(a, isNot(other));
  });

  test('a backward clock jump changes nothing', () async {
    // Nothing reads the clock: a sidecar or leftover dated in the future or
    // the distant past behaves the same.
    final leftover = crashLeftover(5);
    File('${leftover.path}/salt').setLastModifiedSync(DateTime(2099));

    await salts.put(_label, _salt(6));
    sidecar().setLastModifiedSync(DateTime(1990));
    await salts.put(_label, _salt(7));

    expect(await salts.get(_label), _salt(6));
  });

  test('an empty sidecar directory holds no salt and is replaced', () async {
    // What a delete that stopped between the salt file and its directory
    // leaves behind.
    Directory('${store()}/$_label.salt').createSync(recursive: true);

    expect(await salts.get(_label), isNull);
    await salts.put(_label, _salt(4));

    expect(await salts.get(_label), _salt(4));
  });

  test('hasData answers from whether the database file exists', () async {
    expect(await salts.hasData(_label), isFalse);

    File('${store()}/$_label.db')
      ..createSync(recursive: true)
      ..writeAsStringSync('db');

    expect(await salts.hasData(_label), isTrue);
  });

  test(
    'delete removes the sidecar and leftovers, and deleting none is fine',
    () async {
      await salts.put(_label, _salt(3));
      crashLeftover(9);
      final otherLabel = 'cd' * 32;
      await salts.put(otherLabel, _salt(8));

      await salts.delete(_label);
      await salts.delete(_label);

      expect(await salts.get(_label), isNull);
      expect(names(), ['$otherLabel.salt'], reason: 'other labels are kept');
    },
  );

  test('a passphrase key keeps its salt here and refuses a lost one', () async {
    final keys = keysOver(salts);

    final first = await keys.obtain('alice');
    expect(sidecar().lengthSync(), 16);
    expect((await keys.obtain('alice')).bytes, first.bytes);

    // The database exists but its salt is gone: the key can never be derived
    // again, so the provider fails closed instead of minting a new salt.
    File('${store()}/$_label.db').writeAsStringSync('db');
    Directory('${store()}/$_label.salt').deleteSync(recursive: true);
    await expectLater(keys.obtain('alice'), throwsA(isA<KeyUnavailable>()));
    expect(sidecar().existsSync(), isFalse);
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

/// Records every put, and compares equal exactly when the stores it wraps
/// do, as the passphrase key's in-flight map requires.
final class _CountingSaltStore implements PassphraseSaltStore {
  _CountingSaltStore(this.inner, this.puts);

  final PassphraseSaltStore inner;
  final List<String> puts;

  @override
  Future<Uint8List?> get(String label) => inner.get(label);

  @override
  Future<bool> hasData(String label) => inner.hasData(label);

  @override
  Future<void> put(String label, Uint8List salt) {
    puts.add(label);
    return inner.put(label, salt);
  }

  @override
  Future<void> delete(String label) => inner.delete(label);

  @override
  bool operator ==(Object other) =>
      other is _CountingSaltStore && other.inner == inner;

  @override
  int get hashCode => inner.hashCode;
}

final class _FixedLabel implements PrincipalLabeler {
  @override
  Future<String> principalLabel(String principal) async => _label;
}
