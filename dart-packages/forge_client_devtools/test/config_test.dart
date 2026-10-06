import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, String> _yaml(String path) => {
  for (final line in File(path).readAsLinesSync())
    if (RegExp(r'^(\w+):\s*(.+)$').firstMatch(line) case final match?)
      match.group(1)!: match.group(2)!.trim(),
};

void main() {
  test(
    'the DevTools manifest names this package and carries every required field',
    () {
      final config = _yaml('extension/devtools/config.yaml');
      final pubspec = _yaml('pubspec.yaml');

      expect(config['name'], pubspec['name']);
      expect(config['version'], pubspec['version']);
      expect(config['issueTracker'], startsWith('https://'));
      expect(config['requiresConnection'], 'true');
      // The storage icon. If this fails, set materialIconCodePoint to the value
      // the failure prints; the test is the source of truth for the code point.
      expect(
        config['materialIconCodePoint'],
        "'0x${Icons.storage.codePoint.toRadixString(16)}'",
      );
    },
  );
}
