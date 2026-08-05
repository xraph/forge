/**
 * Tag shapes, and how two tags that ought to have met fail to.
 *
 * This file exists for one question: *why did this query not refetch*. The
 * answer is almost always that a mutation raised one tag and a query carries a
 * different one, and that the two are related in a way a person would call
 * obvious and a set intersection calls disjoint. `Order:7` and `Order[]` are
 * the canonical pair. Naming that relation is the difference between a devtools
 * panel that shows you two lists and one that tells you what is wrong.
 */

/** A tag pulled apart. Everything is optional because a tag is just a string. */
export interface ParsedTag {
  readonly raw: string;
  /** The typename, e.g. `Order`. The whole tag when it has no structure. */
  readonly type: string;
  /** The instance identity, when the tag names one: `Order:7` -> `7`. */
  readonly id: string | undefined;
  /** `Order[]` and `Order[]:archived` are collections; `Order:7` is not. */
  readonly collection: boolean;
  /** Whatever followed the `[]`, e.g. `Order[]:archived` -> `:archived`. */
  readonly scope: string;
}

/**
 * Split a tag into typename, identity and collection-ness.
 *
 * The grammar is the one the rest of the runtime already uses: `Type[]` for a
 * collection, `Type:id` for an instance, anything else for itself. Nothing here
 * validates -- an application is free to invalidate `everything`, and this
 * reports it as a typename with no identity rather than refusing to parse it.
 */
export function parseTag(tag: string): ParsedTag {
  const bracket = tag.indexOf('[]');

  if (bracket >= 0) {
    return {
      raw: tag,
      type: tag.slice(0, bracket),
      id: undefined,
      collection: true,
      scope: tag.slice(bracket + 2),
    };
  }

  const colon = tag.indexOf(':');

  if (colon > 0) {
    return {
      raw: tag,
      type: tag.slice(0, colon),
      id: tag.slice(colon + 1),
      collection: false,
      scope: '',
    };
  }

  return { raw: tag, type: tag, id: undefined, collection: false, scope: '' };
}

/**
 * How an invalidated tag and a carried tag are related without matching.
 *
 * Each of these is a distinct mistake with a distinct fix, which is why they
 * are separate values rather than one "close" flag.
 */
export type NearMissRelation =
  /** Invalidated `Order:7`, carried `Order[]`. The create-not-appearing defect. */
  | 'instance-vs-collection'
  /** Invalidated `Order[]`, carried `Order:7`. A list write and a detail view. */
  | 'collection-vs-instance'
  /** Invalidated `Order:7`, carried `Order:8`. Usually a wrong id in a template. */
  | 'different-instance'
  /** `Order[]` against `order[]`. A typo the type system cannot see. */
  | 'case'
  /** One tag is the other plus a suffix: `Order[]` against `Order[]:archived`. */
  | 'scoped';

/** One place the two tag sets nearly meet, and what to do about it. */
export interface NearMiss {
  /** The tag the cause raised. */
  readonly invalidated: string;
  /** The tag the query carries. */
  readonly carried: string;
  readonly relation: NearMissRelation;
  /** One sentence naming the fix. */
  readonly hint: string;
}

/** Lower is closer. Only used to order the report. */
const RANK: Record<NearMissRelation, number> = {
  'instance-vs-collection': 0,
  case: 1,
  'collection-vs-instance': 2,
  scoped: 3,
  'different-instance': 4,
};

/**
 * Every near miss between what a cause raised and what a query carries.
 *
 * Sorted most-suspicious first and capped, because a mutation that raises three
 * tags against a list carrying four hundred entity dependencies would otherwise
 * produce twelve hundred rows of "these are both Orders".
 */
export function nearMisses(
  invalidated: readonly string[],
  carried: readonly string[],
  limit = 8,
): NearMiss[] {
  const found: NearMiss[] = [];
  const seen = new Set<string>();

  for (const left of invalidated) {
    const a = parseTag(left);

    for (const right of carried) {
      if (left === right) continue;

      const relation = relate(a, parseTag(right));

      if (relation === undefined) continue;

      const pair = `${left}${right}`;

      if (seen.has(pair)) continue;

      seen.add(pair);
      found.push({ invalidated: left, carried: right, relation, hint: hint(relation, left, right) });
    }
  }

  found.sort((x, y) => RANK[x.relation] - RANK[y.relation]);

  return found.slice(0, limit);
}

function relate(a: ParsedTag, b: ParsedTag): NearMissRelation | undefined {
  if (a.type === b.type && a.type !== '') {
    if (!a.collection && b.collection) return 'instance-vs-collection';
    if (a.collection && !b.collection) return 'collection-vs-instance';
    if (!a.collection && !b.collection && a.id !== b.id) return 'different-instance';
    if (a.collection && b.collection && a.scope !== b.scope) return 'scoped';

    return undefined;
  }

  if (a.type.toLowerCase() === b.type.toLowerCase()) return 'case';

  if (a.raw.startsWith(b.raw) || b.raw.startsWith(a.raw)) return 'scoped';

  return undefined;
}

/**
 * What to do about one near miss.
 *
 * Written as instructions rather than observations. "These tags differ" is
 * something the developer can already see; which of the two declarations to
 * change is the thing they came here for.
 */
function hint(relation: NearMissRelation, invalidated: string, carried: string): string {
  const parsed = parseTag(invalidated);

  switch (relation) {
    case 'instance-vs-collection':
      return (
        `the mutation invalidated the instance \`${invalidated}\` but this query provides the ` +
        `collection \`${carried}\`, and the two never intersect. A query only carries ` +
        `\`${invalidated}\` once a response has actually put that entity in its result -- which a ` +
        `create never has. Add \`${parsed.type}[]\` to the operation's Invalidates.`
      );
    case 'collection-vs-instance':
      return (
        `the mutation invalidated the collection \`${invalidated}\` but this query provides the ` +
        `instance \`${carried}\`. Detail views are not reached by list invalidations: add ` +
        `\`${carried}\` (or \`${parsed.type}:{id}\`) to the operation's Invalidates.`
      );
    case 'different-instance':
      return (
        `both name a \`${parsed.type}\`, but different ones. Check the placeholder the ` +
        `operation's Invalidates template resolved against -- an id read from the request when ` +
        `it should have come from the response is the usual cause.`
      );
    case 'case':
      return (
        `\`${invalidated}\` and \`${carried}\` differ only in case. Tags are compared as exact ` +
        `strings, so this never matches. One of the two declarations has the typename wrong.`
      );
    case 'scoped':
      return (
        `\`${invalidated}\` and \`${carried}\` share a prefix but are not equal, so they do not ` +
        `intersect. A scoped tag has to be invalidated by the same scope the query provides.`
      );
  }
}
