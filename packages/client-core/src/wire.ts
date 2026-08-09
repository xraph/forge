import { isRef, makeRef, markRewritten } from './ref';
import type { EntityKey, Ref } from './types';

/**
 * The JSON encoding a dehydrated payload uses, and its inverse.
 *
 * `__ref` is the wire form of a reference and always has been -- see `Ref` --
 * but it cannot be the *recognition* rule, because a response may legitimately
 * contain an object of exactly that shape. `normalize` leaves such an object
 * inline, so it reaches the store as ordinary record data; a revive pass that
 * treated every `{__ref: string}` as a reference would mint one from it and
 * `denormalize` would resolve it to `undefined`. That is precisely the lossy
 * round-trip `ref.ts` refuses, reintroduced from the other direction.
 *
 * So the encoder escapes on the way out and the reviver unescapes on the way
 * in. Both directions are one walk, and the encoder's walk is needed anyway --
 * `dehydrate` has to collect the references it finds to build its reachability
 * closure, and has to notice a cycle, and both fall out of visiting every node.
 */

/** A key that would be read back as the marker, or as an escape of one. */
const COLLIDES = /^_*__ref$/;

/** A key this encoder escaped on the way out. */
const ESCAPED = /^_+__ref$/;

export interface EncodeContext {
  /** The query whose payload this is, for the cycle error. */
  readonly query: string;
  /** The record being encoded, when this is a record rather than a skeleton. */
  readonly entity?: string;
}

export interface EncodeResult {
  readonly value: unknown;
  /**
   * Every reference found, in encounter order and **not** deduplicated.
   *
   * The caller is walking a closure and already holds the set of keys it has
   * seen, so deduplicating here would build a second Set per node to answer a
   * question the caller has to answer again anyway.
   */
  readonly refs: readonly EntityKey[];
}

/** Copy `node` into a JSON-safe form, escaping keys and lifting references. */
export function encode(node: unknown, context: EncodeContext): EncodeResult {
  const refs: EntityKey[] = [];
  const route = new Set<object>();

  function walk(value: unknown, path: string): unknown {
    if (value === null || typeof value !== 'object') return value;

    if (isRef(value)) {
      const key = (value as Ref).__ref;
      refs.push(key);

      // A fresh literal rather than the frozen `Ref` itself. Emitting the
      // reference would put an object registered in `ref.ts`'s WeakSet into the
      // payload -- harmless on the wire, and a trap for anything that later
      // asked whether the payload contained one.
      return { __ref: key };
    }

    // A route rather than a running set of everything seen, for the reason
    // `store.ts`'s `equal` gives: an object reached twice through different
    // fields is a DAG, not a cycle, and refusing it would reject a response the
    // server can legitimately send.
    if (route.has(value)) throw cyclic(context, path);

    route.add(value);

    const out = Array.isArray(value)
      ? value.map((element, index) => walk(element, `${path}[${index}]`))
      : walkObject(value as Record<string, unknown>, path);

    route.delete(value);

    return out;
  }

  function walkObject(value: Record<string, unknown>, path: string): Record<string, unknown> {
    const out: Record<string, unknown> = {};

    for (const key of Object.keys(value)) {
      out[COLLIDES.test(key) ? `_${key}` : key] = walk(value[key], `${path}.${key}`);
    }

    return out;
  }

  return { value: walk(node, rootPath(context)), refs };
}

/**
 * Throw if `node` is cyclic, without copying it.
 *
 * The denormalized payload mode ships a rehydrated value straight to
 * `JSON.stringify`, so it needs the cycle check and none of the escaping: it
 * contains no genuine references, and escaping a value nothing will unescape
 * would corrupt it.
 */
export function assertAcyclic(node: unknown, context: EncodeContext): void {
  const route = new Set<object>();

  function walk(value: unknown, path: string): void {
    if (value === null || typeof value !== 'object') return;

    if (route.has(value)) throw cyclic(context, path);

    route.add(value);

    if (Array.isArray(value)) {
      value.forEach((element, index) => walk(element, `${path}[${index}]`));
    } else {
      for (const key of Object.keys(value)) {
        walk((value as Record<string, unknown>)[key], `${path}.${key}`);
      }
    }

    route.delete(value);
  }

  walk(node, rootPath(context));
}

/**
 * Turn a decoded payload back into a skeleton the runtime recognises.
 *
 * `markRewritten` is applied to a container **only** where a reference occurs
 * beneath it. A container without one is deliberately left unmarked and, where
 * nothing about it changed, returned by identity -- which is what keeps
 * `EntityStore`'s "not rewritten means no walk" fast path intact for a hydrated
 * skeleton exactly as it is for a normalized one. Marking everything would be
 * correct and would void structural sharing for the whole response.
 */
export function revive(node: unknown): unknown {
  return reviveNode(node).value;
}

interface Revived {
  readonly value: unknown;
  /** Whether a reference was minted here or anywhere beneath. */
  readonly refs: boolean;
}

function reviveNode(node: unknown): Revived {
  if (node === null || typeof node !== 'object') return { value: node, refs: false };

  if (isMarker(node)) {
    return { value: makeRef((node as Record<string, unknown>).__ref as EntityKey), refs: true };
  }

  return Array.isArray(node) ? reviveArray(node) : reviveObject(node as Record<string, unknown>);
}

function reviveArray(node: unknown[]): Revived {
  const out = new Array<unknown>(node.length);
  let refs = false;
  let changed = false;

  for (let i = 0; i < node.length; i++) {
    const child = reviveNode(node[i]);

    out[i] = child.value;
    if (child.refs) refs = true;
    if (child.value !== node[i]) changed = true;
  }

  // Nothing moved, so nothing was minted either: a reference always differs
  // from the marker object it replaced.
  if (!changed) return { value: node, refs: false };

  return { value: refs ? markRewritten(out) : out, refs };
}

function reviveObject(node: Record<string, unknown>): Revived {
  const out: Record<string, unknown> = {};
  let refs = false;
  let changed = false;

  for (const key of Object.keys(node)) {
    const name = ESCAPED.test(key) ? key.slice(1) : key;
    const child = reviveNode(node[key]);

    out[name] = child.value;
    if (child.refs) refs = true;
    if (name !== key || child.value !== node[key]) changed = true;
  }

  if (!changed) return { value: node, refs: false };

  return { value: refs ? markRewritten(out) : out, refs };
}

/**
 * Whether a decoded object is the marker `encode` emits.
 *
 * Exactly one own key, named `__ref`, holding a string -- the shape `makeRef`
 * produces and the only shape the encoder ever emits unescaped. Anything looser
 * would claim objects the encoder escaped for a reason.
 */
function isMarker(node: object): boolean {
  if (Array.isArray(node)) return false;

  const keys = Object.keys(node);

  return (
    keys.length === 1 &&
    keys[0] === '__ref' &&
    typeof (node as Record<string, unknown>).__ref === 'string'
  );
}

function rootPath(context: EncodeContext): string {
  return context.entity === undefined ? 'skeleton' : 'data';
}

function cyclic(context: EncodeContext, path: string): Error {
  const headline =
    context.entity === undefined
      ? 'cannot serialize a cyclic value'
      : 'cannot serialize a cycle within one record';
  const entity = context.entity === undefined ? '' : `\n  entity  ${context.entity}`;

  return new Error(
    `[forge] dehydrate: ${headline}\n  query   ${context.query}${entity}\n  path    ${path}`,
  );
}
