import 'package:forge_client/src/devtools/tag.dart';
import 'package:test/test.dart';

void main() {
  group('parsing a tag', () {
    test('recognises collections, instances and bare names', () {
      final list = parseTag('Order[]');
      expect((list.type, list.collection, list.scope), ('Order', true, ''));

      final one = parseTag('Order:7');
      expect((one.type, one.id, one.collection), ('Order', '7', false));

      final scoped = parseTag('Order[]:archived');
      expect(
        (scoped.type, scoped.collection, scoped.scope),
        ('Order', true, ':archived'),
      );

      // Not everything is structured, and an application is free to invalidate
      // whatever it likes. Reported as a typename rather than refused.
      final bare = parseTag('everything');
      expect(
        (bare.type, bare.id, bare.collection),
        ('everything', null, false),
      );
    });

    test('keeps a composite id whole rather than splitting on the first colon twice', () {
      final tag = parseTag('Order:tenant:7');
      expect((tag.type, tag.id), ('Order', 'tenant:7'));
    });
  });

  group('naming the near miss', () {
    test('reports each relation, most suspicious first', () {
      final found = nearMisses(
        ['Order:7', 'order[]', 'Customer[]', 'Invoice[]'],
        ['Order[]', 'Order[]:archived', 'Customer:3', 'Invoice[]'],
      );
      final pairs = [
        for (final miss in found)
          '${miss.invalidated}|${miss.carried}|${miss.relation.wire}',
      ];

      expect(pairs.first, 'Order:7|Order[]|instance-vs-collection');
      expect(pairs, contains('order[]|Order[]|case'));
      expect(pairs, contains('Customer[]|Customer:3|collection-vs-instance'));
      expect(
        pairs,
        contains('Order:7|Order[]:archived|instance-vs-collection'),
      );

      // An exact match is not a near miss: those tags met.
      expect(
        pairs.any((pair) => pair.startsWith('Invoice[]|Invoice[]')),
        isFalse,
      );
    });

    test('spots a wrong id, which is a wrong placeholder in a template', () {
      final miss = nearMisses(['Order:7'], ['Order:8']).first;

      expect(miss.relation, NearMissRelation.differentInstance);
      expect(miss.hint, contains('different ones'));
    });

    test('spots a scope that only one side carries', () {
      expect(
        nearMisses(['Order[]'], ['Order[]:archived']).first.relation,
        NearMissRelation.scoped,
      );
    });

    test('says nothing about tags that are simply unrelated', () {
      expect(nearMisses(['Shipment:1'], ['Order[]', 'Customer:3']), isEmpty);
    });

    test('caps its output, so a huge dependency set does not produce a wall of text', () {
      final carried = [for (var i = 0; i < 400; i++) 'Order:$i'];

      expect(nearMisses(['Order:9000'], carried), hasLength(8));
      expect(nearMisses(['Order:9000'], carried, 3), hasLength(3));
    });
  });
}
