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
