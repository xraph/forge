import * as React from "react";
import { Button } from "@/components/ui/button";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useFilterStateOptional } from "./_filter-state";
import type { IntentComponentProps } from "../runtime/registry";

interface PaginationCursorProps {
  /** Page size sent as `limit` (defaults to 25). */
  pageSize?: number;
  /** Query parameter name for the cursor (defaults to "cursor"). */
  cursorParam?: string;
  /** Field on the previous page payload that holds the next-page cursor. */
  nextCursorField?: string;
}

/**
 * pagination.cursor renders Prev/Next controls for cursor-paginated
 * lists. It writes the cursor into the enclosing resource.list's
 * FilterStateContext (the same mechanism filter.* uses) so the list
 * re-fetches with the new cursor. A history stack is kept locally so
 * Prev works without the server needing to round-trip prior cursors.
 */
export function PaginationCursor({
  props,
}: IntentComponentProps<unknown, PaginationCursorProps>) {
  const filter = useFilterStateOptional();
  const cursorParam = props.cursorParam ?? "cursor";
  const limit = props.pageSize ?? 25;

  const [history, setHistory] = React.useState<string[]>([]);

  React.useEffect(() => {
    if (filter && filter.values["limit"] !== limit) {
      filter.setValue("limit", limit);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [limit]);

  const nextCursor = (filter?.values["__nextCursor__"] as string | undefined) ?? "";
  const hasPrev = history.length > 0;
  const hasNext = Boolean(nextCursor);

  const goNext = () => {
    if (!filter || !nextCursor) return;
    const cur = (filter.values[cursorParam] as string | undefined) ?? "";
    setHistory((h) => [...h, cur]);
    filter.setValue(cursorParam, nextCursor);
  };

  const goPrev = () => {
    if (!filter || history.length === 0) return;
    const prev = history[history.length - 1] ?? "";
    setHistory((h) => h.slice(0, -1));
    filter.setValue(cursorParam, prev || undefined);
  };

  return (
    <div className="flex items-center justify-end gap-1">
      <Button variant="outline" size="sm" disabled={!hasPrev} onClick={goPrev}>
        <ChevronLeft className="h-4 w-4" />
        Previous
      </Button>
      <Button variant="outline" size="sm" disabled={!hasNext} onClick={goNext}>
        Next
        <ChevronRight className="h-4 w-4" />
      </Button>
    </div>
  );
}
