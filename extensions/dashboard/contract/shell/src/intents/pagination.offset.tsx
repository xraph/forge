import * as React from "react";
import { Button } from "@/components/ui/button";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useFilterStateOptional } from "./_filter-state";
import type { IntentComponentProps } from "../runtime/registry";

interface PaginationOffsetProps {
  pageSize?: number;
  offsetParam?: string;
  /** Field on the list payload holding the total count. */
  totalField?: string;
}

/**
 * pagination.offset renders Prev/Next + page-counter controls for
 * offset-paginated lists. It writes `offset` and `limit` into the
 * enclosing resource.list's FilterStateContext so the list re-fetches
 * on page change. The total count is read from the list payload's
 * `total` field (overridable via props.totalField) and used to
 * compute the last page.
 */
export function PaginationOffset({
  props,
}: IntentComponentProps<unknown, PaginationOffsetProps>) {
  const filter = useFilterStateOptional();
  const offsetParam = props.offsetParam ?? "offset";
  const limit = props.pageSize ?? 25;
  const offset = (filter?.values[offsetParam] as number | undefined) ?? 0;
  const total = (filter?.values[props.totalField ?? "__total__"] as number | undefined) ?? 0;
  const page = Math.floor(offset / limit) + 1;
  const lastPage = total > 0 ? Math.max(1, Math.ceil(total / limit)) : page;

  React.useEffect(() => {
    if (filter && filter.values["limit"] !== limit) {
      filter.setValue("limit", limit);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [limit]);

  const goPrev = () => {
    if (!filter || offset === 0) return;
    filter.setValue(offsetParam, Math.max(0, offset - limit));
  };
  const goNext = () => {
    if (!filter || page >= lastPage) return;
    filter.setValue(offsetParam, offset + limit);
  };

  return (
    <div className="flex items-center justify-end gap-2 text-sm">
      <span className="text-muted-foreground">
        Page {page} of {lastPage}
      </span>
      <Button variant="outline" size="sm" disabled={offset === 0} onClick={goPrev}>
        <ChevronLeft className="h-4 w-4" />
      </Button>
      <Button variant="outline" size="sm" disabled={page >= lastPage} onClick={goNext}>
        <ChevronRight className="h-4 w-4" />
      </Button>
    </div>
  );
}
