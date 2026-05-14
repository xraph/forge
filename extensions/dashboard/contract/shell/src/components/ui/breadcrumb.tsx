import * as React from "react";
import { ChevronRight, MoreHorizontal } from "lucide-react";
import { cn } from "@/lib/utils";

// Slice (l) ports the shadcn Breadcrumb primitives. No external library
// dependency — plain semantic HTML wrapped in cn() helpers, matching the
// shadcn API surface so consumers get the familiar import path.

export const Breadcrumb = React.forwardRef<HTMLElement, React.ComponentProps<"nav">>(
  ({ ...props }, ref) => <nav ref={ref} aria-label="breadcrumb" {...props} />,
);
Breadcrumb.displayName = "Breadcrumb";

export const BreadcrumbList = React.forwardRef<HTMLOListElement, React.ComponentProps<"ol">>(
  ({ className, ...props }, ref) => (
    <ol
      ref={ref}
      className={cn(
        "flex flex-wrap items-center gap-1.5 break-words text-sm text-muted-foreground sm:gap-2.5",
        className,
      )}
      {...props}
    />
  ),
);
BreadcrumbList.displayName = "BreadcrumbList";

export const BreadcrumbItem = React.forwardRef<HTMLLIElement, React.ComponentProps<"li">>(
  ({ className, ...props }, ref) => (
    <li ref={ref} className={cn("inline-flex items-center gap-1.5", className)} {...props} />
  ),
);
BreadcrumbItem.displayName = "BreadcrumbItem";

export const BreadcrumbLink = React.forwardRef<
  HTMLAnchorElement,
  React.ComponentProps<"a"> & { asChild?: boolean }
>(({ asChild, className, children, ...props }, ref) => {
  const cls = cn("transition-colors hover:text-foreground", className);
  if (asChild && React.isValidElement(children)) {
    const child = children as React.ReactElement<Record<string, unknown>>;
    const childClass = typeof child.props.className === "string" ? child.props.className : undefined;
    return React.cloneElement(child, {
      ...(props as Record<string, unknown>),
      ...child.props,
      className: cn(cls, childClass),
    });
  }
  return (
    <a ref={ref} className={cls} {...props}>
      {children}
    </a>
  );
});
BreadcrumbLink.displayName = "BreadcrumbLink";

export const BreadcrumbPage = React.forwardRef<HTMLSpanElement, React.ComponentProps<"span">>(
  ({ className, ...props }, ref) => (
    <span
      ref={ref}
      role="link"
      aria-disabled="true"
      aria-current="page"
      className={cn("font-normal text-foreground", className)}
      {...props}
    />
  ),
);
BreadcrumbPage.displayName = "BreadcrumbPage";

export const BreadcrumbSeparator = ({
  children,
  className,
  ...props
}: React.ComponentProps<"li">) => (
  <li
    role="presentation"
    aria-hidden="true"
    className={cn("[&>svg]:size-3.5", className)}
    {...props}
  >
    {children ?? <ChevronRight />}
  </li>
);
BreadcrumbSeparator.displayName = "BreadcrumbSeparator";

export const BreadcrumbEllipsis = ({ className, ...props }: React.ComponentProps<"span">) => (
  <span
    role="presentation"
    aria-hidden="true"
    className={cn("flex h-9 w-9 items-center justify-center", className)}
    {...props}
  >
    <MoreHorizontal className="h-4 w-4" />
    <span className="sr-only">More</span>
  </span>
);
BreadcrumbEllipsis.displayName = "BreadcrumbEllipsis";
