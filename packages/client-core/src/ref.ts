import type { EntityKey, Ref } from './types';

/**
 * References are recognised by object identity, not by inspecting properties.
 *
 * An in-band marker -- "an object with a `__ref` key is a reference" -- makes
 * the runtime lossy for any response that legitimately contains that shape:
 * it would be lifted on the way in and resolved to nothing on the way out,
 * and the round-trip property would hold for every tree except the one that
 * mattered. A WeakSet costs the same and cannot collide, because only
 * `makeRef` puts anything in it.
 */
const refs = new WeakSet<object>();

/**
 * Containers that `normalize` had to rebuild because a reference appears
 * somewhere beneath them.
 *
 * A skeleton node absent from this set contains no references at all, so
 * rehydrating it is the identity function -- no walk, no copy, no memo entry,
 * and referential stability for free.
 */
const rewritten = new WeakSet<object>();

export function makeRef(key: EntityKey): Ref {
  const ref: Ref = Object.freeze({ __ref: key });
  refs.add(ref);

  return ref;
}

export function isRef(value: unknown): value is Ref {
  return typeof value === 'object' && value !== null && refs.has(value);
}

export function markRewritten<T extends object>(node: T): T {
  rewritten.add(node);

  return node;
}

export function isRewritten(node: object): boolean {
  return rewritten.has(node);
}

/**
 * `Order` + `7` becomes `Order:7`.
 *
 * The id is stringified, so numeric `7` and string `'7'` are one key. That is
 * deliberate. A Go type's identity field has one type, fixed at generation
 * time -- string, integer, or `encoding.TextMarshaler` -- so one typename
 * cannot legitimately produce both, and a server that returned `7` from one
 * endpoint and `"7"` from another for the same record is describing the same
 * record. Keying them apart would split one entity into two cache entries
 * that never converge, which is the failure this store exists to remove.
 */
export function entityKey(type: string, id: string | number | bigint): EntityKey {
  return `${type}:${String(id)}`;
}

/**
 * Whether a value can identify a record.
 *
 * The Go side already restricted identity fields to strings, integers and
 * `encoding.TextMarshaler` (which serialises as a string), so anything else
 * reaching here means the server sent a shape the manifest did not describe.
 * The response is kept and left inline rather than keyed under `Order:null`,
 * which would collide every record whose id failed to serialise.
 */
export function isIdentity(value: unknown): value is string | number | bigint {
  if (typeof value === 'string') return value.length > 0;
  if (typeof value === 'number') return Number.isFinite(value);

  return typeof value === 'bigint';
}

/**
 * Value equality that sees through references.
 *
 * Two normalisation passes over the same response produce two distinct `Ref`
 * objects for `Order:7`. Comparing them with `Object.is` would report a
 * change on every refetch of unchanged data, bump the version, and churn
 * every object identity downstream of it.
 */
export function sameValue(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;

  return isRef(a) && isRef(b) && a.__ref === b.__ref;
}
