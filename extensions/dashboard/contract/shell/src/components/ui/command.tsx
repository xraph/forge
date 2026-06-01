import * as React from "react";
import { Combobox as BaseCombobox } from "@base-ui-components/react/combobox";
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Command palette / combobox primitive built on Base UI's Combobox.
 * Provides the API surface familiar from `cmdk` so existing patterns
 * port cleanly — but uses only the headless library already installed.
 */

export const Command = BaseCombobox.Root;
export const CommandList = BaseCombobox.List;
export const CommandGroup = BaseCombobox.Group;

export const CommandInputWrapper = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={cn("flex items-center border-b px-3", className)}
    {...props}
  />
));
CommandInputWrapper.displayName = "CommandInputWrapper";

export const CommandInput = React.forwardRef<
  HTMLInputElement,
  React.ComponentPropsWithoutRef<typeof BaseCombobox.Input>
>(({ className, ...props }, ref) => (
  <CommandInputWrapper>
    <Search className="mr-2 h-4 w-4 shrink-0 opacity-50" />
    <BaseCombobox.Input
      ref={ref}
      className={cn(
        "flex h-11 w-full rounded-md bg-transparent py-3 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  </CommandInputWrapper>
));
CommandInput.displayName = "CommandInput";

export const CommandEmpty = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseCombobox.Empty>
>(({ className, ...props }, ref) => (
  <BaseCombobox.Empty
    ref={ref}
    className={cn("py-6 text-center text-sm text-muted-foreground", className)}
    {...props}
  />
));
CommandEmpty.displayName = "CommandEmpty";

export const CommandGroupLabel = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseCombobox.GroupLabel>
>(({ className, ...props }, ref) => (
  <BaseCombobox.GroupLabel
    ref={ref}
    className={cn("px-2 py-1.5 text-xs font-medium text-muted-foreground", className)}
    {...props}
  />
));
CommandGroupLabel.displayName = "CommandGroupLabel";

export const CommandItem = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseCombobox.Item>
>(({ className, ...props }, ref) => (
  <BaseCombobox.Item
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className,
    )}
    {...props}
  />
));
CommandItem.displayName = "CommandItem";

export const CommandShortcut = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLSpanElement>) => (
  <span
    className={cn("ml-auto text-xs tracking-widest text-muted-foreground", className)}
    {...props}
  />
);
CommandShortcut.displayName = "CommandShortcut";

export const CommandPortal = BaseCombobox.Portal;
export const CommandPositioner = BaseCombobox.Positioner;
export const CommandPopup = BaseCombobox.Popup;
