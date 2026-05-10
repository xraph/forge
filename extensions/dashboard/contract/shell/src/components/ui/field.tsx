import * as React from "react";
import { cn } from "@/lib/utils";

// Slice (l.5) ports shadcn/ui's Field primitives — the layout helpers used
// by the login-04 block from ui.shadcn.com/blocks. They're plain semantic
// wrappers (no Radix / Base UI dependency) that compose form rows with
// labels, descriptions, and visual separators consistent with shadcn's
// typography. Public API matches shadcn so downstream blocks slot in.

export const FieldGroup = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex flex-col gap-6", className)} {...props} />
  ),
);
FieldGroup.displayName = "FieldGroup";

export const Field = React.forwardRef<HTMLDivElement, React.ComponentProps<"div">>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex flex-col gap-2", className)} {...props} />
  ),
);
Field.displayName = "Field";

export const FieldLabel = React.forwardRef<HTMLLabelElement, React.ComponentProps<"label">>(
  ({ className, ...props }, ref) => (
    <label
      ref={ref}
      className={cn(
        "text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
        className,
      )}
      {...props}
    />
  ),
);
FieldLabel.displayName = "FieldLabel";

export const FieldDescription = React.forwardRef<HTMLParagraphElement, React.ComponentProps<"p">>(
  ({ className, ...props }, ref) => (
    <p ref={ref} className={cn("text-sm text-muted-foreground [&>a]:underline [&>a]:underline-offset-4 [&>a:hover]:text-foreground", className)} {...props} />
  ),
);
FieldDescription.displayName = "FieldDescription";

interface FieldSeparatorProps extends React.ComponentProps<"div"> {
  /** Optional inline label rendered between two horizontal rules. */
  children?: React.ReactNode;
}

export function FieldSeparator({ className, children, ...props }: FieldSeparatorProps) {
  if (!children) {
    return (
      <div
        role="separator"
        aria-orientation="horizontal"
        className={cn("h-px w-full bg-border", className)}
        {...props}
      />
    );
  }
  return (
    <div
      role="separator"
      aria-orientation="horizontal"
      className={cn("relative", className)}
      {...props}
    >
      <div className="absolute inset-0 flex items-center" aria-hidden>
        <span className="w-full border-t border-border" />
      </div>
      <div className="relative flex justify-center text-xs">
        <span className="bg-background px-2 text-muted-foreground">{children}</span>
      </div>
    </div>
  );
}
FieldSeparator.displayName = "FieldSeparator";
