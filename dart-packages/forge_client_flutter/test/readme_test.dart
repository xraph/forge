import 'dart:io';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';

String? noSelection() => null;

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
    for (final match in RegExp(
      r'```dart\n(.*?)```',
      dotAll: true,
    ).allMatches(readme))
      match
          .group(1)!
          .replaceAll(
            'package:orders_forge_client/orders_forge_client.dart',
            'support/readme_stubs.dart',
          ),
  ];

  test('the README has Dart examples to check', () {
    expect(blocks, isNotEmpty);
  });

  for (var i = 0; i < blocks.length; i++) {
    test(
      'README Dart block ${i + 1} is compiled in test/readme_examples.dart',
      () {
        expect(
          examples,
          contains(squash(blocks[i])),
          reason:
              'paste this block into test/readme_examples.dart:\n${blocks[i]}',
        );
      },
    );
  }

  testWidgets(
    'two const keys with the same function and label share one state per scope',
    (tester) async {
      // The README tells readers this, so it is pinned here.
      const same1 = ForgeStateKey<String?>(noSelection, debugLabel: 'selected');
      const same2 = ForgeStateKey<String?>(noSelection, debugLabel: 'selected');
      const other = ForgeStateKey<String?>(noSelection, debugLabel: 'other');
      late ForgeState<String?> a;
      late ForgeState<String?> b;
      late ForgeState<String?> c;

      await tester.pumpWidget(
        scope(
          harness((_, _) => null),
          Builder(
            builder: (context) {
              a = context.forgeState(same1);
              b = context.forgeState(same2);
              c = context.forgeState(other);
              return const SizedBox();
            },
          ),
        ),
      );

      expect(identical(a, b), isTrue);
      expect(identical(a, c), isFalse);
    },
  );

  test('the README has no em dashes', () {
    expect(readme, isNot(contains(String.fromCharCode(0x2014))));
  });

  test('the README pins a ref on every git dependency', () {
    // Pub pins this package's own path dependency to the commit it resolved,
    // so a git entry without the matching ref fails to resolve.
    final entries = RegExp(r'git:\n((?: {6}.*\n)+)')
        .allMatches(readme)
        .toList();
    expect(entries, hasLength(2));
    for (final entry in entries) {
      expect(entry.group(1), contains('ref: <commit sha>'));
    }
  });

  test(
    'the README depends on the packages by path or git, never a version',
    () {
      // A 1.0.0-dev, publish_to: none package cannot resolve a caret range.
      expect(readme, isNot(contains('^1.0.0')));
    },
  );
}
