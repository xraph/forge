import * as React from "react";
import { NavLink, useLocation } from "react-router-dom";
import {
  Activity,
  ChartBar,
  Heart,
  History,
  Home,
  LayoutDashboard,
  Menu as MenuIcon,
  Package,
  Plug,
  ScanSearch,
  Server,
} from "lucide-react";
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

// iconFor maps the manifest's lucide-style icon name to the actual component.
// Only the icons referenced from the pilot manifest are wired; unknowns
// fall back to the generic Plug icon (matches legacy templ default).
const ICONS: Record<string, React.ComponentType<{ className?: string }>> = {
  home: Home,
  "heart-pulse": Heart,
  "chart-bar": ChartBar,
  "scan-search": ScanSearch,
  package: Package,
  server: Server,
  activity: Activity,
  history: History,
};

function iconFor(name?: string) {
  if (!name) return Plug;
  return ICONS[name] ?? Plug;
}

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
  const groups = nav.data?.groups ?? [];
  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <LayoutDashboard className="h-5 w-5" />
          <span className="text-sm font-semibold group-data-[collapsible=icon]:hidden">Forge</span>
        </div>
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
