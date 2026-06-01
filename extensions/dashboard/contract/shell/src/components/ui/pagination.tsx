import * as React from "react";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight, MoreHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface PaginationRootProps extends React.HTMLAttributes<HTMLElement> {
  className?: string;
}

export const Pagination = ({ className, ...props }: PaginationRootProps) => (
  <nav
    role="navigation"
    aria-label="pagination"
    className={cn("mx-auto flex w-full justify-center", className)}
    {...props}
  />
);

export const PaginationContent = React.forwardRef<HTMLUListElement, React.HTMLAttributes<HTMLUListElement>>(
  ({ className, ...props }, ref) => (
    <ul ref={ref} className={cn("flex flex-row items-center gap-1", className)} {...props} />
  ),
);
PaginationContent.displayName = "PaginationContent";

export const PaginationItem = React.forwardRef<HTMLLIElement, React.HTMLAttributes<HTMLLIElement>>(
  ({ className, ...props }, ref) => <li ref={ref} className={cn(className)} {...props} />,
);
PaginationItem.displayName = "PaginationItem";

interface PaginationLinkProps extends React.ComponentProps<typeof Button> {
  isActive?: boolean;
}

export const PaginationLink = ({ className, isActive, size = "icon", ...props }: PaginationLinkProps) => (
  <Button
    aria-current={isActive ? "page" : undefined}
    variant={isActive ? "outline" : "ghost"}
    size={size}
    className={cn(className)}
    {...props}
  />
);

export const PaginationFirst = (props: React.ComponentProps<typeof Button>) => (
  <PaginationLink aria-label="Go to first page" {...props}>
    <ChevronsLeft className="h-4 w-4" />
  </PaginationLink>
);

export const PaginationPrevious = (props: React.ComponentProps<typeof Button>) => (
  <PaginationLink aria-label="Go to previous page" {...props}>
    <ChevronLeft className="h-4 w-4" />
  </PaginationLink>
);

export const PaginationNext = (props: React.ComponentProps<typeof Button>) => (
  <PaginationLink aria-label="Go to next page" {...props}>
    <ChevronRight className="h-4 w-4" />
  </PaginationLink>
);

export const PaginationLast = (props: React.ComponentProps<typeof Button>) => (
  <PaginationLink aria-label="Go to last page" {...props}>
    <ChevronsRight className="h-4 w-4" />
  </PaginationLink>
);

export const PaginationEllipsis = ({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) => (
  <span
    aria-hidden
    className={cn("flex h-9 w-9 items-center justify-center", className)}
    {...props}
  >
    <MoreHorizontal className="h-4 w-4" />
    <span className="sr-only">More pages</span>
  </span>
);
