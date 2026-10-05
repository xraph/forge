// New in Dart: the value types of operation.dart and security.dart, which
// have no TypeScript module of their own.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

void main() {
  group('TagContext', () {
    test('is empty only when it carries nothing', () {
      expect(TagContext.empty.isEmpty, isTrue);
      expect(const NoArgs().toTagContext(), same(TagContext.empty));
      expect(const TagContext(path: {'id': 1}).isEmpty, isFalse);
      expect(const TagContext(headers: {'x': 'y'}).isEmpty, isFalse);
      expect(const TagContext(body: <String, Object?>{}).isEmpty, isFalse);
    });
  });

  group('Value', () {
    test('tells unchanged from an assigned null', () {
      String read(Value<String> value) => switch (value) {
        Unchanged() => 'unchanged',
        Assign(value: null) => 'cleared',
        Assign(:final value) => 'set $value',
      };

      expect(read(const Unchanged()), 'unchanged');
      expect(read(const Assign(null)), 'cleared');
      expect(read(const Assign('x')), 'set x');
    });

    test('compares by variant and value, so rebuilt args compare equal', () {
      expect(const Unchanged<String>(), const Unchanged<int>());
      expect(Assign<String>('x'.toUpperCase()), const Assign<String>('X'));
      expect(
        Assign<String>('x'.toUpperCase()).hashCode,
        const Assign<String>('X').hashCode,
      );
      expect(const Assign<String>(null), isNot(const Unchanged<String>()));
      expect(const Assign<String>('a'), isNot(const Assign<String>('b')));
    });
  });

  group('SecurityScheme', () {
    test('holds a scheme row as the generator emits it', () {
      const bearer = SecurityScheme(type: 'http', scheme: 'bearer');
      const key = SecurityScheme(
        type: 'apiKey',
        name: 'X-Api-Key',
        location: 'header',
      );

      expect(bearer.scheme, 'bearer');
      expect(key.location, 'header');
    });
  });
}
