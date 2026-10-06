/// Tag shapes, and how two tags that ought to have met fail to. Port of
/// `client-devtools/src/tag.ts`.
library;

import '../types.dart';

/// A tag pulled apart. A tag is just a string, so nothing here validates.
final class ParsedTag {
  /// Creates a parsed tag.
  const ParsedTag({
    required this.raw,
    required this.type,
    required this.id,
    required this.collection,
    required this.scope,
  });

  /// The tag as written.
  final String raw;

  /// The typename, or the whole tag when it has no structure.
  final String type;

  /// The instance identity: `Order:7` gives `7`.
  final String? id;

  /// `Order[]` and `Order[]:archived` are collections.
  final bool collection;

  /// Whatever followed the `[]`.
  final String scope;
}

/// Splits a tag into typename, identity and collection-ness.
ParsedTag parseTag(String tag) {
  final bracket = tag.indexOf('[]');

  if (bracket >= 0) {
    return ParsedTag(
      raw: tag,
      type: tag.substring(0, bracket),
      id: null,
      collection: true,
      scope: tag.substring(bracket + 2),
    );
  }

  final colon = tag.indexOf(':');

  if (colon > 0) {
    return ParsedTag(
      raw: tag,
      type: tag.substring(0, colon),
      id: tag.substring(colon + 1),
      collection: false,
      scope: '',
    );
  }

  return ParsedTag(raw: tag, type: tag, id: null, collection: false, scope: '');
}

/// How an invalidated tag and a carried tag are related without matching.
enum NearMissRelation {
  /// Invalidated `Order:7`, carried `Order[]`. The create-not-appearing defect.
  instanceVsCollection('instance-vs-collection', 0),

  /// `Order[]` against `order[]`.
  letterCase('case', 1),

  /// Invalidated `Order[]`, carried `Order:7`.
  collectionVsInstance('collection-vs-instance', 2),

  /// One tag is the other plus a suffix.
  scoped('scoped', 3),

  /// Invalidated `Order:7`, carried `Order:8`.
  differentInstance('different-instance', 4);

  const NearMissRelation(this.wire, this.rank);

  /// The TS name, used on the wire.
  final String wire;

  /// Lower is more suspicious. Orders the report.
  final int rank;
}

/// One place the two tag sets nearly meet, and what to do about it.
final class NearMiss {
  /// Creates a near miss.
  const NearMiss({
    required this.invalidated,
    required this.carried,
    required this.relation,
    required this.hint,
  });

  /// The tag the cause raised.
  final String invalidated;

  /// The tag the query carries.
  final String carried;

  /// How they are related.
  final NearMissRelation relation;

  /// One sentence naming the fix.
  final String hint;

  /// The JSON form.
  Json toJson() => {
    'invalidated': invalidated,
    'carried': carried,
    'relation': relation.wire,
    'hint': hint,
  };
}

/// Every near miss between what a cause raised and what a query carries,
/// most suspicious first, capped at [limit].
List<NearMiss> nearMisses(
  List<String> invalidated,
  List<String> carried, [
  int limit = 8,
]) {
  final found = <(int, NearMiss)>[];
  final seen = <String>{};

  for (final left in invalidated) {
    final a = parseTag(left);

    for (final right in carried) {
      if (left == right) continue;

      final relation = _relate(a, parseTag(right));

      if (relation == null) continue;
      if (!seen.add('$left\u0000$right')) continue;

      found.add((
        found.length,
        NearMiss(
          invalidated: left,
          carried: right,
          relation: relation,
          hint: _hint(relation, left, right),
        ),
      ));
    }
  }

  found.sort((x, y) {
    final byRank = x.$2.relation.rank.compareTo(y.$2.relation.rank);
    return byRank != 0 ? byRank : x.$1.compareTo(y.$1);
  });

  return [for (final (_, miss) in found.take(limit)) miss];
}

NearMissRelation? _relate(ParsedTag a, ParsedTag b) {
  if (a.type == b.type && a.type.isNotEmpty) {
    if (!a.collection && b.collection) {
      return NearMissRelation.instanceVsCollection;
    }
    if (a.collection && !b.collection) {
      return NearMissRelation.collectionVsInstance;
    }
    if (!a.collection && !b.collection && a.id != b.id) {
      return NearMissRelation.differentInstance;
    }
    if (a.collection && b.collection && a.scope != b.scope) {
      return NearMissRelation.scoped;
    }
    return null;
  }

  if (a.type.toLowerCase() == b.type.toLowerCase()) {
    return NearMissRelation.letterCase;
  }
  if (a.raw.startsWith(b.raw) || b.raw.startsWith(a.raw)) {
    return NearMissRelation.scoped;
  }

  return null;
}

String _hint(NearMissRelation relation, String invalidated, String carried) {
  final type = parseTag(invalidated).type;

  return switch (relation) {
    NearMissRelation.instanceVsCollection =>
      'the mutation invalidated the instance `$invalidated` but this query provides the '
          'collection `$carried`, and the two never intersect. A query only carries '
          '`$invalidated` once a response has actually put that entity in its result, which a '
          "create never has. Add `$type[]` to the operation's Invalidates.",
    NearMissRelation.collectionVsInstance =>
      'the mutation invalidated the collection `$invalidated` but this query provides the '
          'instance `$carried`. Detail views are not reached by list invalidations: add '
          "`$carried` (or `$type:{id}`) to the operation's Invalidates.",
    NearMissRelation.differentInstance =>
      'both name a `$type`, but different ones. Check the placeholder the '
          "operation's Invalidates template resolved against: an id read from the request when "
          'it should have come from the response is the usual cause.',
    NearMissRelation.letterCase =>
      '`$invalidated` and `$carried` differ only in case. Tags are compared as exact '
          'strings, so this never matches. One of the two declarations has the typename wrong.',
    NearMissRelation.scoped =>
      '`$invalidated` and `$carried` share a prefix but are not equal, so they do not '
          'intersect. A scoped tag has to be invalidated by the same scope the query provides.',
  };
}
