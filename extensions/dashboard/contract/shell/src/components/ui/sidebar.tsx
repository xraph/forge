import * as React from "react";
import { PanelLeft } from "lucide-react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent } from "@/components/ui/sheet";

// Slice (l) ports shadcn/ui's Sidebar block onto our Base UI primitives. The
// public API mirrors ui.shadcn.com so consumers familiar with shadcn don't
// have to relearn anything; only the internals (mobile drawer, asChild glue)
// are rewritten.

const SIDEBAR_COOKIE = "forge.dashboard.sidebar";
const SIDEBAR_COOKIE_MAX_AGE = 60 * 60 * 24 * 7; // 7 days
const SIDEBAR_WIDTH = "16rem";
const SIDEBAR_WIDTH_ICON = "3rem";
const SIDEBAR_WIDTH_MOBILE = "18rem";
const MOBILE_BREAKPOINT_PX = 768;

interface SidebarContextValue {
  state: "expanded" | "collapsed";
  open: boolean;
  setOpen: (open: boolean) => void;
  openMobile: boolean;
  setOpenMobile: (open: boolean) => void;
  isMobile: boolean;
  toggle: () => void;
}

const SidebarContext = React.createContext<SidebarContextValue | null>(null);

export function useSidebar(): SidebarContextValue {
  const ctx = React.useContext(SidebarContext);
  if (!ctx) throw new Error("useSidebar must be used within a SidebarProvider");
  return ctx;
}

