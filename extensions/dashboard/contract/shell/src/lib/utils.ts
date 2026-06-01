import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn merges Tailwind class strings, deduplicating conflicts via tailwind-merge. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

// Canonical capitalizations for tokens that don't take plain title-case.
// Keyed by lowercase form so the matcher stays case-insensitive. True
// acronyms map to all-caps ("API", "URL"); stylized brand names take
// whatever case the brand uses ("OAuth", "OAuth2", "GraphQL").
//
// Add entries here when new jargon shows up in manifests — extending
// this map is the override path when a column key isn't worth promoting
// to an explicit `{field, label}` in the YAML.
const ACRONYM_CANONICAL: Record<string, string> = {
  id: "ID",
  url: "URL",
  uri: "URI",
  api: "API",
  css: "CSS",
  html: "HTML",
  json: "JSON",
  yaml: "YAML",
  http: "HTTP",
  https: "HTTPS",
  ip: "IP",
  sso: "SSO",
  mfa: "MFA",
  "2fa": "2FA",
  oauth: "OAuth",
  oauth2: "OAuth2",
  graphql: "GraphQL",
  scim: "SCIM",
  sdk: "SDK",
  cli: "CLI",
  tls: "TLS",
  ssl: "SSL",
  cors: "CORS",
  csrf: "CSRF",
  uuid: "UUID",
  ms: "ms",
  ns: "ns",
};

/**
 * titleCase converts a camelCase/snake_case/kebab-case identifier into a
 * human-readable label suitable for a table column header. Splits on word
 * boundaries (camel case humps, underscores, hyphens) and capitalizes each
 * piece — except when the piece matches a known acronym, in which case the
 * whole acronym is upper-cased.
 *
 * Examples:
 *   titleCase("name")          // "Name"
 *   titleCase("displayName")   // "Display Name"
 *   titleCase("page_count")    // "Page Count"
 *   titleCase("traceID")       // "Trace ID"
 *   titleCase("apiKey")        // "API Key"
 *   titleCase("durationMs")    // "Duration Ms"   (override via {field,label} for "Duration (ms)")
 *   titleCase("")              // ""
 *
 * The output is for display only — never use it as a lookup key for the
 * underlying data.
 */
export function titleCase(field: string): string {
  if (!field) return "";
  // Split on underscores and hyphens, then split each piece on camelCase
  // humps. Filter out empty pieces from leading separators.
  const parts = field
    .split(/[_-]+/)
    .flatMap((p) => p.split(/(?<=[a-z0-9])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])/))
    .filter(Boolean);
  return parts
    .map((p) => {
      const canonical = ACRONYM_CANONICAL[p.toLowerCase()];
      if (canonical) return canonical;
      return p.charAt(0).toUpperCase() + p.slice(1);
    })
    .join(" ");
}
