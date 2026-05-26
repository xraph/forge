/**
 * Predicate evaluator.
 *
 * Mirrors contract.Predicate from the Go side. Supports `all`, `any`, and
 * `not` checks. Each check is a simple expression of the form:
 *
 *   form.fieldName==value      // strict equality after toString
 *   form.fieldName!=value
 *   form.fieldName              // truthy
 *   !form.fieldName             // falsy
 *   role:admin                  // session role check
 *
 * Unknown forms evaluate to false and are skipped — this evaluator is for
 * UI conditional visibility/disabled, not for security gating (the Go side
 * enforces real predicates against Warden).
 */

import type { Principal } from "@/contract/types";

export interface Predicate {
  all?: string[];
  any?: string[];
  not?: string[];
  warden?: string;
}

export interface PredicateContext {
  form?: Record<string, unknown>;
  parent?: unknown;
  route?: Record<string, string>;
  session?: { user?: Principal | null };
}

export function evaluatePredicate(p: Predicate | undefined, ctx: PredicateContext): boolean {
  if (!p) return true;
  if (p.all && p.all.length > 0) {
    for (const c of p.all) if (!evaluateCheck(c, ctx)) return false;
  }
  if (p.any && p.any.length > 0) {
    let ok = false;
    for (const c of p.any) if (evaluateCheck(c, ctx)) { ok = true; break; }
    if (!ok) return false;
  }
  if (p.not && p.not.length > 0) {
    for (const c of p.not) if (evaluateCheck(c, ctx)) return false;
  }
  // Warden checks are server-side only; treat as pass for UI predicates.
  return true;
}

function evaluateCheck(raw: string, ctx: PredicateContext): boolean {
  const expr = raw.trim();
  if (!expr) return false;
  const negated = expr.startsWith("!");
  const body = negated ? expr.slice(1) : expr;

  // form.fieldName==value / !=value
  if (body.startsWith("form.")) {
    const eqIdx = body.indexOf("==");
    const neqIdx = body.indexOf("!=");
    if (eqIdx > 0) {
      const lhs = readPath(body.slice(0, eqIdx), ctx);
      const rhs = body.slice(eqIdx + 2);
      return negated !== (asString(lhs) === rhs);
    }
    if (neqIdx > 0) {
      const lhs = readPath(body.slice(0, neqIdx), ctx);
      const rhs = body.slice(neqIdx + 2);
      return negated !== (asString(lhs) !== rhs);
    }
    const v = readPath(body, ctx);
    return negated !== Boolean(v);
  }

  // route.name==value
  if (body.startsWith("route.")) {
    const eqIdx = body.indexOf("==");
    if (eqIdx > 0) {
      const lhs = readPath(body.slice(0, eqIdx), ctx);
      const rhs = body.slice(eqIdx + 2);
      return negated !== (asString(lhs) === rhs);
    }
  }

  // role:admin — session role check
  if (body.startsWith("role:")) {
    const want = body.slice("role:".length);
    const roles = (ctx.session?.user as { roles?: string[] } | null | undefined)?.roles ?? [];
    const has = roles.includes(want);
    return negated !== has;
  }

  // Bare truthiness against the form by name (form.field shorthand without prefix).
  const v = readPath(`form.${body}`, ctx);
  return negated !== Boolean(v);
}

function readPath(path: string, ctx: PredicateContext): unknown {
  const segs = path.split(".");
  const root = segs.shift();
  let cursor: unknown;
  switch (root) {
    case "form":
      cursor = ctx.form;
      break;
    case "route":
      cursor = ctx.route;
      break;
    case "session":
      cursor = ctx.session;
      break;
    case "parent":
      cursor = ctx.parent;
      break;
    default:
      return undefined;
  }
  for (const s of segs) {
    if (cursor == null || typeof cursor !== "object") return undefined;
    cursor = (cursor as Record<string, unknown>)[s];
  }
  return cursor;
}

function asString(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "boolean") return v ? "true" : "false";
  return String(v);
}
