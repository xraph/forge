@TestOn('vm')
library;

import 'dart:io';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

// Run from the package root, as `dart test` does, so this reaches the real Go
// source in the monorepo. A missing file throws and fails the test: the pin
// never passes by having nothing to read.
const _goSource = '../../extensions/streaming/internal/streaming.go';

/// The value of every `MessageType*` constant in [source], typed or untyped.
Set<String> _declaredKinds(String source) => {
  for (final match in RegExp(
    r'MessageType\w+(?:\s+\w+)?\s*=\s*"([^"]+)"',
  ).allMatches(source))
    match.group(1)!,
};

/// How many identifiers start a line as `MessageType<Name>`, whatever follows.
/// A constant the extraction above cannot read still shows up here.
int _declaredNames(String source) =>
    RegExp(r'^\s*MessageType\w+', multiLine: true).allMatches(source).length;

void main() {
  // The Dart half of the wire-contract pin. An eighth kind added to the Go
  // extension and not here would reach this client as a frame name no binding
  // can claim, reported as an unknown message forever.
  test(
    'reserves exactly the transport kinds the Go streaming extension declares',
    () {
      final source = File(_goSource).readAsStringSync();
      final kinds = _declaredKinds(source);

      expect(kinds, isNotEmpty);
      expect(forgeTransportKinds, kinds);
    },
  );

  // The extraction is a regexp, so it is checked against the shapes it could
  // silently miss: a constant it reads nothing from would shrink the Go set and
  // let a Dart-side omission pass.
  test(
    'reads every MessageType constant in the Go source, so none is skipped',
    () {
      final source = File(_goSource).readAsStringSync();

      expect(_declaredKinds(source), hasLength(_declaredNames(source)));
      expect(
        _declaredKinds('MessageTypeA = "a"\n\tMessageTypeB MessageType = "b"'),
        {'a', 'b'},
      );
      expect(_declaredNames('MessageTypeA = "a"\n\tMessageTypeB = f()'), 2);
    },
  );
}
