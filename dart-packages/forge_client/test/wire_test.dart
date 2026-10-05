import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/wire.dart';
import 'package:test/test.dart';

const _where = EncodeContext(query: 'GET /orders({})');

Matcher _throwsMessage(String pattern) => throwsA(
  isA<StateError>().having(
    (error) => error.message,
    'message',
    matches(pattern),
  ),
);

void main() {
  group('encode', () {
    test('emits a reference as a plain marker object and reports its key', () {
      final result = encode({'order': const EntityRef('Order:7')}, _where);

      expect(result.value, {
        'order': {'__ref': 'Order:7'},
      });
      expect(result.refs, ['Order:7']);
    });

    test('escapes response data that is shaped exactly like a reference', () {
      final result = encode({
        'meta': {'__ref': 'not a reference'},
      }, _where);

      expect(result.value, {
        'meta': {'___ref': 'not a reference'},
      });
      expect(result.refs, isEmpty);
    });

    test('escapes an already-escape-shaped key, so the scheme nests', () {
      expect(encode({'___ref': 1, '____ref': 2}, _where).value, {
        '____ref': 1,
        '_____ref': 2,
      });
    });

    test('leaves every other key alone', () {
      expect(encode({'__refs': 1, 'ref': 2, '_ref': 3}, _where).value, {
        '__refs': 1,
        'ref': 2,
        '_ref': 3,
      });
    });

    test(
      'reports references found at any depth, deduplication left to the caller',
      () {
        final result = encode({
          'rows': [
            {'o': const EntityRef('Order:1')},
            {'o': const EntityRef('Order:2')},
            const EntityRef('Order:1'),
          ],
        }, _where);

        expect(result.refs, ['Order:1', 'Order:2', 'Order:1']);
      },
    );

    test('allows the same object twice through different branches -- a DAG is not a cycle', () {
      final shared = {'n': 1};

      expect(encode({'a': shared, 'b': shared}, _where).value, {
        'a': {'n': 1},
        'b': {'n': 1},
      });
    });

    test('throws on a cycle, naming the query and the path', () {
      final node = <String, Object?>{'id': 7};
      node['self'] = node;

      expect(() => encode(node, _where), _throwsMessage('cyclic value'));
      expect(() => encode(node, _where), _throwsMessage(r'skeleton\.self'));
    });

    test('names the record when one is being encoded', () {
      final node = <String, Object?>{};
      node['meta'] = {'self': node};

      const record = EncodeContext(query: 'GET /orders({})', entity: 'Order:7');

      expect(() => encode(node, record), _throwsMessage('entity {2}Order:7'));
      expect(() => encode(node, record), _throwsMessage(r'data\.meta\.self'));
    });

    test('reports an array index in the path', () {
      final row = <String, Object?>{};
      row['rows'] = [row];

      expect(() => encode(row, _where), _throwsMessage(r'skeleton\.rows\[0\]'));
    });
  });

  group('cycles through lists alone', () {
    // No TS counterpart. The TS cases route every cycle through an object, so
    // a missing check on the array branch would go unnoticed.
    test('encode throws on a list that contains itself', () {
      final list = <Object?>[];
      list.add(list);

      expect(() => encode(list, _where), _throwsMessage(r'skeleton\[0\]'));
    });

    test('encode accepts one list reached through two branches', () {
      final shared = [1, 2];

      expect(encode({'a': shared, 'b': shared}, _where).value, {
        'a': [1, 2],
        'b': [1, 2],
      });
    });

    test('assertAcyclic throws on a list that contains itself', () {
      final list = <Object?>[];
      list.add(list);

      expect(
        () => assertAcyclic(list, _where),
        _throwsMessage(r'skeleton\[0\]'),
      );
    });
  });

  group('assertAcyclic', () {
    test('accepts an acyclic value', () {
      expect(
        () => assertAcyclic({
          'a': [
            1,
            {'b': 2},
          ],
        }, _where),
        returnsNormally,
      );
    });

    test('accepts a DAG', () {
      final shared = {'n': 1};

      expect(
        () => assertAcyclic({'a': shared, 'b': shared}, _where),
        returnsNormally,
      );
    });

    test('throws on a cycle', () {
      final node = <String, Object?>{};
      node['self'] = node;

      expect(() => assertAcyclic(node, _where), _throwsMessage('cyclic value'));
    });
  });

  group('revive', () {
    test('mints a genuine reference the runtime recognises', () {
      final revived =
          revive({
                'order': {'__ref': 'Order:7'},
              })!
              as Map<String, Object?>;

      expect(isRef(revived['order']), isTrue);
    });

    test(
      'unescapes data that was shaped like a reference, and does not mint one',
      () {
        final revived =
            revive({
                  'meta': {'___ref': 'not a reference'},
                })!
                as Map<String, Object?>;

        expect(revived['meta'], {'__ref': 'not a reference'});
        expect(isRef(revived['meta']), isFalse);
      },
    );

    test('marks a container that has a reference beneath it', () {
      final revived =
          revive({
                'rows': [
                  {'__ref': 'Order:7'},
                ],
              })!
              as Map<String, Object?>;

      expect(isRewritten(revived['rows']!), isTrue);
      expect(isRewritten(revived), isTrue);
    });

    test('leaves a container with no reference beneath it unmarked and by identity', () {
      final input = {
        'totals': {'open': 3},
      };
      final revived = revive(input)! as Map<String, Object?>;

      expect(revived, same(input));
      expect(isRewritten(revived['totals']!), isFalse);
    });

    test('does not mark a container that only needed unescaping', () {
      final revived = revive({
        'meta': {'___ref': 'x'},
      })!;

      expect(isRewritten(revived), isFalse);
    });

    test(
      'ignores a marker-shaped object carrying anything but a lone string',
      () {
        expect(isRef(revive({'__ref': 7})), isFalse);
        expect(isRef(revive({'__ref': 'Order:7', 'extra': 1})), isFalse);
      },
    );

    test('round-trips through JSON', () {
      final encoded = encode({
        'rows': [
          const EntityRef('Order:7'),
          {'__ref': 'data'},
        ],
      }, _where);
      final revived =
          revive(jsonDecode(jsonEncode(encoded.value)))!
              as Map<String, Object?>;
      final rows = revived['rows']! as List<Object?>;

      expect(isRef(rows[0]), isTrue);
      expect(rows[1], {'__ref': 'data'});
      expect(isRef(rows[1]), isFalse);
    });
  });

  // The cases below have no TS counterpart. JSON.stringify makes four choices
  // for free that Dart's jsonEncode does not, so `encode` makes them explicitly
  // and these pin that a Dart snapshot is the same document TS would write.
  group('JSON text parity with the TS encoder', () {
    String text(Object? node) => jsonEncode(encode(node, _where).value);

    test('writes an integer-valued double as an integer', () {
      expect(
        text({'whole': 7.0, 'big': 1e15, 'frac': 0.5}),
        '{"whole":7,"big":1000000000000000,"frac":0.5}',
      );
    });

    test('writes negative zero as 0', () {
      expect(text([-0.0, 0.0, 0]), '[0,0,0]');
    });

    test('writes a non-finite number as null', () {
      expect(
        text([double.nan, double.infinity, double.negativeInfinity]),
        '[null,null,null]',
      );
    });

    test(
      'orders integer-like keys first and ascending, then the rest as inserted',
      () {
        expect(
          text({
            'zeta': 1,
            '10': 2,
            'alpha': 3,
            '2': 4,
            '01': 5,
            '4294967295': 6,
            '4294967294': 7,
            '-1': 8,
          }),
          '{"2":4,"10":2,"4294967294":7,"zeta":1,"alpha":3,"01":5,"4294967295":6,"-1":8}',
        );
      },
    );

    test(
      'writes the exact document the TS encoder writes for a mixed payload',
      () {
        // Produced by running packages/client-core/src/wire.ts `encode` and
        // JSON.stringify over the equivalent JS value.
        final document = text({
          'zeta': 1,
          '10': 'ten',
          'alpha': {'__ref': 'looks like one', 'n': 1},
          '2': 'two',
          'order': const EntityRef('Order:7'),
          'meta': {'__ref': 'lone'},
          'esc': {'___ref': 1, '01': 'padded'},
          'rows': [
            const EntityRef('Order:1'),
            {'o': const EntityRef('Order:2'), '5': true, 'b': null},
          ],
          'neg': -0.0,
          'whole': 7.0,
          'nan': double.nan,
          'inf': double.infinity,
          'frac': 0.5,
          'text': 'a"b',
        });

        expect(
          document,
          '{"2":"two","10":"ten","zeta":1,"alpha":{"___ref":"looks like one","n":1},'
          '"order":{"__ref":"Order:7"},"meta":{"___ref":"lone"},'
          '"esc":{"____ref":1,"01":"padded"},'
          '"rows":[{"__ref":"Order:1"},{"5":true,"o":{"__ref":"Order:2"},"b":null}],'
          '"neg":0,"whole":7,"nan":null,"inf":null,"frac":0.5,"text":"a\\"b"}',
        );
      },
    );
  });

  group('revive keeps the document it was given', () {
    test('keeps key order and mints references from a TS document', () {
      final revived =
          revive(
                jsonDecode(
                  '{"2":"two","zeta":1,"order":{"__ref":"Order:7"},"meta":{"___ref":"lone"}}',
                ),
              )!
              as Map<String, Object?>;

      expect(revived.keys, ['2', 'zeta', 'order', 'meta']);
      expect((revived['order']! as EntityRef).key, 'Order:7');
      expect(revived['meta'], {'__ref': 'lone'});
    });

    test(
      'does not report a change for a primitive that merely equals its input',
      () {
        // A double travels through unchanged; the container must stay by identity.
        final input = {
          'rate': 0.1,
          'ids': [1, 2, 3],
        };

        expect(revive(input), same(input));
      },
    );
  });

  // No TS counterpart: TS writes the denormalized mode with JSON.stringify,
  // which Dart has no equivalent of, so the snapshot routes it through this.
  group('encodePlain', () {
    test(
      'writes JSON.stringify text and leaves reference-shaped keys alone',
      () {
        final value = {
          'zeta': 7.0,
          '10': -0.0,
          '2': double.nan,
          'meta': {'__ref': 'not a reference', '___ref': 'nor this'},
        };

        expect(
          jsonEncode(encodePlain(value, _where)),
          '{"2":null,"10":0,"zeta":7,"meta":{"__ref":"not a reference","___ref":"nor this"}}',
        );
      },
    );

    test('throws on a cycle and accepts a DAG', () {
      final shared = <String, Object?>{'id': 1};
      final node = <String, Object?>{'a': shared, 'b': shared};

      expect(() => encodePlain(node, _where), returnsNormally);

      node['self'] = node;

      expect(() => encodePlain(node, _where), _throwsMessage('cyclic value'));
    });
  });
}
