import * as React from "react";
import { useNavigate } from "react-router-dom";
import { ChevronsUpDown, Check } from "lucide-react";
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
  useSidebar,
} from "@/components/ui/sidebar";
import { ForgeLogo } from "@/components/forge-logo";
import { Skeleton } from "@/components/ui/skeleton";
import { useApps, useAppStore, type AppInfo } from "../runtime/apps";
import { iconFor } from "./icons";

/**
 * AppSwitcher renders the sidebar header's active-app pill plus a
 * dropdown listing every contributor that opted into being a switchable
 * app (contributor.app: block in its contract manifest). Picking an
 * app navigates to its prefixed home and triggers the sidebar to
 * filter nav to that app's items.
 *
 * Shape mirrors shadcn's TeamSwitcher block — SidebarMenuButton trigger
 * with a square icon tile, two-line text (display name + @slug), and a
 * ChevronsUpDown grip. SidebarMenuButton is the trigger surface the
 * sidebar primitive expects, so the dropdown opens reliably in both
 * expanded and icon-collapsed states.
 */
export function AppSwitcher() {
  const apps = useApps();
  const selected = useAppStore((s) => s.selected);
  const setSelected = useAppStore((s) => s.setSelected);
  const navigate = useNavigate();
  const { isMobile } = useSidebar();

  const list = apps.data?.apps ?? [];
  // Fall back to the first (lowest-priority) app when nothing is
  // selected or the persisted selection points at a contributor that no
  // longer exists (e.g. an extension was uninstalled).
  const active = React.useMemo<AppInfo | undefined>(() => {
    if (selected) {
      const hit = list.find((a) => a.contributor === selected);
      if (hit) return hit;
    }
    return list[0];
  }, [list, selected]);

  const onSelect = React.useCallback(
    (app: AppInfo) => {
      setSelected(app.contributor);
      if (app.home) {
        navigate(app.home);
      }
    },
    [navigate, setSelected],
  );

  if (apps.isLoading || !active) {
    return (
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton size="lg" disabled>
            <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
              <ForgeLogo className="size-4" />
            </div>
            <div className="grid flex-1 text-left text-sm leading-tight">
              <Skeleton className="h-3.5 w-16" />
              <Skeleton className="mt-1 h-2.5 w-10" />
            </div>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    );
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <AppIconTile app={active} />
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">{active.displayName}</span>
                {active.slug ? (
                  <span className="truncate text-xs text-muted-foreground">@{active.slug}</span>
                ) : null}
              </div>
              <ChevronsUpDown className="ml-auto size-4 opacity-60" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="min-w-56 rounded-lg"
            align="start"
            side={isMobile ? "bottom" : "right"}
            sideOffset={4}
          >
            {/*
              Plain div instead of DropdownMenuLabel — Base UI's Menu.GroupLabel
              (which DropdownMenuLabel wraps) requires being inside a Menu.Group
              context. Using it outside throws a context error that bubbles up
              and blanks the page when the dropdown opens. A non-interactive
              label doesn't need the menu semantics anyway.
            */}
            <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">Apps</div>
            {list.map((app) => (
              <DropdownMenuItem
                key={app.contributor}
                onClick={() => onSelect(app)}
                className="cursor-pointer gap-2 p-2"
              >
                <div className="flex size-6 shrink-0 items-center justify-center rounded-md border">
                  <AppItemIcon app={app} />
                </div>
                <span className="flex-1 truncate">{app.displayName}</span>
                {app.contributor === active.contributor ? (
                  <Check className="size-4 opacity-80" />
                ) : null}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

// AppIconTile is the large icon block in the trigger — colored
// background, contrast-foreground glyph. The "forge" icon name renders
// the inline ForgeLogo SVG so the brand mark stays distinct from stock
// lucide glyphs; everything else falls through to the lucide registry.
function AppIconTile({ app }: { app: AppInfo }) {
  return (
    <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
      <AppIconGlyph app={app} className="size-4" />
    </div>
  );
}

// AppItemIcon is the smaller bordered icon used inside the dropdown
// list. Same glyph resolution as the trigger tile but in the bordered
// chip style the shadcn TeamSwitcher uses.
function AppItemIcon({ app }: { app: AppInfo }) {
  return <AppIconGlyph app={app} className="size-3.5 shrink-0" />;
}

function AppIconGlyph({ app, className }: { app: AppInfo; className?: string }) {
  if (app.icon === "forge") {
    return <ForgeLogo className={className} />;
  }
  const Icon = iconFor(app.icon);
  return <Icon className={className} />;
}
