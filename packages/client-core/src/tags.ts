import { isIdentity } from './ref.js';

/**
 * The four places a tag template can name a value.
 *
 * `path` and `query` are the request's parameters already decoded into plain
 * records; `body` is the request body; `response` is what the operation
 * returned. They arrive from the generated facade, which knows which argument
 * went where -- the runtime never re-parses a URL to find out.
 */
export interface TagContext {
  readonly path?: Readonly<Record<string, unknown>>;
  readonly query?: Readonly<Record<string, unknown>>;
  readonly body?: unknown;
  readonly response?: unknown;
}

/** What `resolveTags` produces: the tags that resolved, and the templates that did not. */
export interface ResolvedTags {
  readonly tags: string[];
  readonly unresolved: string[];
}

const PLACEHOLDER = /\{([^{}]*)\}/g;

/**
 * Substitute a tag template against one operation's arguments and response.
 *
 * `Order[]` has no placeholder and is returned unchanged. `Order:{id}`,
 * `Customer:{req.customerId}` and `Shipment:{res.shipment.id}` substitute.
 *
 * Returns `undefined` -- never a partially substituted string -- when any
 * placeholder in the template names nothing usable. A tag that silently
 * becomes `Customer:` is an invalidation that fires against a key no query
 * ever provides: it never matches, never refetches, and never reports. The
 * whole point of returning `undefined` here is that the caller is forced to
 * decide what to do about it. See `Invalidator` for what that decision is.
 */
export function resolveTag(template: string, ctx: TagContext): string | undefined {
  if (!template.includes('{')) return template;

  let resolved = true;

  const tag = template.replace(PLACEHOLDER, (_match, expr: string) => {
    const value = lookup(expr.trim(), ctx);

    if (!usable(value)) {
      resolved = false;

      return '';
    }

    return String(value);
  });

  return resolved ? tag : undefined;
}

/**
 * Resolve a whole `provides` or `invalidates` list, deduplicated.
 *
 * Unresolvable templates are reported rather than thrown, so one bad
 * cross-entity declaration cannot cost the caller the other nine tags in the
 * same list.
 */
export function resolveTags(templates: readonly string[], ctx: TagContext): ResolvedTags {
  const tags: string[] = [];
  const unresolved: string[] = [];
  const seen = new Set<string>();

  const push = (tag: string): void => {
    if (!seen.has(tag)) {
      seen.add(tag);
      tags.push(tag);
    }
  };

  for (const template of templates) {
    const tag = resolveTag(template, ctx);

    if (tag !== undefined) {
      push(tag);
      continue;
    }

    // A collection answers a response template once per element. `GET /orders`
    // declares `Order:{id}`, and what that promises is one tag per record
    // returned, not a property the array itself carries. Resolving against the
    // array as a whole finds nothing and would report the healthiest query in
    // the application as a declaration that resolved to nothing. An empty
    // array provides nothing, which is not the same as failing to resolve:
    // the template was answered, with zero records. A template whose every
    // placeholder is `req.`-scoped is left out, since no response answers it.
    const response = ctx.response;

    if (Array.isArray(response) && /\{(?!\s*req\.)/.test(template)) {
      let answered = response.length === 0;

      for (const item of response) {
        const each = resolveTag(template, { ...ctx, response: item });

        if (each !== undefined) {
          answered = true;
          push(each);
        }
      }

      if (answered) continue;
    }

    unresolved.push(template);
  }

  return { tags, unresolved };
}

/**
 * Resolve one placeholder expression.
 *
 * Explicit-first, exactly as the design specifies. `{req.x}` searches the
 * request only -- path, then query, then body. `{res.a.b}` searches the
 * response only. A bare `{x}` searches path, query, body, then response, first
 * match wins, which is what makes `Customer:{customerId}` work whether the id
 * arrived as a path segment or as a body field.
 *
 * "First match wins" means the first source that has the property *at all*.
 * A source holding `null` is a match that then fails the usability check, and
 * that is deliberate: a body that explicitly said `customerId: null` has
 * answered the question, and falling through to the response would invalidate
 * some other customer's list on the strength of a value nobody asked for.
 */
function lookup(expr: string, ctx: TagContext): unknown {
  const dot = expr.indexOf('.');

  if (dot > 0) {
    const head = expr.slice(0, dot);
    const rest = expr.slice(dot + 1);

    if (head === 'req') return fromRequest(rest, ctx);
    if (head === 'res') return dig(ctx.response, rest);
  }

  const request = fromRequest(expr, ctx);

  return request !== undefined ? request : dig(ctx.response, expr);
}

function fromRequest(expr: string, ctx: TagContext): unknown {
  const fromPath = dig(ctx.path, expr);

  if (fromPath !== undefined) return fromPath;

  const fromQuery = dig(ctx.query, expr);

  return fromQuery !== undefined ? fromQuery : dig(ctx.body, expr);
}

/**
 * Walk a dotted path. Numeric segments index arrays, which are objects too.
 *
 * A segment that names no property is retried under its other spellings:
 * `customer_id` finds `customerId`, and the reverse. A template is written
 * against the wire, because that is what the server can see, while the body
 * and the response it is resolved against carry the client-side names. The
 * generator renames templates it can see the schema for; this is the
 * fallback for a document it could not, or one generated before it did. An
 * exact key always wins, so a payload carrying both spellings is read as
 * written.
 */
function dig(root: unknown, path: string): unknown {
  let node: unknown = root;

  for (const segment of path.split('.')) {
    if (node === null || typeof node !== 'object') return undefined;

    const record = node as Record<string, unknown>;

    node = record[segment in record ? segment : (spelledAs(record, segment) ?? segment)];

    if (node === undefined) return undefined;
  }

  return node;
}

/** The key of `record` that is `name` under another casing, if there is one. */
function spelledAs(record: Record<string, unknown>, name: string): string | undefined {
  // `customer_id`, `customerId`, `CustomerID` and `customer-id` all fold alike.
  const fold = (key: string): string => key.replace(/[_-]/g, '').toLowerCase();
  const wanted = fold(name);

  return Object.keys(record).find((key) => fold(key) === wanted);
}

/**
 * Whether a value can stand in a tag.
 *
 * The same rule the entity store applies to an identity field -- a non-empty
 * string, a finite number, a bigint -- widened by booleans, because a tag may
 * legitimately partition a list by a flag (`Order[]:{req.archived}`). Anything
 * else, including `null`, `undefined`, `NaN` and objects, resolves to nothing.
 */
function usable(value: unknown): value is string | number | bigint | boolean {
  return typeof value === 'boolean' || isIdentity(value);
}

/**
 * The cache key for one mounted query: its operation plus its arguments.
 *
 * Object keys are sorted, so `{a: 1, b: 2}` and `{b: 2, a: 1}` are one query
 * rather than two entries that each refetch the other's invalidations.
 * Properties whose value is `undefined` are dropped for the same reason: an
 * optional argument left off and an optional argument passed `undefined` are
 * the same request.
 */
export function queryKey(operation: string, args?: unknown): string {
  return args === undefined ? operation : `${operation}|${stable(args)}`;
}

function stable(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'null';

  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`;

  const source = value as Record<string, unknown>;
  const parts: string[] = [];

  for (const key of Object.keys(source).sort()) {
    if (source[key] === undefined) continue;

    parts.push(`${JSON.stringify(key)}:${stable(source[key])}`);
  }

  return `{${parts.join(',')}}`;
}
