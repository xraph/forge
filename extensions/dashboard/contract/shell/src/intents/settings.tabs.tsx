import * as React from "react";
import {
  Tabs as TabsPrimitive,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
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
import { SettingsPanel, type SettingsScope } from "./settings.panel";
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
}

/**
 * settings.tabs is the top-level settings page composite. It fetches
 * the list of plugin namespaces from `settings.namespaces`, renders a
 * tab per namespace, and embeds a `<SettingsPanel>` in each tab body
 * bound to the active scope.
 *
 * The scope picker is a top-of-page select that flips all child panels
 * between global / app / org / user. Choices that have no concrete ID
 * in the current context are disabled — e.g. "Org" is disabled when no
 * organization is active in the dashboard's switcher.
 */
export function SettingsTabs({ props }: IntentComponentProps<unknown, SettingsTabsProps>) {
  const contributor = useContributor();
  const namespacesIntent = props.namespacesIntent ?? "settings.namespaces";
  const panelIntent = props.panelIntent ?? "settings.namespace";
  const updateOp = props.updateOp ?? "settings.update";

  const query = useContractQuery<NamespacesPayload>(contributor, namespacesIntent);

  const [scope, setScope] = React.useState<SettingsScope>(props.defaultScope ?? "global");

  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const namespaces = query.data?.namespaces ?? [];
  const ctx = query.data?.context ?? {};
  const appId = props.appId ?? ctx.appId ?? "";
  const orgId = props.orgId ?? ctx.orgId ?? "";
  const userId = props.userId ?? ctx.userId ?? "";

  if (namespaces.length === 0) {
    return (
      <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
        No plugin settings are registered with this deployment.
      </div>
    );
  }

  const firstTab = namespaces[0]?.name ?? "";

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

      <TabsPrimitive defaultValue={firstTab}>
        <TabsList>
          {namespaces.map((ns) => (
            <TabsTrigger key={ns.name} value={ns.name}>
              <span>{ns.displayName ?? ns.name}</span>
              {typeof ns.settingCount === "number" && ns.settingCount > 0 ? (
                <Badge variant="secondary" className="ml-2">
                  {ns.settingCount}
                </Badge>
              ) : null}
            </TabsTrigger>
          ))}
        </TabsList>
        {namespaces.map((ns) => (
          <TabsContent key={ns.name} value={ns.name} className="pt-4">
            <SettingsPanel
              node={{} as never}
              slots={{}}
              props={{
                namespace: ns.name,
                scope,
                appId,
                orgId,
                userId,
                queryIntent: panelIntent,
                updateOp,
              }}
            />
          </TabsContent>
        ))}
      </TabsPrimitive>
    </div>
  );
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
