import { Building2, Check, ChevronsUpDown, Globe2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useContractCommand, useContractQuery } from "../contract/hooks";
import { cn } from "@/lib/utils";

// The auth contributor's apps.context query / switch commands live
// under the "auth" contributor name. Keep this constant rather than
// passing the contributor as a prop — the tenant switcher is auth-
// specific and shouldn't render under other contributors.
const AUTH_CONTRIBUTOR = "auth";

interface AppSummary {
  id: string;
  name: string;
  slug: string;
  logo?: string;
  isPlatform: boolean;
}

interface EnvSummary {
  id: string;
  name: string;
  slug: string;
  type?: string;
  isDefault: boolean;
}

interface AppContextResponse {
  currentApp?: AppSummary | null;
  currentEnv?: EnvSummary | null;
  availableApps: AppSummary[];
  availableEnvs: EnvSummary[];
}

/**
 * useAuthAppContext fetches the active app + environment metadata
 * from the auth contributor's `apps.context` query. Returns null
 * (rather than throwing) when authsome isn't installed — letting
 * the switcher render conditionally without a try/catch boundary.
 */
export function useAuthAppContext() {
  return useContractQuery<AppContextResponse>(AUTH_CONTRIBUTOR, "apps.context");
}

/**
 * TenantSwitcher renders two stacked dropdowns inside the sidebar
 * header: the active authsome App (multi-tenant boundary) and the
 * active Environment (dev/staging/prod within that app). Each
 * selection dispatches the relevant switch command and refetches the
 * context query. The whole widget is hidden when the auth contributor
 * isn't registered or the user has only one app + one env (nothing
 * useful to switch to).
 */
export function TenantSwitcher() {
  const ctx = useAuthAppContext();

  if (ctx.isLoading || ctx.error || !ctx.data) {
    return null;
  }
  const apps = ctx.data.availableApps ?? [];
  const envs = ctx.data.availableEnvs ?? [];
  if (apps.length <= 1 && envs.length <= 1) {
    // Single-tenant deployment — nothing to switch between.
    return null;
  }

  return (
    <SidebarMenu>
      {apps.length > 1 ? (
        <AppSwitcherItem
          apps={apps}
          current={ctx.data.currentApp ?? undefined}
          onSwitched={() => ctx.refetch()}
        />
      ) : null}
      {envs.length > 1 ? (
        <EnvSwitcherItem
          envs={envs}
          current={ctx.data.currentEnv ?? undefined}
          onSwitched={() => ctx.refetch()}
        />
      ) : null}
    </SidebarMenu>
  );
}

interface AppSwitcherItemProps {
  apps: AppSummary[];
  current?: AppSummary;
  onSwitched: () => void;
}

function AppSwitcherItem({ apps, current, onSwitched }: AppSwitcherItemProps) {
  const cmd = useContractCommand<{ appId: string }, unknown>(AUTH_CONTRIBUTOR, "apps.switch");
  const pick = async (appID: string) => {
    if (current && appID === current.id) return;
    try {
      await cmd.mutateAsync({ appId: appID });
      // Reload the page so cookie-scoped queries re-run with the new
      // app context. Cheaper than threading invalidation across every
      // open query.
      window.location.reload();
    } catch {
      // Refetch surfaces the rolled-back state to the UI.
      onSwitched();
    }
  };

  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuButton size="lg" className="data-[state=open]:bg-sidebar-accent">
            <div className="flex aspect-square size-8 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground">
              <Building2 className="size-4" />
            </div>
            <div className="grid flex-1 text-left text-sm leading-tight group-data-[collapsible=icon]:hidden">
              <span className="truncate font-semibold">
                {current?.name ?? "Select app"}
              </span>
              <span className="truncate text-xs text-muted-foreground">
                {current?.slug ? `@${current.slug}` : "tenant"}
              </span>
            </div>
            <ChevronsUpDown className="ml-auto size-4 opacity-60 group-data-[collapsible=icon]:hidden" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56" align="start">
          {/* Plain div instead of DropdownMenuLabel — Base UI's
              Menu.GroupLabel (which DropdownMenuLabel wraps) requires
              being inside a Menu.Group context. Using it outside throws
              error #31 and blanks the page when the dropdown opens.
              The existing AppSwitcher hit the same pitfall — see its
              comment for the full backstory. */}
          <div className="px-2 py-1.5 text-xs font-semibold uppercase text-muted-foreground">
            Apps
          </div>
          {apps.map((a) => (
            <DropdownMenuItem
              key={a.id}
              disabled={cmd.isPending}
              onClick={() => pick(a.id)}
              className="gap-2"
            >
              <Building2 className="size-4" />
              <div className="flex flex-1 flex-col">
                <span className={cn(a.isPlatform && "font-medium")}>{a.name}</span>
                <span className="text-xs text-muted-foreground">@{a.slug}</span>
              </div>
              {current?.id === a.id ? <Check className="size-4 text-primary" /> : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  );
}

interface EnvSwitcherItemProps {
  envs: EnvSummary[];
  current?: EnvSummary;
  onSwitched: () => void;
}

function EnvSwitcherItem({ envs, current, onSwitched }: EnvSwitcherItemProps) {
  const cmd = useContractCommand<{ envId: string }, unknown>(
    AUTH_CONTRIBUTOR,
    "environments.switch",
  );
  const pick = async (envID: string) => {
    if (current && envID === current.id) return;
    try {
      await cmd.mutateAsync({ envId: envID });
      window.location.reload();
    } catch {
      onSwitched();
    }
  };

  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuButton
            size="sm"
            className="data-[state=open]:bg-sidebar-accent text-xs"
          >
            <Globe2 className="size-3.5" />
            <span className="group-data-[collapsible=icon]:hidden">
              {current?.name ?? "Default"}
            </span>
            <ChevronsUpDown className="ml-auto size-3.5 opacity-60 group-data-[collapsible=icon]:hidden" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-48" align="start">
          {/* Plain div instead of DropdownMenuLabel — see AppSwitcherItem
              comment above for why. */}
          <div className="px-2 py-1.5 text-xs font-semibold uppercase text-muted-foreground">
            Environment
          </div>
          <div className="mx-1 my-1 h-px bg-border" aria-hidden />
          {envs.map((e) => (
            <DropdownMenuItem
              key={e.id}
              disabled={cmd.isPending}
              onClick={() => pick(e.id)}
              className="gap-2"
            >
              <Globe2 className="size-3.5" />
              <span className="flex-1">{e.name}</span>
              {e.isDefault ? (
                <span className="text-xs text-muted-foreground">default</span>
              ) : null}
              {current?.id === e.id ? <Check className="size-3.5 text-primary" /> : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  );
}
