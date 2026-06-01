import { z, type ZodTypeAny } from "zod";

/**
 * FieldRule mirrors the Go side (components.FieldRule). Kept loose to avoid
 * coupling the type signature too tightly — the contract serializes
 * additional kinds as needed and the unknown branch is a no-op.
 */
export interface FieldRule {
  kind: string;
  value?: unknown;
  message?: string;
  intent?: string;
}

/** Subset of FieldSpec used to build the validation schema. */
export interface FieldSpecLite {
  name: string;
  type: string;
  required?: boolean;
  validate?: FieldRule[];
  multiple?: boolean;
  min?: number;
  max?: number;
  minLength?: number;
  maxLength?: number;
}

/**
 * buildZodSchema returns a zod object that validates the given fields'
 * values. Each field's base type comes from FieldSpec.type; rules in
 * FieldSpec.validate apply additional checks (regex, email, min, max…).
 *
 * Fields without a `required` rule are made optional() so empty values
 * don't fail submit. Fields with `intent`-kind validators are accepted at
 * schema time — async checks happen in the form runtime.
 */
export function buildZodSchema(fields: FieldSpecLite[]): z.ZodObject<Record<string, ZodTypeAny>> {
  const shape: Record<string, ZodTypeAny> = {};
  for (const f of fields) {
    let s: ZodTypeAny = baseSchema(f);
    s = applyRules(s, f);
    if (!isRequired(f)) {
      s = s.optional();
    }
    shape[f.name] = s;
  }
  return z.object(shape);
}

function baseSchema(f: FieldSpecLite): ZodTypeAny {
  switch (f.type) {
    case "number":
    case "slider":
      return z.coerce.number();
    case "checkbox":
    case "switch":
      return z.boolean();
    case "multi-select":
    case "tag-input":
      return z.array(z.string());
    case "file":
      return f.multiple ? z.array(z.any()) : z.any();
    case "date":
      return z.string(); // ISO string (YYYY-MM-DD)
    case "date-range":
      return z.string(); // ISO/ISO
    case "email":
      return z.string().email().or(z.string().length(0));
    case "url":
      return z.string().url().or(z.string().length(0));
    case "hidden":
      return z.any();
    default:
      return z.string();
  }
}

function applyRules(s: ZodTypeAny, f: FieldSpecLite): ZodTypeAny {
  for (const r of f.validate ?? []) {
    s = applyRule(s, r);
  }
  return s;
}

function applyRule(s: ZodTypeAny, r: FieldRule): ZodTypeAny {
  const message = r.message;
  switch (r.kind) {
    case "required":
      // Required is enforced via the `isRequired` check that wraps the
      // schema in optional() when absent. A required string is also
      // .min(1) so empty strings fail.
      if (s instanceof z.ZodString) {
        return s.min(1, message ?? "Required");
      }
      return s;
    case "minLength":
      if (s instanceof z.ZodString && typeof r.value === "number") {
        return s.min(r.value, message ?? `Min length ${r.value}`);
      }
      return s;
    case "maxLength":
      if (s instanceof z.ZodString && typeof r.value === "number") {
        return s.max(r.value, message ?? `Max length ${r.value}`);
      }
      return s;
    case "min":
      if (s instanceof z.ZodNumber && typeof r.value === "number") {
        return s.min(r.value, message ?? `Min ${r.value}`);
      }
      return s;
    case "max":
      if (s instanceof z.ZodNumber && typeof r.value === "number") {
        return s.max(r.value, message ?? `Max ${r.value}`);
      }
      return s;
    case "regex":
      if (s instanceof z.ZodString && typeof r.value === "string") {
        try {
          return s.regex(new RegExp(r.value), message ?? "Invalid format");
        } catch {
          return s;
        }
      }
      return s;
    case "email":
      if (s instanceof z.ZodString) {
        return s.email(message ?? "Invalid email");
      }
      return s;
    case "url":
      if (s instanceof z.ZodString) {
        return s.url(message ?? "Invalid URL");
      }
      return s;
    case "uuid":
      if (s instanceof z.ZodString) {
        return s.uuid(message ?? "Invalid UUID");
      }
      return s;
    case "intent":
      // Async validation is handled in the form runtime, not at schema
      // build time. No-op here.
      return s;
    default:
      return s;
  }
}

function isRequired(f: FieldSpecLite): boolean {
  if (f.required) return true;
  return (f.validate ?? []).some((r) => r.kind === "required");
}
