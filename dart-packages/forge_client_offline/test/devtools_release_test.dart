@TestOn('vm')
@Timeout(Duration(minutes: 5))
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

// forge_client's release_test.dart compiles a `configureClient` program and
// reads the bytes. This does the same for `OfflineClient.open(devtools: true)`,
// whose devtools branch lives in this package: a claim that it folds away in
// release, argued from source, is a hope.
const _fixture = 'test/devtools_release/release_app.dart';

/// Strings that exist only in the devtools: protocol names, the event kind,
/// near-miss relation names and explanation text, and the simulator's error.
const _markers = [
  'ext.forge.snapshot',
  'ext.forge.hello',
  'forge:event',
  'instance-vs-collection',
  'stale-while-unmounted',
  'differ only in case',
  'never heard of',
  'switched on from the devtools panel',
];

/// Present in every build: proves the byte search can find a string at all.
const _control = 'forge-release-fixture-marker';

/// The Dart SDK that ships with the Flutter running this test, falling back to
/// the one on the PATH.
String _dart() {
  final root = Platform.environment['FLUTTER_ROOT'];
  if (root != null) {
    final candidate =
        '$root/bin/cache/dart-sdk/bin/dart${Platform.isWindows ? '.exe' : ''}';
    if (File(candidate).existsSync()) return candidate;
  }
  return 'dart';
}

Future<String> _compile(List<String> args, String out) async {
  final result = await Process.run(_dart(), [
    'compile',
    'js',
    ...args,
    '-o',
    out,
    _fixture,
  ]);
  expect(result.exitCode, 0, reason: '${result.stdout}\n${result.stderr}');
  return latin1.decode(await File(out).readAsBytes(), allowInvalid: true);
}

void main() {
  late Directory dir;

  setUpAll(
    () async =>
        dir = await Directory.systemTemp.createTemp('forge_offline_release_'),
  );
  tearDownAll(() => dir.delete(recursive: true));

  test('a debug dart2js build of OfflineClient.open carries the devtools, so the checks below can fail', () async {
    final js = await _compile(['-O2'], '${dir.path}/debug.js');

    expect(js, contains(_control));
    for (final marker in _markers) {
      expect(
        js,
        contains(marker),
        reason:
            'the debug build lacks "$marker", so its absence below proves nothing',
      );
    }
  });

  test('a release dart2js build (dart.vm.product) of OfflineClient.open contains no devtools', () async {
    final js = await _compile([
      '-O2',
      '-Ddart.vm.product=true',
    ], '${dir.path}/release.js');

    expect(js, contains(_control));
    for (final marker in _markers) {
      expect(
        js,
        isNot(contains(marker)),
        reason: 'the release build still contains "$marker"',
      );
    }
  });

  test('forge.devtools=false compiles the devtools out of a debug OfflineClient.open', () async {
    final js = await _compile([
      '-O2',
      '-Dforge.devtools=false',
    ], '${dir.path}/optout.js');

    expect(js, contains(_control));
    for (final marker in _markers) {
      expect(
        js,
        isNot(contains(marker)),
        reason: 'the opt-out build still contains "$marker"',
      );
    }
  });
}
