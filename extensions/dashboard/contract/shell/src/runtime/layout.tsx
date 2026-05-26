import * as React from "react";
import { NavLink, useLocation } from "react-router-dom";
import { Menu as MenuIcon } from "lucide-react";
import { iconFor } from "../layout/icons";
import { AppSwitcher } from "../layout/app-switcher";
import { useApps, useAppStore } from "./apps";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
} from "@/components/ui/breadcrumb";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { ThemeToggle } from "@/components/theme-toggle";
import { useNavigation, type NavItem } from "./navigation";
import { usePrincipalStore } from "../auth/principal";

function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/);
  if (parts.length === 0) return "";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}

interface DashboardLayoutProps {
  /** The current page title; rendered as the active breadcrumb. */
  title?: string;
  children: React.ReactNode;
}

/**
 * DashboardLayout is the slice (l) chrome the React shell renders around
 * every routed page. Sidebar pulls nav data from the contract `navigation`
 * query; topbar carries the breadcrumb, theme toggle and user info. The
 * shadcn shape is deliberately preserved so future shadcn blocks slot in.
 */
export function DashboardLayout({ title, children }: DashboardLayoutProps) {
  // Keep the selected-app store in sync with the URL pathname: navigating
  // directly to a route owned by a different app (e.g. via a deep link)
  // should auto-switch the workspace so the sidebar nav matches the page
  // the user is actually on.
  useSyncSelectedAppToLocation();
  return (
    <SidebarProvider>
      <DashboardSidebar />
      <SidebarInset>
        <DashboardTopbar title={title} />
        <main className="flex-1 overflow-auto p-6">{children}</main>
      </SidebarInset>
    </SidebarProvider>
  );
}

function DashboardSidebar() {
  const nav = useNavigation();
  const selected = useAppStore((s) => s.selected);
  const apps = useApps();

  // Default the active filter to the first (lowest-priority) app when
  // nothing's selected yet — matches AppSwitcher's fallback so the
  // sidebar shows something useful on cold load.
  const activeContributor = React.useMemo(() => {
    const list = apps.data?.apps ?? [];
    if (selected && list.some((a) => a.contributor === selected)) return selected;
    return list[0]?.contributor ?? null;
  }, [apps.data?.apps, selected]);

  const groups = React.useMemo(() => {
    const raw = nav.data?.groups ?? [];
    if (!activeContributor) return raw;
    // Scope sidebar to the active app: drop items from other apps, then
    // drop any group that ended up empty. Items lacking a contributor
    // field (older payloads, library contributors) fall through with the
    // active app so they don't silently disappear.
    return raw
      .map((g) => ({
        ...g,
        items: g.items.filter((it) => !it.contributor || it.contributor === activeContributor),
      }))
      .filter((g) => g.items.length > 0);
  }, [activeContributor, nav.data?.groups]);

  return (
    // Standard (non-inset) sidebar so the switcher trigger sits flush
    // with the page chrome and the dropdown opens cleanly to the side.
    // The inset variant adds a wrapper that interfered with the
    // Base UI dropdown's positioning when expanded.
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <AppSwitcher />
      </SidebarHeader>
      <SidebarContent>
        {nav.isLoading ? (
          <SidebarGroup>
            <SidebarGroupContent>
              <Skeleton className="h-6 w-full" />
            </SidebarGroupContent>
          </SidebarGroup>
        ) : (
          groups.map((g) => (
            <SidebarGroup key={g.group}>
              <SidebarGroupLabel>{g.group}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {g.items.map((item) => (
                    <NavLinkItem key={item.href} item={item} />
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))
        )}
      </SidebarContent>
      <SidebarFooter>
        <UserBadge />
      </SidebarFooter>
    </Sidebar>
  );
}

// useSyncSelectedAppToLocation auto-switches the active app whenever the
// browser URL lands on a route owned by a different app. Without this,
// deep-linking to /users would render the page correctly but the sidebar
// would still show the previously-selected app's nav.
//
// The lookup uses the navigation response (which carries each item's
// contributor) as the route → contributor index. Routes not present in
// the nav (detail pages, login, etc.) are ignored.
function useSyncSelectedAppToLocation() {
  const location = useLocation();
  const nav = useNavigation();
  const selected = useAppStore((s) => s.selected);
  const setSelected = useAppStore((s) => s.setSelected);

  React.useEffect(() => {
    const groups = nav.data?.groups;
    if (!groups) return;
    for (const g of groups) {
      for (const item of g.items) {
        if (item.href === location.pathname && item.contributor && item.contributor !== selected) {
          setSelected(item.contributor);
          return;
        }
      }
    }
  }, [location.pathname, nav.data?.groups, selected, setSelected]);
}

function NavLinkItem({ item }: { item: NavItem }) {
  const Icon = iconFor(item.icon);
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild>
        <NavLink to={item.href} end={item.href === "/"} className="flex w-full items-center gap-2">
          {({ isActive }) => (
            <span
              data-active={isActive ? "true" : undefined}
              className="flex w-full items-center gap-2"
            >
              <Icon className="h-4 w-4" />
              <span className="group-data-[collapsible=icon]:hidden">{item.label}</span>
              {item.badge ? (
                <span className="ml-auto rounded bg-sidebar-accent px-1.5 py-0.5 text-xs group-data-[collapsible=icon]:hidden">
                  {item.badge}
                </span>
              ) : null}
            </span>
          )}
        </NavLink>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function UserBadge() {
  const principal = usePrincipalStore((s) => s.principal);
  if (!principal || !principal.subject) {
    return null;
  }
  return (
    <div className="flex items-center gap-2 rounded-md p-2 text-sm group-data-[collapsible=icon]:justify-center">
      <Avatar className="h-7 w-7">
        <AvatarFallback className="text-xs">{initialsOf(principal.displayName)}</AvatarFallback>
      </Avatar>
      <div className="flex min-w-0 flex-1 flex-col group-data-[collapsible=icon]:hidden">
        <span className="truncate text-sm font-medium">{principal.displayName}</span>
        {principal.email ? (
          <span className="truncate text-xs text-muted-foreground">{principal.email}</span>
        ) : null}
      </div>
    </div>
  );
}

function DashboardTopbar({ title }: { title?: string }) {
  const location = useLocation();
  const display = title ?? prettifyPath(location.pathname);
  return (
    <header className="flex h-14 items-center gap-3 border-b border-border bg-background px-4">
      <SidebarTrigger>
        <MenuIcon className="h-4 w-4" />
      </SidebarTrigger>
      <Separator orientation="vertical" className="h-6" />
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbPage>{display}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <div className="ml-auto flex items-center gap-3">
        <ThemeToggle />
      </div>
    </header>
  );
}

// prettifyPath is a fallback breadcrumb label when the current node has no
// title — turns "/metrics/live" into "Metrics > Live" and "/" into "Home".
function prettifyPath(path: string): string {
  const trimmed = path.replace(/^\/+|\/+$/g, "");
  if (trimmed === "") return "Home";
  return trimmed
    .split("/")
    .map((seg) => seg.charAt(0).toUpperCase() + seg.slice(1))
    .join(" › ");
}
