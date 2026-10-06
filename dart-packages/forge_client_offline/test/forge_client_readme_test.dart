import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// forge_client's README shows how to wire the devtools under an outbox, and
/// forge_client cannot compile that itself (the outbox lives in this package).
/// Its blocks are pasted into test/forge_client_readme_examples.dart, which
/// `flutter analyze` compiles. This test keeps the two in step.
void main() {
  // Whitespace is not significant, so a re-wrapped line does not fail.
  String squash(String code) => code.replaceAll(RegExp(r'\s+'), ' ').trim();

  final readme = File('../forge_client/README.md').readAsStringSync();
  final start = readme.indexOf('\n## Devtools');
  final end = readme.indexOf('\n## ', start + 1);
  final section = readme.substring(start, end);
  final examples = squash(
    File('test/forge_client_readme_examples.dart').readAsStringSync(),
  );
  final blocks = [
    for (final match in RegExp(
      r'```dart\n(.*?)```',
      dotAll: true,
    ).allMatches(section))
      match.group(1)!,
  ];

  test('the Devtools section has Dart examples to check', () {
    expect(start, isNonNegative);
    expect(blocks, hasLength(5));
  });

  for (var i = 0; i < blocks.length; i++) {
    test(
      'forge_client README devtools block ${i + 1} is compiled in test/forge_client_readme_examples.dart',
      () {
        expect(
          examples,
          contains(squash(blocks[i])),
          reason:
              'paste this block into test/forge_client_readme_examples.dart:\n${blocks[i]}',
        );
      },
    );
  }
}
