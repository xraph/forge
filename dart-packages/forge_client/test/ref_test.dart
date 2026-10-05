// New in Dart: ref.ts has no suite of its own in TS (its rules are exercised
// through normalize and store); these pin the Dart-specific parts.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

void main() {
  group('entityKey', () {
    test('renders an id exactly as JavaScript String() does', () {
      expect(entityKey('Order', 7), 'Order:7');
      expect(entityKey('Order', 7.0), 'Order:7');
      expect(entityKey('Order', '7'), 'Order:7');
      expect(entityKey('Order', 1.5), 'Order:1.5');
      expect(
        entityKey('Order', BigInt.parse('9007199254740993')),
        'Order:9007199254740993',
      );
    });
  });

  group('isIdentity', () {
    test('accepts non-empty strings, finite numbers and big integers only', () {
      for (final good in <Object>['a', 0, -1, 2.5, BigInt.one]) {
        expect(isIdentity(good), isTrue, reason: '$good');
      }

      for (final bad in <Object?>[
        null,
        '',
        true,
        double.nan,
        double.infinity,
        <String, Object?>{},
        <Object?>[],
      ]) {
        expect(isIdentity(bad), isFalse, reason: '$bad');
      }
    });
  });

  group('references', () {
    test('are recognised by type, never by shape', () {
      expect(isRef(makeRef('Order:7')), isTrue);
      expect(isRef({'__ref': 'Order:7'}), isFalse);
    });

    test('compare by key through sameValue', () {
      expect(sameValue(makeRef('Order:7'), makeRef('Order:7')), isTrue);
      expect(sameValue(makeRef('Order:7'), makeRef('Order:8')), isFalse);
      expect(sameValue(7, 7.0), isTrue);
      expect(sameValue('7', 7), isFalse);
    });

    test('mark a rebuilt container without changing it', () {
      final node = <Object?>[1];

      expect(isRewritten(node), isFalse);
      expect(markRewritten(node), same(node));
      expect(isRewritten(node), isTrue);
      expect(node, [1]);
    });
  });
}