function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = React.useState(() => {
    if (typeof window === "undefined") return false;
    return window.innerWidth < MOBILE_BREAKPOINT_PX;
  });
  React.useEffect(() => {
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`);
    const onChange = () => setIsMobile(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return isMobile;
}

function readCookie(name: string): string | null {
  if (typeof document === "undefined") return null;
  const match = document.cookie.match(new RegExp("(?:^|; )" + name + "=([^;]*)"));
  if (!match || match[1] === undefined) return null;
  return decodeURIComponent(match[1]);
}

function writeCookie(name: string, value: string): void {
  if (typeof document === "undefined") return;
  document.cookie = `${name}=${encodeURIComponent(value)}; path=/; max-age=${SIDEBAR_COOKIE_MAX_AGE}; samesite=lax`;
}

interface SidebarProviderProps extends React.ComponentProps<"div"> {
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export const SidebarProvider = React.forwardRef<HTMLDivElement, SidebarProviderProps>(
  ({ defaultOpen = true, open: openProp, onOpenChange, className, style, children, ...props }, ref) => {
    const isMobile = useIsMobile();
    const [openMobile, setOpenMobile] = React.useState(false);

    const initialOpen = React.useMemo(() => {
      if (openProp !== undefined) return openProp;
      const cookie = readCookie(SIDEBAR_COOKIE);
      if (cookie === "true") return true;
      if (cookie === "false") return false;
      return defaultOpen;
    }, [openProp, defaultOpen]);

    const [internalOpen, setInternalOpen] = React.useState(initialOpen);
    const open = openProp ?? internalOpen;
    const setOpen = React.useCallback(
      (next: boolean) => {
        if (onOpenChange) onOpenChange(next);
        if (openProp === undefined) setInternalOpen(next);
        writeCookie(SIDEBAR_COOKIE, String(next));
      },
      [onOpenChange, openProp],
    );

    const toggle = React.useCallback(() => {
      if (isMobile) setOpenMobile((m) => !m);
      else setOpen(!open);
    }, [isMobile, open, setOpen]);

    React.useEffect(() => {
      const handler = (e: KeyboardEvent) => {
        if ((e.metaKey || e.ctrlKey) && e.key === "b") {
          e.preventDefault();
          toggle();
        }
      };
      window.addEventListener("keydown", handler);
      return () => window.removeEventListener("keydown", handler);
    }, [toggle]);

    const value = React.useMemo<SidebarContextValue>(
      () => ({
        state: open ? "expanded" : "collapsed",
        open,
        setOpen,
        openMobile,
        setOpenMobile,
        isMobile,
        toggle,
      }),
      [open, setOpen, openMobile, isMobile, toggle],
    );

    return (
      <SidebarContext.Provider value={value}>
        <div
          ref={ref}
          data-slot="sidebar-wrapper"
          style={
            {
              "--sidebar-width": SIDEBAR_WIDTH,
              "--sidebar-width-icon": SIDEBAR_WIDTH_ICON,
              ...style,
            } as React.CSSProperties
          }
          className={cn("group/sidebar-wrapper flex min-h-svh w-full has-[[data-variant=inset]]:bg-sidebar", className)}
          {...props}
        >
          {children}
        </div>
      </SidebarContext.Provider>
    );
  },
);
SidebarProvider.displayName = "SidebarProvider";

interface SidebarProps extends React.ComponentProps<"div"> {
  side?: "left" | "right";
  variant?: "sidebar" | "floating" | "inset";
  collapsible?: "offcanvas" | "icon" | "none";
}

export const Sidebar = React.forwardRef<HTMLDivElement, SidebarProps>(
  (
    {
      side = "left",
      variant = "sidebar",
      collapsible = "offcanvas",
      className,
      children,
      ...props
    },
    ref,
  ) => {
    const { isMobile, state, openMobile, setOpenMobile } = useSidebar();

    if (collapsible === "none") {
      return (
        <div
          ref={ref}
          className={cn(
            "flex h-full w-[--sidebar-width] flex-col bg-sidebar text-sidebar-foreground",
            className,
          )}
          {...props}
        >
          {children}
        </div>
      );
    }

    if (isMobile) {
      return (
        <Sheet open={openMobile} onOpenChange={setOpenMobile}>
          <SheetContent
            data-sidebar="sidebar"
            data-mobile="true"
            side={side}
            className="w-[--sidebar-width] bg-sidebar p-0 text-sidebar-foreground [&>button]:hidden"
            style={{ "--sidebar-width": SIDEBAR_WIDTH_MOBILE } as React.CSSProperties}
          >
            <div className="flex h-full w-full flex-col">{children}</div>
          </SheetContent>
        </Sheet>
      );
    }

    return (
      <div
        ref={ref}
        className="group peer hidden text-sidebar-foreground md:block"
        data-state={state}
        data-collapsible={state === "collapsed" ? collapsible : ""}
        data-variant={variant}
        data-side={side}
      >
        {/* sidebar gap: keeps the inset content from shifting */}
        <div
          className={cn(
            "duration-200 relative h-svh w-[--sidebar-width] bg-transparent transition-[width] ease-linear",
            "group-data-[collapsible=offcanvas]:w-0",
            "group-data-[side=right]:rotate-180",
            variant === "floating" || variant === "inset"
              ? "group-data-[collapsible=icon]:w-[calc(var(--sidebar-width-icon)+1rem)]"
              : "group-data-[collapsible=icon]:w-[--sidebar-width-icon]",
          )}
        />
        <div
          className={cn(
            "duration-200 fixed inset-y-0 z-10 hidden h-svh w-[--sidebar-width] transition-[left,right,width] ease-linear md:flex",
            side === "left"
              ? "left-0 group-data-[collapsible=offcanvas]:left-[calc(var(--sidebar-width)*-1)]"
              : "right-0 group-data-[collapsible=offcanvas]:right-[calc(var(--sidebar-width)*-1)]",
            variant === "floating" || variant === "inset"
              ? "p-2 group-data-[collapsible=icon]:w-[calc(var(--sidebar-width-icon)+1rem+2px)]"
              : "group-data-[collapsible=icon]:w-[--sidebar-width-icon] group-data-[side=left]:border-r group-data-[side=right]:border-l",
            className,
          )}
          {...props}
        >
          <div
            data-sidebar="sidebar"
            className="flex h-full w-full flex-col bg-sidebar group-data-[variant=floating]:rounded-lg group-data-[variant=floating]:border group-data-[variant=floating]:border-sidebar-border group-data-[variant=floating]:shadow"
          >
            {children}
          </div>
        </div>
      </div>
    );
  },
);
Sidebar.displayName = "Sidebar";

export const SidebarTrigger = React.forwardRef<
  HTMLButtonElement,
  React.ComponentProps<typeof Button>
>(({ className, onClick, ...props }, ref) => {
  const { toggle } = useSidebar();
  return (
    <Button
      ref={ref}
      variant="ghost"
      size="icon"
      className={cn("h-7 w-7", className)}
      onClick={(e) => {
        onClick?.(e);
        toggle();
      }}
      {...props}
    >
      <PanelLeft className="h-4 w-4" />
      <span className="sr-only">Toggle Sidebar</span>
    </Button>
  );
});
SidebarTrigger.displayName = "SidebarTrigger";

export const SidebarInset = React.forwardRef<HTMLElement, React.ComponentProps<"main">>(
  ({ className, ...props }, ref) => (
    <main
      ref={ref}
      className={cn(
        "relative flex min-h-svh flex-1 flex-col bg-background",
        "peer-data-[variant=inset]:min-h-[calc(100svh-theme(spacing.4))] md:peer-data-[variant=inset]:m-2 md:peer-data-[state=collapsed]:peer-data-[variant=inset]:ml-2 md:peer-data-[variant=inset]:ml-0 md:peer-data-[variant=inset]:rounded-xl md:peer-data-[variant=inset]:shadow",
        className,
      )}
      {...props}
    />
  ),
);
SidebarInset.displayName = "SidebarInset";

export const SidebarHeader = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex flex-col gap-2 p-2", className)} {...props} />
  ),
);
SidebarHeader.displayName = "SidebarHeader";

export const SidebarFooter = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex flex-col gap-2 p-2", className)} {...props} />
  ),
);
SidebarFooter.displayName = "SidebarFooter";

export const SidebarSeparator = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn("mx-2 my-1 h-px bg-sidebar-border", className)}
      {...props}
    />
  ),
);
SidebarSeparator.displayName = "SidebarSeparator";

export const SidebarContent = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        "flex min-h-0 flex-1 flex-col gap-2 overflow-auto group-data-[collapsible=icon]:overflow-hidden",
        className,
      )}
      {...props}
    />
  ),
);
SidebarContent.displayName = "SidebarContent";

export const SidebarGroup = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("relative flex w-full min-w-0 flex-col p-2", className)} {...props} />
  ),
);
SidebarGroup.displayName = "SidebarGroup";

export const SidebarGroupLabel = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        "duration-200 flex h-8 shrink-0 items-center rounded-md px-2 text-xs font-medium text-sidebar-foreground/70 transition-[margin,opacity] ease-linear",
        "group-data-[collapsible=icon]:-mt-8 group-data-[collapsible=icon]:opacity-0",
        className,
      )}
      {...props}
    />
  ),
);
SidebarGroupLabel.displayName = "SidebarGroupLabel";

export const SidebarGroupContent = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("w-full text-sm", className)} {...props} />
  ),
);
SidebarGroupContent.displayName = "SidebarGroupContent";

export const SidebarMenu = React.forwardRef<HTMLUListElement, React.ComponentProps<"ul">>(
  ({ className, ...props }, ref) => (
    <ul ref={ref} className={cn("flex w-full min-w-0 flex-col gap-1", className)} {...props} />
  ),
);
SidebarMenu.displayName = "SidebarMenu";

export const SidebarMenuItem = React.forwardRef<HTMLLIElement, React.ComponentProps<"li">>(
  ({ className, ...props }, ref) => (
    <li ref={ref} className={cn("group/menu-item relative", className)} {...props} />
  ),
);
SidebarMenuItem.displayName = "SidebarMenuItem";

const sidebarMenuButtonVariants = cva(
  "peer/menu-button flex w-full items-center gap-2 overflow-hidden rounded-md p-2 text-left text-sm outline-none ring-sidebar-ring transition-[width,height,padding] hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 active:bg-sidebar-accent active:text-sidebar-accent-foreground disabled:pointer-events-none disabled:opacity-50 data-[active=true]:bg-sidebar-accent data-[active=true]:font-medium data-[active=true]:text-sidebar-accent-foreground group-data-[collapsible=icon]:!size-8 group-data-[collapsible=icon]:!p-2 [&>svg]:size-4 [&>svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "",
        outline: "bg-background shadow-[0_0_0_1px_hsl(var(--sidebar-border))]",
      },
      size: {
        default: "h-8",
        sm: "h-7 text-xs",
        lg: "h-12 text-sm group-data-[collapsible=icon]:!p-0",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

interface SidebarMenuButtonProps
  extends React.ComponentProps<"button">,
    VariantProps<typeof sidebarMenuButtonVariants> {
  asChild?: boolean;
  isActive?: boolean;
}

/**
 * Local Slot mirrors the helper in button.tsx — clones a single child element
 * and merges className / props onto it. Keeps SidebarMenuButton free of any
 * Radix dependency.
 */
function MenuSlot({
  children,
  className,
  ...rest
}: { children: React.ReactNode; className?: string } & Record<string, unknown>) {
  if (!React.isValidElement(children)) return null;
  const child = children as React.ReactElement<Record<string, unknown>>;
  const childClassName =
    typeof child.props.className === "string" ? child.props.className : undefined;
  return React.cloneElement(child, {
    ...rest,
    ...child.props,
    className: cn(className, childClassName),
  });
}

export const SidebarMenuButton = React.forwardRef<HTMLButtonElement, SidebarMenuButtonProps>(
  ({ asChild, isActive, variant, size, className, children, ...props }, ref) => {
    const cls = cn(sidebarMenuButtonVariants({ variant, size, className }));
    const dataActive = isActive ? "true" : undefined;
    if (asChild) {
      return (
        <MenuSlot className={cls} data-active={dataActive} {...(props as Record<string, unknown>)}>
          {children}
        </MenuSlot>
      );
    }
    return (
      <button ref={ref} className={cls} data-active={dataActive} {...props}>
        {children}
      </button>
    );
  },
);
SidebarMenuButton.displayName = "SidebarMenuButton";

export const SidebarMenuSub = React.forwardRef<HTMLUListElement, React.ComponentProps<"ul">>(
  ({ className, ...props }, ref) => (
    <ul
      ref={ref}
      className={cn(
        "mx-3.5 flex min-w-0 translate-x-px flex-col gap-1 border-l border-sidebar-border px-2.5 py-0.5",
        "group-data-[collapsible=icon]:hidden",
        className,
      )}
      {...props}
    />
  ),
);
SidebarMenuSub.displayName = "SidebarMenuSub";

export const SidebarMenuSubItem = SidebarMenuItem;

export const SidebarMenuSubButton = React.forwardRef<
  HTMLAnchorElement,
  React.ComponentProps<"a"> & { asChild?: boolean; isActive?: boolean }
>(({ asChild, isActive, className, children, ...props }, ref) => {
  const cls = cn(
    "flex h-7 min-w-0 -translate-x-px items-center gap-2 overflow-hidden rounded-md px-2 text-sidebar-foreground outline-none ring-sidebar-ring hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 active:bg-sidebar-accent data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground [&>svg]:size-4 [&>svg]:shrink-0",
    className,
  );
  const dataActive = isActive ? "true" : undefined;
  if (asChild) {
    return (
      <MenuSlot className={cls} data-active={dataActive} {...(props as Record<string, unknown>)}>
        {children}
      </MenuSlot>
    );
  }
  return (
    <a ref={ref} className={cls} data-active={dataActive} {...props}>
      {children}
    </a>
  );
});
SidebarMenuSubButton.displayName = "SidebarMenuSubButton";
