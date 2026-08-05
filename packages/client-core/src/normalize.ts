import { entityKey, isIdentity, makeRef, markRewritten } from './ref';
import type { EntityKey, EntitySchema, NormalizeResult, Ref } from './types';

/**
 * Split a response into a flat entity store and a skeleton of references.
 *
 * `rootType` is the typename of `value` -- or of its elements, when `value`
 * is an array -- and comes from the generated manifest (`ops.orderGet.entity`).
 * It is the only way this runtime learns a typename: JSON carries none, and
 * inferring one from the presence of an `id` property is the guess the Go
 * side deliberately refuses. Descending past the root uses
 * `schema[type].fields`; where that is absent, the subtree is left inline.
 *
 * Pure. `value` is never mutated, and subtrees containing no entity are
 * returned by reference rather than copied.
 */
export function normalize(
  value: unknown,
  schema: EntitySchema,
  rootType?: string,
): NormalizeResult {
  const records = new Map<EntityKey, Record<string, unknown>>();
  const deps = new Set<EntityKey>();

  /**
   * Source node to its skeleton. Registered *before* the node's children are
   * walked, which is what makes a cyclic graph terminate: the second visit
   * returns the placeholder the first visit is still filling.
   */
  const seen = new Map<object, unknown>();

  function walk(node: unknown, type: string | undefined): unknown {
    if (node === null || typeof node !== 'object') return node;

    if (seen.has(node)) return seen.get(node);

    if (Array.isArray(node)) return walkArray(node, type);

    return walkObject(node as Record<string, unknown>, type);
  }

  function walkArray(node: unknown[], type: string | undefined): unknown {
    // An array does not change the typename: `[]Order` is a list of `Order`.
    const out: unknown[] = new Array(node.length);
    seen.set(node, out);

    let changed = false;

    for (let i = 0; i < node.length; i++) {
      out[i] = walk(node[i], type);
      if (out[i] !== node[i]) changed = true;
    }

    // Nothing beneath this array referenced an entity, so the input array is
    // already its own skeleton. Any cycle back to this node would have
    // produced `out` somewhere below and flipped `changed`, so returning the
    // input here cannot strand a placeholder.
    if (!changed) {
      seen.set(node, node);

      return node;
    }

    return markRewritten(out);
  }

  function walkObject(node: Record<string, unknown>, type: string | undefined): unknown {
    const meta = type === undefined ? undefined : schema[type];
    const id = meta === undefined ? undefined : node[meta.idField];
    const key: EntityKey | undefined =
      meta !== undefined && isIdentity(id) ? entityKey(type as string, id) : undefined;

    const out: Record<string, unknown> = {};
    let ref: Ref | undefined;

    if (key === undefined) {
      seen.set(node, out);
    } else {
      ref = makeRef(key);
      seen.set(node, ref);
      deps.add(key);
    }

    let changed = false;

    for (const field of Object.keys(node)) {
      out[field] = walk(node[field], meta?.fields?.[field]);
      if (out[field] !== node[field]) changed = true;
    }

    if (key !== undefined) {
      const prev = records.get(key);

      // The same entity can occur twice in one response carrying different
      // field sets -- a full record in one branch, a summary in another.
      // Merging rather than replacing is the same rule the store applies
      // across responses: a field another view reads must not vanish because
      // this response happened not to include it.
      records.set(key, prev === undefined ? out : { ...prev, ...out });

      return ref as Ref;
    }

    if (!changed) {
      seen.set(node, node);

      return node;
    }

    return markRewritten(out);
  }

  return { skeleton: walk(value, rootType), records, deps };
}
