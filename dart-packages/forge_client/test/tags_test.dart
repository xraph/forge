// Ported from packages/client-core/__tests__/tags.test.ts.
//
// TS folds the response into its TagContext; Dart passes it as the third
// argument to resolveTag and resolveTags, so `ctx` below is a pair.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

const path = {'id': 7, 'customerId': 'from-path'};
const query = {'status': 'open', 'customerId': 'from-query'};
const body = {
  'customerId': 'from-body',
  'nested': {'id': 'b-1'},
  'note': null,
};
const response = {
  'id': 9,
  'customerId': 'from-response',
  'customer': {'id': 'c-3'},
};
const ctx = TagContext(path: path, query: query, body: body);

String operationKey(String operation, [TagContext? args]) =>
    operationQueryKey(operation, args);

void main() {
  group('resolveTag', () {
    test('returns a template with no placeholder unchanged', () {
      expect(resolveTag('Order[]', TagContext.empty), 'Order[]');
    });

    test('resolves from the path', () {
      expect(
        resolveTag('Order:{id}', const TagContext(path: {'id': 7})),
        'Order:7',
      );
    });

    test('resolves from the query string', () {
      expect(
        resolveTag(
          'Order:{cursor}',
          const TagContext(query: {'cursor': 'abc'}),
        ),
        'Order:abc',
      );
    });

    test('resolves from the request body', () {
      expect(
        resolveTag(
          'Customer:{customerId}',
          const TagContext(body: {'customerId': 'c-3'}),
        ),
        'Customer:c-3',
      );
    });

    test('resolves from the response', () {
      expect(resolveTag('Order:{id}', TagContext.empty, {'id': 9}), 'Order:9');
    });

    test('resolves an explicit {req.x} against the request only', () {
      expect(
        resolveTag('Customer:{req.customerId}', ctx, response),
        'Customer:from-path',
      );
      expect(
        resolveTag('Customer:{req.customerId}', TagContext.empty, {
          'customerId': 'r',
        }),
        isNull,
      );
    });

    test('resolves an explicit {res.a.b} against the response only', () {
      expect(
        resolveTag('Customer:{res.customer.id}', ctx, response),
        'Customer:c-3',
      );
      expect(
        resolveTag(
          'Customer:{res.customerId}',
          const TagContext(body: {'customerId': 'b'}),
        ),
        isNull,
      );
    });

    test('walks a dotted path into the request body', () {
      expect(resolveTag('Order:{req.nested.id}', ctx, response), 'Order:b-1');
    });

    test(
      'accepts a wire-spelled placeholder against a client-cased payload',
      () {
        expect(
          resolveTag(
            'Customer:{req.customer_id}',
            const TagContext(body: {'customerId': 'c-3'}),
          ),
          'Customer:c-3',
        );
        expect(
          resolveTag('Ledger:{res.customer.external_id}', TagContext.empty, {
            'customer': {'externalId': 'x-9'},
          }),
          'Ledger:x-9',
        );
        expect(
          resolveTag(
            'Customer:{customerId}',
            const TagContext(body: {'customer_id': 'c-4'}),
          ),
          'Customer:c-4',
        );
      },
    );

    test('reads an exact key as written when both spellings are present', () {
      expect(
        resolveTag(
          'Customer:{req.customer_id}',
          const TagContext(
            body: {'customer_id': 'wire', 'customerId': 'client'},
          ),
        ),
        'Customer:wire',
      );
    });

    test('still resolves to nothing when no spelling matches', () {
      expect(
        resolveTag(
          'Customer:{req.customer_id}',
          const TagContext(body: {'id': 1}),
        ),
        isNull,
      );
    });

    // Path, then query, then body, then response: first match wins.
    final bare = <(String, TagContext, Object?)>[
      ('Customer:from-path', ctx, response),
      (
        'Customer:from-query',
        const TagContext(query: query, body: body),
        response,
      ),
      ('Customer:from-body', const TagContext(body: body), response),
      (
        'Customer:from-response',
        const TagContext(body: <String, Object?>{}),
        response,
      ),
    ];

    for (final (expected, context, answer) in bare) {
      test('resolves a bare placeholder to $expected', () {
        expect(resolveTag('Customer:{customerId}', context, answer), expected);
      });
    }

    test('stops at a source that holds null rather than falling through', () {
      expect(
        resolveTag('Note:{note}', const TagContext(body: {'note': null}), {
          'note': 'n-1',
        }),
        isNull,
      );
    });

    final unusable = <(String, String, TagContext)>[
      ('nothing anywhere', 'Customer:{customerId}', TagContext.empty),
      (
        'an empty string',
        'Customer:{customerId}',
        const TagContext(path: {'customerId': ''}),
      ),
      ('NaN', 'Order:{id}', const TagContext(query: {'id': double.nan})),
      (
        'an object',
        'Order:{id}',
        const TagContext(
          body: {
            'id': {'nested': true},
          },
        ),
      ),
      (
        'an unknown explicit source',
        'Order:{ctx.id}',
        const TagContext(path: {'id': 7}),
      ),
    ];

    for (final (name, template, context) in unusable) {
      test('resolves to undefined, never the empty string, for $name', () {
        expect(resolveTag(template, context), isNull);
      });
    }

    test(
      'fails the whole template when one of several placeholders is missing',
      () {
        expect(
          resolveTag('Order:{id}:{missing}', const TagContext(path: {'id': 7})),
          isNull,
        );
      },
    );

    test('substitutes every placeholder in a multi-part template', () {
      expect(
        resolveTag(
          'Order:{id}:{req.status}',
          const TagContext(path: {'id': 7}, query: {'status': 'open'}),
        ),
        'Order:7:open',
      );
    });

    test('accepts numbers, bigints and booleans as values', () {
      expect(resolveTag('A:{a}', const TagContext(path: {'a': 0})), 'A:0');
      expect(
        resolveTag('A:{a}', TagContext(path: {'a': BigInt.from(10)})),
        'A:10',
      );
      expect(
        resolveTag('A:{a}', const TagContext(path: {'a': false})),
        'A:false',
      );
      // Dart-only: an integral double renders as JavaScript renders it.
      expect(resolveTag('A:{a}', const TagContext(path: {'a': 7.0})), 'A:7');
    });
  });

  group('resolveTags', () {
    test('separates what resolved from what did not, and deduplicates', () {
      final result = resolveTags([
        'Order[]',
        'Order:{id}',
        'Order:{id}',
        'Customer:{missing}',
      ], const TagContext(path: {'id': 7}));

      expect(result.tags, ['Order[]', 'Order:7']);
      expect(result.unresolved, ['Customer:{missing}']);
    });

    test(
      'resolves a response template once per element of an array response',
      () {
        final result = resolveTags(
          ['Order:{id}', 'Order[]'],
          TagContext.empty,
          [
            {'id': 1},
            {'id': 2},
            {'id': 1},
          ],
        );

        expect(result.tags, ['Order:1', 'Order:2', 'Order[]']);
        expect(result.unresolved, isEmpty);
      },
    );

    test(
      'treats an empty array response as providing nothing, not as unresolved',
      () {
        final result = resolveTags(
          ['Order:{res.id}'],
          TagContext.empty,
          <Object?>[],
        );

        expect(result.tags, isEmpty);
        expect(result.unresolved, isEmpty);
      },
    );

    test('still reports a template no element of the array can answer', () {
      final result = resolveTags(
        ['Customer:{res.customerId}'],
        TagContext.empty,
        [
          {'id': 1},
        ],
      );

      expect(result.unresolved, ['Customer:{res.customerId}']);
    });

    test('does not let an array response answer a request-only template', () {
      final result = resolveTags(
        ['Customer:{req.customerId}'],
        TagContext.empty,
        [
          {'customerId': 'r'},
        ],
      );

      expect(result.unresolved, ['Customer:{req.customerId}']);
    });

    test(
      'prefers the request over the array response for a bare placeholder',
      () {
        final result = resolveTags(
          ['Customer:{customerId}'],
          const TagContext(query: {'customerId': 'q'}),
          [
            {'customerId': 'r'},
          ],
        );

        expect(result.tags, ['Customer:q']);
      },
    );
  });

  group('queryKey', () {
    test('is stable under key order', () {
      expect(
        operationKey('orderList', const TagContext(query: {'a': 1, 'b': 2})),
        operationKey('orderList', const TagContext(query: {'b': 2, 'a': 1})),
      );
    });

    test(
      'treats an absent argument and an undefined one as the same request',
      () {
        expect(
          operationKey(
            'orderList',
            const TagContext(query: {'a': 1, 'b': null}),
          ),
          operationKey('orderList', const TagContext(query: {'a': 1})),
        );
      },
    );

    test('separates different arguments and different operations', () {
      expect(
        operationKey('orderList', const TagContext(query: {'page': 1})),
        isNot(operationKey('orderList', const TagContext(query: {'page': 2}))),
      );
      expect(operationKey('orderList'), isNot(operationKey('orderCount')));
    });

    test('keeps array order significant', () {
      expect(
        operationKey(
          'op',
          const TagContext(
            query: {
              'ids': [1, 2],
            },
          ),
        ),
        isNot(
          operationKey(
            'op',
            const TagContext(
              query: {
                'ids': [2, 1],
              },
            ),
          ),
        ),
      );
    });

    // Dart-only: the exact bytes, because a Dart snapshot and a TS dehydrate
    // payload are keyed by these strings (plan 01b) and must agree.
    test('spells a key exactly as the TypeScript runtime does', () {
      const meta = OperationMeta(
        id: 'op_list_orders',
        method: 'GET',
        path: '/orders',
      );

      expect(queryKey(meta, TagContext.empty), 'GET /orders');
      expect(
        queryKey(
          meta,
          const TagContext(path: {'id': 7}, query: {'b': 'x', 'a': 1.0}),
        ),
        'GET /orders|{"path":{"id":7},"query":{"a":1,"b":"x"}}',
      );
    });
  });
}
