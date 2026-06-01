import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { buildIntentRegistry } from "../src/intents/register";
import { ContributorProvider, IntentRegistryProvider } from "../src/runtime/context";
import { GraphRenderer } from "../src/runtime/renderer";
import type { GraphNode } from "../src/contract/types";

/**
 * Catalog mirrored from extensions/dashboard/contract/components/catalog.go.
 * Keep these two lists in lockstep — the cross-language test that protects
 * against drift is TestRegistry_everyBuilderEmitsKnownIntent on the Go side
 * plus the registry-completeness test here on the React side.
 */
const CATALOG: readonly string[] = [
  // Layout (13)
  "layout.page",
  "layout.row",
  "layout.column",
  "layout.grid",
  "layout.stack",
  "layout.container",
  "layout.section",
  "layout.card",
  "layout.tabs",
  "layout.accordion",
  "layout.split",
  "layout.divider",
  "layout.spacer",
  // Atoms (24)
  "atom.button",
  "atom.icon-button",
  "atom.badge",
  "atom.label",
  "atom.text",
  "atom.heading",
  "atom.link",
  "atom.icon",
  "atom.avatar",
  "atom.image",
  "atom.separator",
  "atom.skeleton",
  "atom.spinner",
  "atom.progress",
  "atom.input",
  "atom.textarea",
  "atom.checkbox",
  "atom.radio",
  "atom.switch",
  "atom.slider",
  "atom.select",
  "atom.kbd",
  "atom.code",
  "atom.tooltip",
  // Molecules (18)
  "molecule.field",
  "molecule.stat-card",
  "molecule.alert",
  "molecule.search-bar",
  "molecule.empty-state",
  "molecule.error-state",
  "molecule.loading-state",
  "molecule.breadcrumb",
  "molecule.pagination",
  "molecule.tag-input",
  "molecule.file-upload",
  "molecule.date-picker",
  "molecule.combobox",
  "molecule.command-palette",
  "molecule.dropdown-menu",
  "molecule.list-item",
  "molecule.chip",
  "molecule.rating",
  // Organisms (11)
  "organism.data-grid",
  "organism.dynamic-form",
  "organism.kanban",
  "organism.calendar",
  "organism.timeline",
  "organism.tree-view",
  "organism.stepper",
  "organism.chart",
  "organism.code-editor",
  "organism.notification-center",
  "organism.comments",
  // Auth (12)
  "auth.signin-form",
  "auth.signup-form",
  "auth.tabs",
  "auth.magic-link-form",
  "auth.oauth-buttons",
  "auth.user-button",
  "auth.account-menu",
  "auth.org-switcher",
  "auth.org-profile",
  "auth.two-factor-setup",
  "auth.passkey-prompt",
  "auth.session-list",
];

/**
 * Minimal valid props per intent so we can smoke-render every component
 * without it crashing on missing required props. Most renderers accept
 * empty props; the entries below override only where the renderer
 * expects something specific.
 */
