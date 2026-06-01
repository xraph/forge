import * as React from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { GraphRenderer } from "../runtime/renderer";
import { SettingsPanel, type SettingsScope } from "./settings.panel";
import { cn } from "@/lib/utils";
import type { GraphNode } from "../contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface NamespaceSummary {
  name: string;
  displayName?: string;
  description?: string;
  settingCount?: number;
}

interface NamespacesPayload {
  namespaces: NamespaceSummary[];
  // Optional context echo so the renderer knows which scopes have
  // concrete IDs to bind. Allows the scope picker to disable choices
  // (e.g. "User" when no userId is available).
  context?: {
    appId?: string;
    orgId?: string;
    userId?: string;
  };
}

/**
 * CustomSection lets a manifest inject a sidebar entry that renders
 * an arbitrary intent in the right column instead of the default
 * <SettingsPanel>. Used for things like "Sign-in methods" which is a
 * `feature.toggles` view rather than a plugin namespace.
 */
export interface CustomSection {
  /** Stable identifier used for selection state. Required. */
  key: string;
  /** Sidebar label. */
  label: string;
  /** Optional badge text rendered next to the label. */
  badge?: string;
  /** Intent name to render in the right column when this section is
   *  active. Resolved through the shell's IntentRegistry — must be a
   *  registered intent kind. */
  intent: string;
  /** Optional props forwarded to the rendered intent. */
  props?: Record<string, unknown>;
  /** When true, sticks at the top of the sidebar above plugin
   *  namespaces. Otherwise appears after them. */
  pinned?: boolean;
}

interface SettingsTabsProps {
  /** Intent name fetching the namespace list (default: "settings.namespaces"). */
  namespacesIntent?: string;
  /** Intent name passed through to each settings.panel (default: "settings.namespace"). */
  panelIntent?: string;
  /** Update command name passed through to each panel (default: "settings.update"). */
  updateOp?: string;
  /** Initial scope. Defaults to "global" so we work without app context. */
  defaultScope?: SettingsScope;
  /** Set true to hide the scope picker (useful for plugin-deep-link pages
   *  where the scope is fixed by the URL or surrounding chrome). */
  hideScopePicker?: boolean;
  /** Optional explicit appId / orgId / userId — overrides whatever the
   *  namespaces payload's context echoes back. */
  appId?: string;
  orgId?: string;
  userId?: string;
  /** Custom sidebar entries (e.g. "Sign-in methods" → feature.toggles)
   *  injected alongside the auto-discovered plugin namespaces. */
  customSections?: CustomSection[];
}

/**
 * settings.tabs is the top-level settings page composite. It fetches
 * the list of plugin namespaces from `settings.namespaces` and
 * presents them as a vertical sidebar (rather than a horizontal tab
 * strip — that pattern broke down past ~6 namespaces because the
 * strip ran off the viewport with no scroll affordance). The right
 * column renders the active namespace's `<SettingsPanel>`.
 *
 * Below the `md` breakpoint the sidebar collapses to a dropdown so
 * the page stays usable on narrow viewports.
 *
 * The scope picker is a top-of-page select that flips all child
 * panels between global / app / org / user. Choices that have no
 * concrete ID in the current context are disabled — e.g. "Org" is
 * disabled when no organization is active in the dashboard's switcher.
 */
