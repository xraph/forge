import {
  Pagination as PaginationRoot,
  PaginationContent,
  PaginationEllipsis,
  PaginationFirst,
  PaginationItem,
  PaginationLast,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "@/components/ui/pagination";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface PaginationMoleculeProps {
  page: number;
  pageSize: number;
  total?: number;
  pageSizeOptions?: number[];
  siblingCount?: number;
  showFirstLast?: boolean;
  showPageSize?: boolean;
  onPageChange?: Action;
  onPageSizeChange?: Action;
  className?: string;
}

export function MoleculePagination({
  props,
}: IntentComponentProps<unknown, PaginationMoleculeProps>) {
  const pageCount = props.total ? Math.max(1, Math.ceil(props.total / props.pageSize)) : 1;
  const page = Math.min(Math.max(1, props.page), pageCount);
  const siblings = props.siblingCount ?? 1;

  const go = (next: number) => {
    if (next < 1 || next > pageCount || next === page) return;
    if (props.onPageChange) dispatchAction(props.onPageChange, { value: next });
  };

  const changeSize = (next: string | null) => {
    if (!next) return;
    const n = Number.parseInt(next, 10);
    if (!Number.isFinite(n) || n === props.pageSize) return;
    if (props.onPageSizeChange) dispatchAction(props.onPageSizeChange, { value: n });
  };

  // Build the visible page-number sequence with leading/trailing ellipses.
  const pages = buildPages(page, pageCount, siblings);

  return (
    <div className={cn("flex w-full items-center justify-between gap-4", props.className)}>
      <div className="text-sm text-muted-foreground">
        {props.total !== undefined ? (
          <>
            Showing <strong>{(page - 1) * props.pageSize + 1}</strong>–
            <strong>{Math.min(page * props.pageSize, props.total)}</strong> of{" "}
            <strong>{props.total}</strong>
          </>
        ) : (
          <>Page {page} of {pageCount}</>
        )}
      </div>

      <div className="flex items-center gap-4">
        {props.showPageSize !== false && props.pageSizeOptions && props.pageSizeOptions.length > 0 ? (
          <div className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">Rows per page</span>
            <Select value={String(props.pageSize)} onValueChange={changeSize}>
              <SelectTrigger className="h-8 w-[72px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {props.pageSizeOptions.map((n) => (
                  <SelectItem key={n} value={String(n)}>
                    {n}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}

        <PaginationRoot className="mx-0 w-auto justify-end">
          <PaginationContent>
            {props.showFirstLast ? (
              <PaginationItem>
                <PaginationFirst onClick={() => go(1)} disabled={page === 1} />
              </PaginationItem>
            ) : null}
            <PaginationItem>
              <PaginationPrevious onClick={() => go(page - 1)} disabled={page === 1} />
            </PaginationItem>
            {pages.map((p, i) =>
              p === "ellipsis" ? (
                <PaginationItem key={`e-${i}`}>
                  <PaginationEllipsis />
                </PaginationItem>
              ) : (
                <PaginationItem key={p}>
                  <PaginationLink isActive={p === page} onClick={() => go(p)}>
                    {p}
                  </PaginationLink>
                </PaginationItem>
              ),
            )}
            <PaginationItem>
              <PaginationNext onClick={() => go(page + 1)} disabled={page === pageCount} />
            </PaginationItem>
            {props.showFirstLast ? (
              <PaginationItem>
                <PaginationLast onClick={() => go(pageCount)} disabled={page === pageCount} />
              </PaginationItem>
            ) : null}
          </PaginationContent>
        </PaginationRoot>
      </div>
    </div>
  );
}

function buildPages(page: number, total: number, siblings: number): (number | "ellipsis")[] {
  const range = (lo: number, hi: number) => {
    const out: number[] = [];
    for (let i = lo; i <= hi; i++) out.push(i);
    return out;
  };
  const totalNumbers = siblings * 2 + 5; // first + last + current + 2*siblings + 2 ellipses
  if (totalNumbers >= total) return range(1, total);

  const left = Math.max(page - siblings, 1);
  const right = Math.min(page + siblings, total);
  const showLeftEllipsis = left > 2;
  const showRightEllipsis = right < total - 1;

  const out: (number | "ellipsis")[] = [];
  out.push(1);
  if (showLeftEllipsis) out.push("ellipsis");
  for (const n of range(Math.max(2, left), Math.min(total - 1, right))) out.push(n);
  if (showRightEllipsis) out.push("ellipsis");
  if (total > 1) out.push(total);
  return out;
}
