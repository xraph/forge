import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

List<String> _lines(String path) => [
  for (final line in File(path).readAsLinesSync())
    if (line.trim().isNotEmpty && !line.trimLeft().startsWith('#')) line.trim(),
];

void main() {
  test('git ignores the built extension, and pub still publishes it', () {
    expect(_lines('.gitignore'), contains('/extension/devtools/build/'));
    expect(_lines('.pubignore'), isNot(contains('/extension/devtools/build/')));
    expect(_lines('.pubignore'), contains('/build/'));
  });

  test('no ignore file above the package hides the built extension', () {
    // pub reads every .gitignore from the repository root down to the
    // package, and .pubignore replaces only the package's own. So copy the
    // ancestors' ignore files, and nothing else, into a scratch repository
    // and ask git whether the built extension would still be ignored.
    final package = Directory.current.absolute;
    final ancestors = <Directory>[];
    for (var dir = package.parent; ; dir = dir.parent) {
      ancestors.add(dir);
      if (FileSystemEntity.typeSync('${dir.path}/.git') !=
          FileSystemEntityType.notFound) {
        break;
      }
      if (dir.parent.path == dir.path) {
        fail('no git repository above ${package.path}');
      }
    }
    final root = ancestors.last.path;
    String relative(String path) =>
        path == root ? '' : '${path.substring(root.length + 1)}/';

    final scratch = Directory.systemTemp.createTempSync('ignore_probe');
    addTearDown(() => scratch.deleteSync(recursive: true));
    for (final dir in ancestors) {
      final ignore = File('${dir.path}/.gitignore');
      if (!ignore.existsSync()) continue;
      File('${scratch.path}/${relative(dir.path)}.gitignore')
        ..createSync(recursive: true)
        ..writeAsStringSync(ignore.readAsStringSync());
    }
    final probe =
        '${relative(package.path)}extension/devtools/build/index.html';
    File('${scratch.path}/$probe').createSync(recursive: true);

    final init = Process.runSync('git', [
      'init',
      '-q',
    ], workingDirectory: scratch.path);
    expect(init.exitCode, 0, reason: '${init.stderr}');
    // Exit 1 means not ignored. A global excludes file is not pub's concern.
    final check = Process.runSync('git', [
      '-c',
      'core.excludesFile=',
      'check-ignore',
      '-v',
      probe,
    ], workingDirectory: scratch.path);
    expect(check.exitCode, 1, reason: 'ignored by ${check.stdout}');
  });

  test('git ignores the files pub and flutter write', () {
    expect(_lines('.gitignore'), contains('.flutter-plugins-dependencies'));
    expect(_lines('.pubignore'), contains('.flutter-plugins-dependencies'));
  });

  test('the build script builds with the pinned SDK and validates', () {
    final script = File('tool/build_extension.sh').readAsStringSync();

    expect(
      script,
      contains('build_and_copy --source=. --dest=extension/devtools'),
    );
    expect(script, contains('validate --package=.'));
    expect(script, contains('fvm'));
  });
}
