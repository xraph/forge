import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// The README's Dart blocks are compiled by `flutter analyze` through
/// test/readme_examples.dart, which holds each of them verbatim. This test
/// is what keeps the two in step: edit a block in the README and it fails
/// until the same edit is made there, where the analyzer then checks it.
void main() {
  // Whitespace is not significant, so a re-wrapped line does not fail.
  String squash(String code) => code.replaceAll(RegExp(r'\s+'), ' ').trim();

  final readme = File('README.md').readAsStringSync();
  final examples = squash(File('test/readme_examples.dart').readAsStringSync());
  final blocks = [
    for (final match in RegExp(r'```dart\n(.*?)```', dotAll: true).allMatches(readme))
      match.group(1)!.replaceAll(
        'package:orders_forge_client/orders_forge_client.dart',
        'support/readme_stubs.dart',
      ),
  ];

  test('the README has Dart examples to check', () {
    expect(blocks, isNotEmpty);
  });

  for (var i = 0; i < blocks.length; i++) {
    test('README Dart block ${i + 1} is compiled in test/readme_examples.dart', () {
      expect(
        examples,
        contains(squash(blocks[i])),
        reason: 'paste this block into test/readme_examples.dart:\n${blocks[i]}',
      );
    });
  }

  test('the README has no em dashes', () {
    expect(readme, isNot(contains(String.fromCharCode(0x2014))));
  });

  test('the README depends on the packages by path or git, never a version', () {
    // A 1.0.0-dev, publish_to: none package cannot resolve a caret range.
    expect(readme, isNot(contains('^1.0.0')));
  });

  test('the README imports nothing from forge_client_flutter', () {
    // The barrel re-exports what the examples call, and an app that imports
    // a package it does not depend on directly fails depend_on_referenced_packages.
    final imports = RegExp(r"^import '([^']+)';", multiLine: true)
        .allMatches(readme)
        .map((match) => match.group(1)!);
    expect(imports, isNot(contains(startsWith('package:forge_client_flutter/'))));
  });
}