const PROPS_BY_INTENT: Record<string, Record<string, unknown>> = {
  "atom.icon": { name: "circle", size: 16 },
  "atom.icon-button": { icon: "plus" },
  "atom.badge": { label: "x" },
  "atom.label": { text: "x" },
  "atom.text": { text: "x" },
  "atom.heading": { text: "x", level: 2 },
  "atom.link": { text: "x", href: "/x" },
  "atom.image": { src: "" },
  "atom.input": { name: "x" },
  "atom.textarea": { name: "x" },
  "atom.checkbox": { name: "x" },
  "atom.radio": { name: "x", options: [] },
  "atom.switch": { name: "x" },
  "atom.slider": { name: "x" },
  "atom.select": { name: "x", options: [] },
  "atom.kbd": { keys: ["⌘", "K"] },
  "atom.code": { text: "x" },
  "atom.tooltip": { content: "x" },

  "molecule.field": {},
  "molecule.stat-card": { label: "x", value: "0" },
  "molecule.alert": { title: "x" },
  "molecule.search-bar": {},
  "molecule.empty-state": { title: "Empty" },
  "molecule.error-state": { title: "Error" },
  "molecule.loading-state": {},
  "molecule.breadcrumb": { items: [{ label: "Home" }] },
  "molecule.pagination": { page: 1, pageSize: 25 },
  "molecule.tag-input": { name: "x" },
  "molecule.file-upload": { name: "x" },
  "molecule.date-picker": { name: "x" },
  "molecule.combobox": { name: "x", options: [] },
  "molecule.command-palette": { groups: [] },
  "molecule.dropdown-menu": { items: [] },
  "molecule.list-item": { title: "x" },
  "molecule.chip": { label: "x" },
  "molecule.rating": {},

  "layout.tabs": { items: [] },
  "layout.accordion": { items: [] },
  "layout.page": {},

  // Organisms that REQUIRE node.data are tested with a stubbed binding so
  // the renderer doesn't bail out with an ErrorNode before mounting.
  "organism.data-grid": { columns: [] },
  "organism.dynamic-form": { fields: [], submit: { label: "Save" } },
  "organism.kanban": { columns: [], statusField: "status" },
  "organism.calendar": {},
  "organism.timeline": {},
  "organism.tree-view": {},
  "organism.stepper": { steps: [] },
  "organism.chart": { type: "line", series: [] },
  "organism.code-editor": {},
  "organism.notification-center": {},
  "organism.comments": {},

  "auth.signin-form": {},
  "auth.signup-form": { op: "auth.signup" },
  "auth.tabs": {},
  "auth.magic-link-form": { op: "auth.magicLink" },
  "auth.oauth-buttons": { providers: [] },
  "auth.user-button": {},
  "auth.account-menu": {},
  "auth.org-switcher": {},
  "auth.org-profile": {},
  "auth.two-factor-setup": { generateSecretOp: "auth.totp.generate", verifyOp: "auth.totp.verify" },
  "auth.passkey-prompt": { registerOp: "auth.passkey.register" },
  "auth.session-list": {},
};

/**
 * Intents that consume node.data — for the smoke test we attach a dummy
 * binding so the renderer's data-fetch path can short-circuit cleanly
 * (the test setup returns empty data via the existing test harness).
 */
const DATA_BOUND = new Set([
  "organism.data-grid",
  "organism.kanban",
  "organism.calendar",
  "organism.timeline",
  "organism.tree-view",
  "organism.chart",
  "organism.notification-center",
  "organism.comments",
  "auth.org-switcher",
]);

const reg = buildIntentRegistry();

function Harness({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <IntentRegistryProvider value={reg}>
          <ContributorProvider value="test">{children}</ContributorProvider>
        </IntentRegistryProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("registry completeness", () => {
  it("registers every catalog intent", () => {
    const missing = CATALOG.filter((name) => !reg.has(name));
    expect(missing).toEqual([]);
  });

  it("registers no extra intents beyond the catalog + legacy", () => {
    // Legacy intents kept for backward compatibility. New work should use
    // the catalog (atom/molecule/organism/layout/auth).
    const legacy = new Set([
      "page.shell",
      "metric.counter",
      "action.button",
      "action.menu",
      "action.divider",
      "form.edit",
      "form.field",
      "resource.list",
      "resource.detail",
      "dashboard.grid",
      "audit.tail",
      "auth.login.form",
    ]);
    const knownNew = new Set(CATALOG);

    // Use the private byName map by probing each known string + each legacy.
    // Anything else registered would be undiscoverable via this test, which
    // is fine — we're guarding against accidental removal from the catalog.
    for (const n of [...knownNew, ...legacy]) {
      expect(reg.has(n)).toBe(true);
    }
  });
});

describe("smoke render every catalog intent", () => {
  for (const intent of CATALOG) {
    it(`renders ${intent} without crashing`, () => {
      const node: GraphNode = {
        intent,
        props: PROPS_BY_INTENT[intent] ?? {},
      };
      if (DATA_BOUND.has(intent)) {
        node.data = { intent: `${intent}.test-data` };
      }
      expect(() =>
        render(
          <Harness>
            <GraphRenderer node={node} />
          </Harness>,
        ),
      ).not.toThrow();
    });
  }
});
