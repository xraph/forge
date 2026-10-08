// New in Dart: the fourth adversarial review of the embedded-entity
// optimistic fix. Q4 and Q11 are the reviewer's probes turned into
// regression tests; the rest pin the cases around them. A `Line` is an
// entity when it carries a sku and a value object (a note) when it does not,
// so one list holds both. `Order.fromClientSorted` is a hand-written codec
// that sorts its lines on decode, which generated models never do.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';

const EntitySchema schema = {
  'Order': EntityMeta(idField: 'id', fields: {'lines': 'Line'}),
  'Line': EntityMeta(idField: 'sku'),
};

DateTime? _time(Object? value) =>
    value == null ? null : DateTime.parse(value as String);

final class Line {
  const Line({this.sku, this.qty = 0, this.at, this.note});

  final String? sku;
  final int qty;
  final DateTime? at;
  final String? note;

  static Line fromClient(Object? client) {
    final json = client! as Json;

    return Line(
      sku: json['sku'] as String?,
      qty: (json['qty'] as int?) ?? 0,
      at: _time(json['at']),
      note: json['note'] as String?,
    );
  }

  Json toClient() => {
    'sku': ?sku,
    'qty': qty,
    if (at case final value?) 'at': value.toIso8601String(),
    'note': ?note,
  };

  Line copyWith({int? qty, String? note}) =>
      Line(sku: sku, qty: qty ?? this.qty, at: at, note: note ?? this.note);
}

final class Order {
  const Order({required this.id, required this.status, this.lines = const []});

  final int id;
  final String status;
  final List<Line> lines;

  static Order fromClient(Object? client) => _decode(client, sort: false);

  static Order fromClientSorted(Object? client) => _decode(client, sort: true);

  static Order _decode(Object? client, {required bool sort}) {
    final json = client! as Json;
    final lines = [
      for (final line in (json['lines'] as List<Object?>?) ?? const [])
        decodeCached(Line.fromClient, line),
    ];

    if (sort) lines.sort((a, b) => (a.sku ?? '').compareTo(b.sku ?? ''));

    return Order(
      id: json['id']! as int,
      status: json['status']! as String,
      lines: lines,
    );
  }

  Json toClient() => {
    'id': id,
    'status': status,
    'lines': [for (final line in lines) line.toClient()],
  };

  Order copyWith({List<Line>? lines}) =>
      Order(id: id, status: status, lines: lines ?? this.lines);
}

const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  rootType: 'Order',
);

const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order[]'],
  rootType: 'Order',
);

const order7 = TagContext(path: {'id': 7});

final class OrderArgs implements OperationArgs {
  const OrderArgs();

  @override
  TagContext toTagContext() => order7;
}

MutationBinding<Order, OrderArgs, Order> updateOrder({bool sorted = false}) =>
    mutation<Order, OrderArgs, Order>(
      opUpdateOrder,
      Order.fromClient,
      entityFromClient: sorted ? Order.fromClientSorted : Order.fromClient,
      entityToClient: (order) => order.toClient(),
    );

/// Three timestamps in one microsecond, as Go spells them. A Dart
/// `DateTime` encodes each as `...36.123456Z`.
const nanosA = '2026-10-07T18:12:36.123456789Z';
const nanosB = '2026-10-07T18:12:36.123456111Z';
const nanosC = '2026-10-07T18:12:36.123456555Z';

/// Holds [order] as `Order:7`, runs [write] with `key: 'Order:7'` and
/// settles it with a 204. [before] runs once the order is held.
Future<(QueryCache, List<Object>)> promoted(
  Json order,
  Order Function(Order order) write, {
  bool sorted = false,
  void Function(QueryCache cache)? before,
}) async {
  final gate = Completer<Object?>();
  final reported = <Object>[];
  final cache = QueryCache(
    transport: FakeTransport(
      (request, _) => request.meta.method == 'GET' ? order : gate.future,
    ),
    entities: schema,
    scheduler: ManualScheduler(),
    onError: (error, _) => reported.add(error),
  );

  await cache.fetch(opGetOrder, order7);
  before?.call(cache);

  final pending = updateOrder(sorted: sorted)(
    cache,
    const OrderArgs(),
    optimistic: OptimisticUpdate(write, key: 'Order:7'),
  );

  gate.complete(null);
  await pending.then<void>((_) {}, onError: (Object _) {});

  return (cache, reported);
}

Json record(QueryCache cache, String key) => cache.store.getRecord(key)!.data;

int version(QueryCache cache, String key) =>
    cache.store.getRecord(key)!.version;

