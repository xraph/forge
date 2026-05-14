import * as React from "react";
import { Menu } from "@base-ui-components/react/menu";
import { Check, ChevronRight, Circle } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * shadcn-style DropdownMenu façade over Base UI's Menu primitive. Public
 * names are kept identical to the Radix-based version so consumers don't
 * change. Base UI splits the menu chrome into Root / Trigger / Portal /
 * Positioner / Popup; we collapse that into the shadcn ergonomic where the
 * Content component owns the portal + positioner internally.
 */

export const DropdownMenu = Menu.Root;

/**
 * Translates the shadcn `asChild` ergonomic to Base UI's `render` prop. When
 * asChild is true, the single child element is rendered as the trigger; the
 * shadcn API stays the same so consumers don't change.
 */
export const DropdownMenuTrigger = React.forwardRef<
  HTMLButtonElement,
  React.ComponentPropsWithoutRef<typeof Menu.Trigger> & { asChild?: boolean }
>(({ asChild, children, ...props }, ref) => {
  if (asChild && React.isValidElement(children)) {
    return (
      <Menu.Trigger
        ref={ref}
        render={children as React.ReactElement<Record<string, unknown>>}
        {...props}
      />
    );
  }
  return (
    <Menu.Trigger ref={ref} {...props}>
      {children}
    </Menu.Trigger>
  );
});
DropdownMenuTrigger.displayName = "DropdownMenuTrigger";

export const DropdownMenuGroup = Menu.Group;
export const DropdownMenuPortal = Menu.Portal;
export const DropdownMenuSub = Menu.SubmenuRoot;
export const DropdownMenuRadioGroup = Menu.RadioGroup;

export const DropdownMenuSubTrigger = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.SubmenuTrigger> & { inset?: boolean }
>(({ className, inset, children, ...props }, ref) => (
  <Menu.SubmenuTrigger
    ref={ref}
    className={cn(
      "flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none focus:bg-accent data-[popup-open]:bg-accent",
      inset ? "pl-8" : "",
      className,
    )}
    {...props}
  >
    {children}
    <ChevronRight className="ml-auto h-4 w-4" />
  </Menu.SubmenuTrigger>
));
DropdownMenuSubTrigger.displayName = "DropdownMenuSubTrigger";

export const DropdownMenuSubContent = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.Popup>
>(({ className, ...props }, ref) => (
  <Menu.Portal>
    <Menu.Positioner>
      <Menu.Popup
        ref={ref}
        className={cn(
          "z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-lg data-[open]:animate-in data-[closed]:animate-out",
          className,
        )}
        {...props}
      />
    </Menu.Positioner>
  </Menu.Portal>
));
DropdownMenuSubContent.displayName = "DropdownMenuSubContent";

export const DropdownMenuContent = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.Popup> & {
    sideOffset?: number;
    align?: "start" | "center" | "end";
  }
>(({ className, sideOffset = 4, align = "center", ...props }, ref) => (
  <Menu.Portal>
    <Menu.Positioner sideOffset={sideOffset} align={align}>
      <Menu.Popup
        ref={ref}
        className={cn(
          "z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md outline-none data-[open]:animate-in data-[closed]:animate-out data-[closed]:fade-out-0 data-[open]:fade-in-0",
          className,
        )}
        {...props}
      />
    </Menu.Positioner>
  </Menu.Portal>
));
DropdownMenuContent.displayName = "DropdownMenuContent";

export const DropdownMenuItem = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.Item> & { inset?: boolean }
>(({ className, inset, ...props }, ref) => (
  <Menu.Item
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      inset ? "pl-8" : "",
      className,
    )}
    {...props}
  />
));
DropdownMenuItem.displayName = "DropdownMenuItem";

export const DropdownMenuCheckboxItem = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.CheckboxItem>
>(({ className, children, ...props }, ref) => (
  <Menu.CheckboxItem
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none focus:bg-accent focus:text-accent-foreground data-[highlighted]:bg-accent",
      className,
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <Menu.CheckboxItemIndicator>
        <Check className="h-4 w-4" />
      </Menu.CheckboxItemIndicator>
    </span>
    {children}
  </Menu.CheckboxItem>
));
DropdownMenuCheckboxItem.displayName = "DropdownMenuCheckboxItem";

export const DropdownMenuRadioItem = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.RadioItem>
>(({ className, children, ...props }, ref) => (
  <Menu.RadioItem
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none focus:bg-accent data-[highlighted]:bg-accent",
      className,
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <Menu.RadioItemIndicator>
        <Circle className="h-2 w-2 fill-current" />
      </Menu.RadioItemIndicator>
    </span>
    {children}
  </Menu.RadioItem>
));
DropdownMenuRadioItem.displayName = "DropdownMenuRadioItem";

export const DropdownMenuLabel = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Menu.GroupLabel> & { inset?: boolean }
>(({ className, inset, ...props }, ref) => (
  <Menu.GroupLabel
    ref={ref}
    className={cn("px-2 py-1.5 text-sm font-semibold", inset ? "pl-8" : "", className)}
    {...props}
  />
));
DropdownMenuLabel.displayName = "DropdownMenuLabel";

export const DropdownMenuSeparator = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    role="separator"
    className={cn("-mx-1 my-1 h-px bg-muted", className)}
    {...props}
  />
));
DropdownMenuSeparator.displayName = "DropdownMenuSeparator";

export function DropdownMenuShortcut({
  className,
  ...props
}: React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span className={cn("ml-auto text-xs tracking-widest opacity-60", className)} {...props} />
  );
}
DropdownMenuShortcut.displayName = "DropdownMenuShortcut";
