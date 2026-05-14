import * as React from "react";
import { Dialog } from "@base-ui-components/react/dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

export const Sheet = Dialog.Root;

/** Translates the shadcn `asChild` ergonomic to Base UI's `render` prop. */
export const SheetTrigger = React.forwardRef<
  HTMLButtonElement,
  React.ComponentPropsWithoutRef<typeof Dialog.Trigger> & { asChild?: boolean }
>(({ asChild, children, ...props }, ref) => {
  if (asChild && React.isValidElement(children)) {
    return (
      <Dialog.Trigger
        ref={ref}
        render={children as React.ReactElement<Record<string, unknown>>}
        {...props}
      />
    );
  }
  return (
    <Dialog.Trigger ref={ref} {...props}>
      {children}
    </Dialog.Trigger>
  );
});
SheetTrigger.displayName = "SheetTrigger";

export const SheetClose = Dialog.Close;
export const SheetPortal = Dialog.Portal;

export const SheetOverlay = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Dialog.Backdrop>
>(({ className, ...props }, ref) => (
  <Dialog.Backdrop
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-black/80 data-[open]:animate-in data-[closed]:animate-out data-[closed]:fade-out-0 data-[open]:fade-in-0",
      className,
    )}
    {...props}
  />
));
SheetOverlay.displayName = "SheetOverlay";

export const SheetContent = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof Dialog.Popup> & {
    side?: "right" | "left" | "top" | "bottom";
  }
>(({ side = "right", className, children, ...props }, ref) => {
  const sideClass =
    side === "right"
      ? "inset-y-0 right-0 h-full w-3/4 border-l data-[closed]:slide-out-to-right data-[open]:slide-in-from-right sm:max-w-md"
      : side === "left"
        ? "inset-y-0 left-0 h-full w-3/4 border-r data-[closed]:slide-out-to-left data-[open]:slide-in-from-left sm:max-w-md"
        : side === "top"
          ? "inset-x-0 top-0 border-b data-[closed]:slide-out-to-top data-[open]:slide-in-from-top"
          : "inset-x-0 bottom-0 border-t data-[closed]:slide-out-to-bottom data-[open]:slide-in-from-bottom";
  return (
    <SheetPortal>
      <SheetOverlay />
      <Dialog.Popup
        ref={ref}
        className={cn(
          "fixed z-50 gap-4 bg-background p-6 shadow-lg outline-none transition ease-in-out data-[open]:animate-in data-[closed]:animate-out data-[closed]:duration-300 data-[open]:duration-500",
          sideClass,
          className,
        )}
        {...props}
      >
        {children}
        <Dialog.Close className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none">
          <X className="h-4 w-4" />
          <span className="sr-only">Close</span>
        </Dialog.Close>
      </Dialog.Popup>
    </SheetPortal>
  );
});
SheetContent.displayName = "SheetContent";

export function SheetHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex flex-col space-y-2 text-center sm:text-left", className)}
      {...props}
    />
  );
}

export const SheetTitle = React.forwardRef<
  HTMLHeadingElement,
  React.ComponentPropsWithoutRef<typeof Dialog.Title>
>(({ className, ...props }, ref) => (
  <Dialog.Title
    ref={ref}
    className={cn("text-lg font-semibold text-foreground", className)}
    {...props}
  />
));
SheetTitle.displayName = "SheetTitle";

export const SheetDescription = React.forwardRef<
  HTMLParagraphElement,
  React.ComponentPropsWithoutRef<typeof Dialog.Description>
>(({ className, ...props }, ref) => (
  <Dialog.Description
    ref={ref}
    className={cn("text-sm text-muted-foreground", className)}
    {...props}
  />
));
SheetDescription.displayName = "SheetDescription";
