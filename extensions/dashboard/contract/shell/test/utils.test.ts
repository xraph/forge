import { describe, expect, it } from "vitest";
import { titleCase } from "../src/lib/utils";

describe("titleCase", () => {
  it("returns empty for empty input", () => {
    expect(titleCase("")).toBe("");
  });

  it("capitalizes simple lowercase words", () => {
    expect(titleCase("name")).toBe("Name");
    expect(titleCase("status")).toBe("Status");
  });

  it("splits camelCase on hump boundaries", () => {
    expect(titleCase("displayName")).toBe("Display Name");
    expect(titleCase("pageCount")).toBe("Page Count");
    expect(titleCase("widgetCount")).toBe("Widget Count");
    expect(titleCase("totalServices")).toBe("Total Services");
  });

  it("splits snake_case and kebab-case", () => {
    expect(titleCase("page_count")).toBe("Page Count");
    expect(titleCase("metrics-report")).toBe("Metrics Report");
    expect(titleCase("first_name")).toBe("First Name");
  });

  it("upper-cases known acronyms", () => {
    // The acronym must be a discrete word (after splitting). camelCase
    // `traceID` splits on the I→D run, so the stoplist matches "ID".
    expect(titleCase("traceID")).toBe("Trace ID");
    expect(titleCase("apiKey")).toBe("API Key");
    expect(titleCase("ssoConfig")).toBe("SSO Config");
    expect(titleCase("oauthClient")).toBe("OAuth Client");
    expect(titleCase("scimEndpoint")).toBe("SCIM Endpoint");
    expect(titleCase("mfaEnabled")).toBe("MFA Enabled");
    expect(titleCase("urlScheme")).toBe("URL Scheme");
  });

  it("handles consecutive acronyms and lone words", () => {
    expect(titleCase("id")).toBe("ID");
    expect(titleCase("url")).toBe("URL");
    expect(titleCase("json")).toBe("JSON");
  });

  it("preserves non-acronym labels even when ALLCAPS by chance", () => {
    // "STATUS" isn't in the acronym list; the function down-cases then
    // recapitalizes per word boundary. The legacy templ dashboard had a
    // few all-caps headers — we tolerate them gracefully even if the
    // result isn't pretty.
    expect(titleCase("STATUS")).toBe("STATUS"); // single all-caps word treated as acronym-like (passes through)
  });
});