List<Object?> lines(QueryCache cache) =>
    record(cache, 'Order:7')['lines']! as List<Object?>;

void main() {
  group('I-R4a: a value object edited in place keeps its raw bytes', () {
    test('Q4: the untouched timestamp of an edited note survives', () async {
      final (cache, reported) = await promoted(
        {
          'id': 7,
          'status': 'open',
          'lines': [
            {'note': 'gift', 'qty': 0, 'at': nanosB},
          ],
        },
        (order) =>
            order.copyWith(lines: [order.lines[0].copyWith(note: 'gift!')]),
      );

      expect(lines(cache), [
        {'note': 'gift!', 'qty': 0, 'at': nanosB},
      ]);
      expect(reported, isEmpty);
    });

    test('a note and an entity both edited in place stay exact', () async {
      final (cache, _) = await promoted(
        {
          'id': 7,
          'status': 'open',
          'lines': [
            {'sku': 'a', 'qty': 1, 'at': nanosA},
            {'note': 'gift', 'qty': 0, 'at': nanosB},
          ],
        },
        (order) => order.copyWith(
          lines: [
            order.lines[0].copyWith(qty: 2),
            order.lines[1].copyWith(note: 'gift!'),
          ],
        ),
      );

      expect(lines(cache), [
        refTo('Line:a'),
        {'note': 'gift!', 'qty': 0, 'at': nanosB},
      ]);
      expect(record(cache, 'Line:a'), {'sku': 'a', 'qty': 2, 'at': nanosA});
    });

    test('Q3: a shift ahead of a note keeps the entities exact', () async {
      // Documented: once a removal shifts a value object, it pairs only where
      // it encodes the same, so an edited or respelled one comes back
      // encoded. The entities still pair by identity.
      final (cache, _) = await promoted({
        'id': 7,
        'status': 'open',
        'lines': [
          {'sku': 'a', 'qty': 1, 'at': nanosA},
          {'note': 'gift', 'qty': 0, 'at': nanosB},
          {'sku': 'b', 'qty': 1, 'at': nanosC},
        ],
      }, (order) => order.copyWith(lines: [order.lines[1], order.lines[2]]));

      final shown = lines(cache);
      expect((shown[0]! as Json)['note'], 'gift');
      expect(shown[1], refTo('Line:b'));
      expect(record(cache, 'Line:b')['at'], nanosC);
      expect(version(cache, 'Line:b'), 1);
    });
  });

  group('M-R4b: a codec that reorders never costs the caller an edit', () {
    test('Q11: a moved-in entity keeps the value the caller set', () async {
      final (cache, reported) = await promoted(
        {
          'id': 7,
          'status': 'open',
          'lines': [
            {'sku': 'b', 'qty': 1, 'at': nanosB},
            {'sku': 'a', 'qty': 1, 'at': nanosA},
          ],
        },
        (order) => order.copyWith(
          lines: [
            ...order.lines,
            Line(sku: '0', qty: 1, at: DateTime.parse(nanosC)),
          ],
        ),
        sorted: true,
        before: (cache) =>
            cache.store.put('Line:0', {'sku': '0', 'qty': 5, 'at': nanosC}),
      );

      expect(record(cache, 'Line:0')['qty'], 1);
      expect(record(cache, 'Line:a')['at'], nanosA);
      expect(record(cache, 'Line:b')['at'], nanosB);
      expect(reported, isEmpty);
    });

    test('a paired entity keeps an edit that matches a neighbour', () async {
      // The sorted decode puts `a` where `b` stood. `b`'s new qty equals
      // `a`'s, so comparing with the wrong neighbour would lose it.
      final (cache, _) = await promoted(
        {
          'id': 7,
          'status': 'open',
          'lines': [
            {'sku': 'b', 'qty': 1, 'at': nanosB},
            {'sku': 'a', 'qty': 5, 'at': nanosA},
          ],
        },
        (order) => order.copyWith(
          lines: [
            for (final line in order.lines)
              line.sku == 'b' ? line.copyWith(qty: 5) : line,
          ],
        ),
        sorted: true,
      );

      expect(record(cache, 'Line:b'), {'sku': 'b', 'qty': 5, 'at': nanosB});
      expect(record(cache, 'Line:a'), {'sku': 'a', 'qty': 5, 'at': nanosA});
      expect(version(cache, 'Line:a'), 1);
    });
  });
}

/// Matches an [EntityRef] to [key].
Matcher refTo(String key) =>
    isA<EntityRef>().having((ref) => ref.key, 'key', key);
