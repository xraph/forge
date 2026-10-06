@TestOn('vm')
@Timeout(Duration(minutes: 5))
library;

import 'dart:convert';
import 'dart:io';

import 'package:test/test.dart';

// Review Focus 2. A claim about tree-shaking argued from source is a hope;
// this compiles a program that calls configureClient and reads the bytes.
const _fixture = 'test/devtools/fixtures/release_app.dart';

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

Future<String> _compile(List<String> args, String out) async {
  final result = await Process.run(Platform.resolvedExecutable, [
    'compile',
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
    () async => dir = await Directory.systemTemp.createTemp('forge_release_'),
  );
  tearDownAll(() => dir.delete(recursive: true));

  test('a debug dart2js build carries the devtools, so the checks below can fail', () async {
    final js = await _compile(['js', '-O2'], '${dir.path}/debug.js');

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

  test(
    'a release dart2js build (dart.vm.product) contains no devtools',
    () async {
      final js = await _compile([
        'js',
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
    },
  );

  test(
    'forge.devtools=false compiles the devtools out of a debug build',
    () async {
      final js = await _compile([
        'js',
        '-O2',
        '-Dforge.devtools=false',
      ], '${dir.path}/optout.js');

      expect(js, contains(_control));
      for (final marker in _markers) {
        expect(js, isNot(contains(marker)));
      }
    },
  );

  test(
    'an AOT executable, always a product build, contains no devtools',
    () async {
      final exe = await _compile([
        'exe',
      ], '${dir.path}/release_app${Platform.isWindows ? '.exe' : ''}');

      expect(exe, contains(_control));
      for (final marker in _markers) {
        expect(
          exe,
          isNot(contains(marker)),
          reason: 'the AOT executable still contains "$marker"',
        );
      }
    },
  );
}
