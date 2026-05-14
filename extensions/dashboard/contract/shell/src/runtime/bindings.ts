import type { Principal } from "@/contract/types";

export interface BindingContext {
  parent?: Record<string, unknown> | null;
  session?: { user?: Principal | null } | null;
  route?: Record<string, string>;
}

/**
 * resolvePayload turns a manifest-style payload map (whose values may be
 * literal primitives, { value } objects, or { from: "scope.path" } references)
 * into a flat record of resolved JS values. Unresolvable references become
 * undefined and are dropped.
 */
export function resolvePayload(
  payload: Record<string, unknown> | undefined,
  ctx: BindingContext,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (!payload) return out;
  for (const [k, raw] of Object.entries(payload)) {
    const v = resolveValue(raw, ctx);
    if (v !== undefined) out[k] = v;
  }
  return out;
}

/** resolveValue resolves a single ParamSource-like value. */
export function resolveValue(raw: unknown, ctx: BindingContext): unknown {
  if (raw === null) return null;
  if (typeof raw !== "object") return raw;
  // object: either { value } or { from } or a generic record (treated as literal)
  const obj = raw as Record<string, unknown>;
  if ("from" in obj && typeof obj.from === "string") {
    return resolvePath(obj.from, ctx);
  }
  if ("value" in obj) {
    return obj.value;
  }
  return raw; // generic record passes through as a literal
}

function resolvePath(path: string, ctx: BindingContext): unknown {
  // path is like "parent.id" or "session.user.tenantID" or "route.tenant"
  const segments = path.split(".");
  if (segments.length === 0) return undefined;
  const root = segments[0];
  const rest = segments.slice(1);

  let cursor: unknown;
  switch (root) {
    case "parent":
      cursor = ctx.parent ?? undefined;
      break;
    case "session":
      cursor = ctx.session ?? undefined;
      break;
    case "route":
      cursor = ctx.route ?? undefined;
      break;
    default:
      return undefined;
  }
  for (const seg of rest) {
    if (cursor == null || typeof cursor !== "object") return undefined;
    cursor = (cursor as Record<string, unknown>)[seg];
  }
  return cursor;
}
