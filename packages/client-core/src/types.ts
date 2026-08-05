/**
 * The vocabulary the entity store is written against.
 *
 * Every name here is resolved in Go at generation time and shipped in the
 * generated `ops.ts`. Nothing in this file is inferred from the shape of a
 * response: identity was decided against real Go types, and re-deriving it
 * from JSON is the guess that keys two tenants' records to one entry.
 */

/** `Type:id`, e.g. `Order:7`. The store's primary key. */
export type EntityKey = string;

/**
 * What the runtime knows about one named type.
 *
 * `idField` is the JSON property that identifies a record of this type.
 *
 * It is OPTIONAL, and its absence is a positive statement rather than missing
 * data: this type is a signpost, not a record. A paginated envelope
 * (`{items: [...], total: n}`) and an intermediate hop on the way to an entity
 * (`Order -> Shipment -> Carrier`, where `Shipment` is not itself an entity)
 * both need an entry here so the walk can route typenames through them, and
 * neither has an identity to be keyed by. `normalize` walks such a type for its
 * `fields` and never writes a record for it.
 *
 * The alternative -- an `idField` naming a property no payload carries -- is
 * what this file used to require, and it works only by accident: it depends on
 * every future payload continuing not to carry that name. Saying "no identity"
 * outright is the same claim without the accident.
 *
 * `fields` maps a JSON property of this type to the *typename* of what that
 * property contains -- the element typename for arrays. It is how a nested
 * entity of a different type is recognised, because a JSON response carries
 * no typename of its own and this runtime refuses to invent one.
 *
 * The generator resolves these edges in Go against the real component schemas
 * and emits them, but only where the target has an entry in this table -- an
 * edge whose target has none is one the walk below could not follow anyway.
 * Since a non-entity type can now hold an entry, that includes every named type
 * from which an entity is REACHABLE, not only the entities themselves. A named
 * type with no entity anywhere beneath it -- an enum, a plain value struct --
 * still has no entry, and its subtree stays inline. That is under-normalisation,
 * which costs a refetch. Guessing would cost a data leak.
 */
export interface EntityMeta {
  readonly idField?: string;
  readonly fields?: Readonly<Record<string, string>>;
}

/**
 * The typename-to-metadata table, exactly the shape of the generated
 * `export const entities` in `ops.ts`.
 */
export type EntitySchema = Readonly<Record<string, EntityMeta>>;

/**
 * A placeholder standing where an entity was lifted out of the tree.
 *
 * The `__ref` property exists so a skeleton survives JSON serialisation for
 * SSR. It is deliberately *not* how the runtime recognises a reference --
 * see `isRef` -- so a response that happens to contain an object shaped like
 * one round-trips untouched.
 */
export interface Ref {
  readonly __ref: EntityKey;
}

/** One row of the store: the entity's own fields, plus a write counter. */
export interface EntityRecord {
  readonly data: Readonly<Record<string, unknown>>;
  readonly version: number;
}

/** What `normalize` produces. Pure: the input is not touched. */
export interface NormalizeResult {
  /** The input tree with every recognised entity replaced by a `Ref`. */
  readonly skeleton: unknown;
  /** Every entity lifted out, keyed by `EntityKey`. */
  readonly records: ReadonlyMap<EntityKey, Record<string, unknown>>;
  /**
   * Every entity key this pass touched, transitively -- the dependency set a
   * query holding this skeleton must be recomputed against.
   */
  readonly deps: ReadonlySet<EntityKey>;
}