export function SettingsTabs({ props }: IntentComponentProps<unknown, SettingsTabsProps>) {
  const contributor = useContributor();
  const namespacesIntent = props.namespacesIntent ?? "settings.namespaces";
  const panelIntent = props.panelIntent ?? "settings.namespace";
  const updateOp = props.updateOp ?? "settings.update";

  const query = useContractQuery<NamespacesPayload>(contributor, namespacesIntent);

  const [scope, setScope] = React.useState<SettingsScope>(props.defaultScope ?? "global");
  const [activeNs, setActiveNs] = React.useState<string | null>(null);

  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const namespaces = query.data?.namespaces ?? [];
  const ctx = query.data?.context ?? {};
  const appId = props.appId ?? ctx.appId ?? "";
  const orgId = props.orgId ?? ctx.orgId ?? "";
  const userId = props.userId ?? ctx.userId ?? "";

  // Custom sections come from the manifest (e.g. "Sign-in methods" →
  // feature.toggles). They get unified with the auto-discovered
  // plugin namespaces into a single sidebar list — pinned customs
  // appear at the top, the rest at the bottom.
  const customs = props.customSections ?? [];
  const pinnedCustoms = customs.filter((c) => c.pinned);
  const trailingCustoms = customs.filter((c) => !c.pinned);

  type SidebarEntry =
    | { kind: "custom"; section: CustomSection }
    | { kind: "namespace"; ns: NamespaceSummary };
  const entries: SidebarEntry[] = [
    ...pinnedCustoms.map((s) => ({ kind: "custom" as const, section: s })),
    ...namespaces.map((ns) => ({ kind: "namespace" as const, ns })),
    ...trailingCustoms.map((s) => ({ kind: "custom" as const, section: s })),
  ];

  if (entries.length === 0) {
    return (
      <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
        No plugin settings are registered with this deployment.
      </div>
    );
  }

  const firstKey = entryKey(entries[0]!);
  const active = activeNs ?? firstKey;
  const activeEntry = entries.find((e) => entryKey(e) === active) ?? entries[0]!;

  return (
    <div className="space-y-4">
      {props.hideScopePicker ? null : (
        <div className="flex items-center justify-between gap-3 rounded-md border bg-card px-4 py-3">
          <div className="space-y-1">
            <Label className="text-xs uppercase tracking-wide text-muted-foreground">
              Scope
            </Label>
            <p className="text-xs text-muted-foreground">
              Changes apply at the selected scope and cascade downward.
            </p>
          </div>
          <Select value={scope} onValueChange={(v) => setScope(v as SettingsScope)}>
            <SelectTrigger className="w-44">
              <SelectValue>{scopeLabel(scope)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="global">Global</SelectItem>
              <SelectItem value="app" disabled={!appId}>
                App {appId ? "" : "(no app selected)"}
              </SelectItem>
              <SelectItem value="org" disabled={!orgId}>
                Organization {orgId ? "" : "(no org selected)"}
              </SelectItem>
              <SelectItem value="user" disabled={!userId}>
                User {userId ? "" : "(no user selected)"}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      )}

      {/* Mobile: dropdown picker above the panel. Hidden ≥ md. */}
      <div className="md:hidden">
        <Select value={active} onValueChange={(v) => setActiveNs(v)}>
          <SelectTrigger className="w-full">
            <SelectValue>{entryLabel(activeEntry)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {entries.map((e) => (
              <SelectItem key={entryKey(e)} value={entryKey(e)}>
                {entryLabel(e)}
                {entryBadgeText(e) ? ` (${entryBadgeText(e)})` : null}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Desktop: sidebar + panel grid. Hidden < md. */}
      <div className="hidden md:grid md:grid-cols-[14rem_1fr] md:gap-6">
        <nav
          aria-label="Settings namespaces"
          className="max-h-[calc(100vh-12rem)] overflow-y-auto rounded-md border bg-card p-2"
        >
          <ul className="space-y-1">
            {entries.map((e) => {
              const key = entryKey(e);
              const isActive = key === active;
              const badge = entryBadgeText(e);
              return (
                <li key={key}>
                  <button
                    type="button"
                    onClick={() => setActiveNs(key)}
                    className={cn(
                      "flex w-full items-center justify-between gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors",
                      isActive
                        ? "bg-accent text-accent-foreground"
                        : "text-muted-foreground hover:bg-accent/40 hover:text-foreground",
                    )}
                  >
                    <span className="truncate font-medium">{entryLabel(e)}</span>
                    {badge ? (
                      <Badge variant="secondary" className="ml-2 shrink-0">
                        {badge}
                      </Badge>
                    ) : null}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>
        <div className="min-h-0 min-w-0">
          <SectionRenderer
            entry={activeEntry}
            scope={scope}
            appId={appId}
            orgId={orgId}
            userId={userId}
            panelIntent={panelIntent}
            updateOp={updateOp}
          />
        </div>
      </div>

      {/* Mobile: render the active section below the dropdown picker. */}
      <div className="md:hidden">
        <SectionRenderer
          entry={activeEntry}
          scope={scope}
          appId={appId}
          orgId={orgId}
          userId={userId}
          panelIntent={panelIntent}
          updateOp={updateOp}
        />
      </div>
    </div>
  );
}

// ────────────────────────────────────────────────────────────────────
// Sidebar entry helpers
// ────────────────────────────────────────────────────────────────────

function entryKey(e: {
  kind: "custom" | "namespace";
  section?: CustomSection;
  ns?: NamespaceSummary;
}): string {
  return e.kind === "custom" ? `__custom__${e.section!.key}` : e.ns!.name;
}

function entryLabel(e: {
  kind: "custom" | "namespace";
  section?: CustomSection;
  ns?: NamespaceSummary;
}): string {
  if (e.kind === "custom") return e.section!.label;
  return e.ns!.displayName ?? e.ns!.name;
}

function entryBadgeText(e: {
  kind: "custom" | "namespace";
  section?: CustomSection;
  ns?: NamespaceSummary;
}): string | null {
  if (e.kind === "custom") return e.section!.badge ?? null;
  const count = e.ns!.settingCount;
  return typeof count === "number" && count > 0 ? String(count) : null;
}

// SectionRenderer dispatches the right column based on which entry is
// active: a plugin namespace renders <SettingsPanel>; a custom section
// renders its declared intent via GraphRenderer (which looks the
// intent up in the shell's IntentRegistry).
interface SectionRendererProps {
  entry:
    | { kind: "custom"; section: CustomSection }
    | { kind: "namespace"; ns: NamespaceSummary };
  scope: SettingsScope;
  appId: string;
  orgId: string;
  userId: string;
  panelIntent: string;
  updateOp: string;
}

function SectionRenderer({
  entry,
  scope,
  appId,
  orgId,
  userId,
  panelIntent,
  updateOp,
}: SectionRendererProps) {
  if (entry.kind === "namespace") {
    return (
      <SettingsPanel
        key={`${entry.ns.name}:${scope}`}
        node={{} as never}
        slots={{}}
        props={{
          namespace: entry.ns.name,
          scope,
          appId,
          orgId,
          userId,
          queryIntent: panelIntent,
          updateOp,
        }}
      />
    );
  }
  // Custom section: synthesise a graph node with the declared intent
  // and props, then let GraphRenderer dispatch through the IntentRegistry.
  const node: GraphNode = {
    intent: entry.section.intent,
    props: (entry.section.props ?? {}) as Record<string, unknown>,
  };
  return <GraphRenderer node={node} />;
}

function scopeLabel(s: SettingsScope): string {
  switch (s) {
    case "global":
      return "Global";
    case "app":
      return "App";
    case "org":
      return "Organization";
    case "user":
      return "User";
  }
}
